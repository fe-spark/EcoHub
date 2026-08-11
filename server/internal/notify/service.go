package notify

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"regexp"
	"sort"
	"strings"
	"sync"
	"time"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
)

var (
	// 数字 chat id（含负数群组/频道）或 @username
	chatIDPattern = regexp.MustCompile(`^(-?\d+|@[A-Za-z0-9_]+)$`)
	sendSem       = make(chan struct{}, 4)
	client        = newTelegramClient()
)

// SourceConfigChangeItem 批量源配置变更通知项（结构定义见 model 包，此处导出别名）。
type SourceConfigChangeItem = model.SourceConfigChangeItem

// MaskBotToken Token 脱敏展示。
func MaskBotToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	r := []rune(token)
	n := len(r)
	if n <= 10 {
		return strings.Repeat("*", n)
	}
	return string(r[:6]) + "***" + string(r[n-4:])
}

// IsMaskedToken 判断是否为脱敏后的占位 token（更新时保留旧值）。
// 与 MaskBotToken 输出对齐：短 token 全 `*`，长 token 为「前6 + *** + 后4」。
func IsMaskedToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if strings.Trim(token, "*") == "" {
		return true
	}
	// 与 MaskBotToken 长 token 形态一致
	if len([]rune(token)) > 10 && strings.Contains(token, "***") {
		parts := strings.Split(token, "***")
		if len(parts) == 2 && len([]rune(parts[0])) == 6 && len([]rune(parts[1])) == 4 {
			return true
		}
	}
	return false
}

func allowOrLog(key string, minInterval time.Duration) bool {
	if globalRate.allow(key, minInterval) {
		return true
	}
	log.Printf("[Notify] rate-limited key=%s interval=%s", key, minInterval)
	return false
}

// PublicConfig 返回给前端的配置（Token 脱敏）。
func PublicConfig(cfg model.NotifyConfig) model.NotifyConfig {
	cfg.BotToken = MaskBotToken(cfg.BotToken)
	if cfg.ChatIDs == nil {
		cfg.ChatIDs = []string{}
	}
	return cfg
}

// ValidateAndMergeUpdate 校验更新请求，并与旧配置合并 Token。
func ValidateAndMergeUpdate(old, incoming model.NotifyConfig) (model.NotifyConfig, error) {
	cfg := incoming
	// Token：脱敏或空则保留旧值
	token := strings.TrimSpace(cfg.BotToken)
	if token == "" || IsMaskedToken(token) {
		cfg.BotToken = old.BotToken
	} else {
		cfg.BotToken = token
	}

	chatIDs := repository.NormalizeChatIDs(cfg.ChatIDs)
	// 仅启用时校验 Chat ID 格式：禁用状态下允许保存（含清理残留无效 ID），避免无法保存配置
	if cfg.Enabled {
		for _, id := range chatIDs {
			if !chatIDPattern.MatchString(id) {
				return model.NotifyConfig{}, fmt.Errorf("无效的 Chat ID: %s", id)
			}
		}
	}
	cfg.ChatIDs = chatIDs

	// ChatIDs 为成员真相源：合并旧配置与请求中的 Target 元数据（Thread/等级/订阅），
	// 再按 ChatIDs 重建，避免前端只改 chatIds 时残留旧 Targets 导致仍发到旧群。
	targetSources := make([]model.NotifyTarget, 0, len(old.Targets)+len(cfg.Targets))
	targetSources = append(targetSources, old.Targets...)
	targetSources = append(targetSources, cfg.Targets...)
	cfg.Targets = repository.RebuildTargetsFromChatIDs(cfg.ChatIDs, targetSources)

	if cfg.MaxFilmsInMessage <= 0 {
		cfg.MaxFilmsInMessage = model.DefaultMaxFilmsInMessage
	}
	if cfg.MaxFilmsInMessage > model.MaxFilmsInMessageCap {
		return model.NotifyConfig{}, fmt.Errorf("maxFilmsInMessage 范围为 1-%d", model.MaxFilmsInMessageCap)
	}
	if cfg.MinIntervalSec < 0 || cfg.MinIntervalSec > 3600 {
		return model.NotifyConfig{}, fmt.Errorf("minIntervalSec 范围为 0-3600")
	}

	if cfg.QuietHours.Enabled {
		if strings.TrimSpace(cfg.QuietHours.Start) == "" || strings.TrimSpace(cfg.QuietHours.End) == "" {
			return model.NotifyConfig{}, fmt.Errorf("启用免打扰时必须填写开始与结束时间 (HH:mm)")
		}
		if _, _, err := parseHHMM(cfg.QuietHours.Start); err != nil {
			return model.NotifyConfig{}, fmt.Errorf("免打扰开始时间格式无效，应为 HH:mm")
		}
		if _, _, err := parseHHMM(cfg.QuietHours.End); err != nil {
			return model.NotifyConfig{}, fmt.Errorf("免打扰结束时间格式无效，应为 HH:mm")
		}
	}

	if cfg.Enabled {
		if strings.TrimSpace(cfg.BotToken) == "" {
			return model.NotifyConfig{}, fmt.Errorf("启用通知时必须配置 Bot Token")
		}
		if len(cfg.ChatIDs) == 0 {
			return model.NotifyConfig{}, fmt.Errorf("启用通知时至少配置一个 Chat ID")
		}
	}
	return cfg, nil
}

// GetConfig 读取配置。
func GetConfig() model.NotifyConfig {
	return repository.GetNotifyConfig()
}

// SaveConfig 保存配置。
func SaveConfig(cfg model.NotifyConfig) error {
	if err := repository.SaveNotifyConfig(cfg); err != nil {
		return err
	}
	// Token 变更后刷新 Telegram 回调轮询
	EnsureBotPoller()
	return nil
}

// siteName 读取站点名用于消息前缀。
func siteName() string {
	return strings.TrimSpace(repository.GetSiteBasic().SiteName)
}

// eventEnabled 判断事件是否开启。
func eventEnabled(cfg model.NotifyConfig, event string) bool {
	if !cfg.Enabled {
		return false
	}
	switch event {
	case model.NotifyEventCollectBatchSummary:
		return cfg.Events.CollectBatchSummary
	case model.NotifyEventCollectSourceFailed:
		return cfg.Events.CollectSourceFailed
	case model.NotifyEventCollectFinalizeFailed:
		return cfg.Events.CollectFinalizeFailed
	case model.NotifyEventCollectProgressStale:
		return cfg.Events.CollectProgressStale
	case model.NotifyEventCronTaskFailed:
		return cfg.Events.CronTaskFailed
	case model.NotifyEventCronTaskDone:
		return cfg.Events.CronTaskDone
	case model.NotifyEventSourceConfigChanged:
		return cfg.Events.SourceConfigChanged
	default:
		return false
	}
}

// IsEventEnabled 读取当前配置判断某事件是否开启（总开关 + 子开关）。
func IsEventEnabled(event string) bool {
	return eventEnabled(GetConfig(), event)
}

// PublishBatchSummary 异步发送采集批次摘要（不走 MinInterval）。
// 批次由 BuildBatchPayload 在同步阶段写入 payload.ChangeBatchID，异步发送不再读全局状态。
func PublishBatchSummary(payload model.CollectBatchNotifyPayload) {
	go safePublish(func() {
		cfg := GetConfig()
		if !eventEnabled(cfg, model.NotifyEventCollectBatchSummary) {
			return
		}
		payload.SiteName = siteName()
		// 单片更新不附明细；其余服从配置
		if payload.Trigger == model.NotifyTriggerSingleUpdate || !cfg.IncludeFilmDetails {
			payload.IncludeFilmDetails = false
		}
		sendBatchSummary(cfg, payload)
	})
}

// sendBatchSummary 发概要；变更 mid 在 MySQL 批次表，按钮带 batch_id。
func sendBatchSummary(cfg model.NotifyConfig, payload model.CollectBatchNotifyPayload) {
	pageSize := clampPageSize(cfg.MaxFilmsInMessage)

	batchID := strings.TrimSpace(payload.ChangeBatchID)
	listN := 0
	if batchID != "" {
		listN = CountChangeMids(batchID)
	}

	// 判定是否有真正更新的影片或故障报错
	hasChanges := listN > 0
	hasFailures := payload.FailedSources > 0 || strings.TrimSpace(payload.FinalizeError) != ""
	if cfg.OnlyNotifyOnUpdate && !hasChanges && !hasFailures {
		log.Printf("[Notify] 批次采集无更新且无失败，已触发自动静音跳过推送 (OnlyNotifyOnUpdate=true)")
		return
	}
	severity := model.SeverityInfo
	if hasFailures {
		severity = model.SeverityError
	}
	// 无活跃批次时，payload 仍可能有统计；列表依赖 MySQL
	if payload.IncludeFilmDetails && listN > 0 {
		// 头行变更与列表一致
		if payload.TotalFilms < listN {
			payload.TotalFilms = listN
		}
		overview := formatBatchOverview(payload, listN, pageSize)
		parts := splitTelegramMessages(overview)
		buttonPart := parts[len(parts)-1]
		if err := SaveChangeBatchMeta(batchID, payload.SiteName, buttonPart, pageSize, listN); err != nil {
			syslog.Errorf("[Notify] 保存变更批次元数据失败: %v", err)
			sendMessages(cfg, severity, model.CategoryCollect, parts)
			return
		}
		for i, part := range parts {
			var markup *InlineKeyboardMarkup
			if i == len(parts)-1 {
				markup = buildOverviewKeyboard(batchID)
			}
			sendMessagesWithMarkup(cfg, severity, model.CategoryCollect, part, markup)
		}
		EnsureBotPoller()
		return
	}

	// 无列表：仍结束可能残留的空批次
	overview := formatBatchOverview(payload, 0, pageSize)
	sendMessages(cfg, severity, model.CategoryCollect, splitTelegramMessages(overview))
}

// PublishSourceFailed 单源失败即时告警。
func PublishSourceFailed(sourceID, sourceName, reason string) {
	go safePublish(func() {
		cfg := GetConfig()
		if !eventEnabled(cfg, model.NotifyEventCollectSourceFailed) {
			return
		}
		key := model.NotifyEventCollectSourceFailed + ":" + sourceID
		if !allowOrLog(key, time.Duration(cfg.MinIntervalSec)*time.Second) {
			return
		}
		messages := formatSourceFailed(siteName(), sourceName, sourceID, reason, time.Now())
		sendMessages(cfg, model.SeverityError, model.CategoryCollect, messages)
	})
}

// PublishProgressStale 进度超时告警。
func PublishProgressStale(sourceID, sourceName, oldStatus string, age time.Duration) {
	go safePublish(func() {
		cfg := GetConfig()
		if !eventEnabled(cfg, model.NotifyEventCollectProgressStale) {
			return
		}
		key := model.NotifyEventCollectProgressStale + ":" + sourceID
		if !allowOrLog(key, time.Duration(cfg.MinIntervalSec)*time.Second) {
			return
		}
		reason := fmt.Sprintf("进度超时 status=%s age=%s", oldStatus, age.Round(time.Second))
		messages := formatProgressStale(siteName(), sourceName, sourceID, reason, time.Now())
		sendMessages(cfg, model.SeverityWarn, model.CategoryCollect, messages)
	})
}

// PublishFinalizeFailed 收尾失败告警。
func PublishFinalizeFailed(sourceCount int, reason string) {
	go safePublish(func() {
		cfg := GetConfig()
		if !eventEnabled(cfg, model.NotifyEventCollectFinalizeFailed) {
			return
		}
		key := model.NotifyEventCollectFinalizeFailed
		if !allowOrLog(key, time.Duration(cfg.MinIntervalSec)*time.Second) {
			return
		}
		messages := formatFinalizeFailed(siteName(), reason, sourceCount, time.Now())
		sendMessages(cfg, model.SeverityError, model.CategoryCollect, messages)
	})
}

// PublishCronFailed 定时任务失败。
func PublishCronFailed(taskID, remark, reason string) {
	go safePublish(func() {
		cfg := GetConfig()
		if !eventEnabled(cfg, model.NotifyEventCronTaskFailed) {
			return
		}
		key := model.NotifyEventCronTaskFailed + ":" + taskID
		if !allowOrLog(key, time.Duration(cfg.MinIntervalSec)*time.Second) {
			return
		}
		messages := formatCronFailed(siteName(), taskID, remark, reason, time.Now())
		sendMessages(cfg, model.SeverityError, model.CategoryCron, messages)
	})
}

// PublishCronDone 定时任务成功（默关）。
func PublishCronDone(taskID, remark, detail string) {
	go safePublish(func() {
		cfg := GetConfig()
		if !eventEnabled(cfg, model.NotifyEventCronTaskDone) {
			return
		}
		key := model.NotifyEventCronTaskDone + ":" + taskID
		if !allowOrLog(key, time.Duration(cfg.MinIntervalSec)*time.Second) {
			return
		}
		messages := formatCronDone(siteName(), taskID, remark, detail, time.Now())
		sendMessages(cfg, model.SeverityInfo, model.CategoryCron, messages)
	})
}

// PublishSourceConfigChanged 采集源配置变更通知（新增/删除/主站切换/启用停用等）。
// changes 为变更描述列表（如「启用状态: 已启用 → 已停用」），按源限流。
func PublishSourceConfigChanged(sourceName, sourceID string, changes []string) {
	go safePublish(func() {
		cfg := GetConfig()
		if !eventEnabled(cfg, model.NotifyEventSourceConfigChanged) {
			return
		}
		key := model.NotifyEventSourceConfigChanged + ":" + sourceID
		if !allowOrLog(key, time.Duration(cfg.MinIntervalSec)*time.Second) {
			return
		}
		messages := formatSourceConfigChanged(siteName(), sourceName, sourceID, changes, time.Now())
		sendMessages(cfg, model.SeverityNotice, model.CategoryAudit, messages)
	})
}

// PublishSourceConfigsChanged 批量采集源配置变更通知（批量启用/禁用等），聚合发送（超长按页拆分）。
// 限流 key 为事件 + 源 ID 集合指纹：同一批源短时间重复操作会限流，不同源集合互不拦截。
func PublishSourceConfigsChanged(items []SourceConfigChangeItem) {
	if len(items) == 0 {
		return
	}
	go safePublish(func() {
		cfg := GetConfig()
		if !eventEnabled(cfg, model.NotifyEventSourceConfigChanged) {
			return
		}
		key := sourceConfigBatchRateKey(items)
		if !allowOrLog(key, time.Duration(cfg.MinIntervalSec)*time.Second) {
			return
		}
		messages := formatSourceConfigsChanged(siteName(), items, time.Now())
		sendMessages(cfg, model.SeverityNotice, model.CategoryAudit, messages)
	})
}

// sourceConfigBatchRateKey 批量配置变更限流 key：按去重排序后的源 ID 指纹，避免固定 :batch 互踩。
func sourceConfigBatchRateKey(items []SourceConfigChangeItem) string {
	ids := make([]string, 0, len(items))
	seen := make(map[string]struct{}, len(items))
	for _, it := range items {
		id := strings.TrimSpace(it.SourceID)
		if id == "" {
			id = strings.TrimSpace(it.SourceName)
		}
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	sort.Strings(ids)
	if len(ids) == 0 {
		return model.NotifyEventSourceConfigChanged + ":batch:empty"
	}
	sum := sha1.Sum([]byte(strings.Join(ids, "\n")))
	return model.NotifyEventSourceConfigChanged + ":batch:" + hex.EncodeToString(sum[:8])
}

// SendTest 使用已保存配置发送测试消息。
func SendTest() (model.NotifyTestResult, error) {
	return SendTestWith("", nil)
}

// 测试发送最小间隔，避免管理端被当作 Telegram 代发代理刷接口。
const testSendMinInterval = 3 * time.Second

// SendTestWith 使用请求中的草稿 Token/Chat 发送测试（不落库）。
// botToken 为空或脱敏时沿用已保存 Token；chatIDs 为空时沿用已保存列表。
// 管理端可提交草稿 Token 联通验证；接口有最小间隔限流，降低被当代理刷用的风险。
func SendTestWith(botToken string, chatIDs []string) (model.NotifyTestResult, error) {
	if !globalRate.allow("notify:test_send", testSendMinInterval) {
		return model.NotifyTestResult{}, fmt.Errorf("测试发送过于频繁，请 %s 后再试", testSendMinInterval)
	}
	cfg := GetConfig()
	token := strings.TrimSpace(botToken)
	if token == "" || IsMaskedToken(token) {
		token = cfg.BotToken
	}
	ids := repository.NormalizeChatIDs(chatIDs)
	if len(ids) == 0 {
		ids = append([]string(nil), cfg.ChatIDs...)
	}
	for _, id := range ids {
		if !chatIDPattern.MatchString(id) {
			return model.NotifyTestResult{}, fmt.Errorf("无效的 Chat ID: %s", id)
		}
	}
	if strings.TrimSpace(token) == "" {
		return model.NotifyTestResult{}, fmt.Errorf("请先填写 Bot Token")
	}
	if len(ids) == 0 {
		return model.NotifyTestResult{}, fmt.Errorf("请先填写至少一个 Chat ID")
	}
	text := formatTestMessage(siteName())
	result := model.NotifyTestResult{}
	// 给代理握手 + TLS 留足时间；真正连不通仍会在 transport 层提前失败
	ctx, cancel := context.WithTimeout(context.Background(), 35*time.Second)
	defer cancel()
	for _, chatID := range ids {
		if err := client.sendMessage(ctx, token, chatID, text); err != nil {
			// err 已经过 sanitizeTelegramErr，不含完整 Token
			result.Failed = append(result.Failed, model.NotifyChatError{
				ChatID: chatID,
				Error:  err.Error(),
			})
			syslog.Errorf("[Notify] 测试发送失败 chat=%s err=%v", chatID, err)
			continue
		}
		result.Sent++
	}
	if result.Sent == 0 {
		return result, fmt.Errorf("全部 Chat 发送失败: %s", summarizeChatErrors(result.Failed))
	}
	return result, nil
}

// summarizeChatErrors 将各 Chat 失败原因拼成可读摘要（供 API msg 展示）。
func summarizeChatErrors(failed []model.NotifyChatError) string {
	if len(failed) == 0 {
		return "未知原因"
	}
	parts := make([]string, 0, len(failed))
	for _, f := range failed {
		parts = append(parts, fmt.Sprintf("%s (%s)", f.ChatID, f.Error))
	}
	return strings.Join(parts, "; ")
}

func sendMessages(cfg model.NotifyConfig, severity model.Severity, category string, messages []string) {
	for _, msg := range messages {
		sendMessagesWithMarkup(cfg, severity, category, msg, nil)
	}
}

var severityRank = map[model.Severity]int{
	model.SeverityInfo:     1,
	model.SeverityNotice:   2,
	model.SeverityWarn:     3,
	model.SeverityError:    4,
	model.SeverityCritical: 5,
}

func isLevelAllowed(targetMin model.Severity, evtLevel model.Severity) bool {
	if targetMin == "" {
		return true
	}
	rTarget, ok1 := severityRank[targetMin]
	rEvt, ok2 := severityRank[evtLevel]
	if !ok1 || !ok2 {
		return true
	}
	return rEvt >= rTarget
}

func isCategorySubscribed(subscribed []string, category string) bool {
	if len(subscribed) == 0 {
		return true
	}
	for _, c := range subscribed {
		if c == category || strings.HasPrefix(category, c+".") {
			return true
		}
	}
	return false
}

func parseHHMM(s string) (int, int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time format")
	}
	var h, m int
	_, err := fmt.Sscanf(s, "%d:%d", &h, &m)
	if err != nil {
		return 0, 0, err
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid time range")
	}
	return h, m, nil
}

// quietHoursLocation 免打扰按业务时区（东八区）计算，避免 Docker 默认 UTC 导致时段偏移。
var quietHoursLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

func isInQuietHours(qh model.NotifyQuietHours, now time.Time) bool {
	if !qh.Enabled || qh.Start == "" || qh.End == "" {
		return false
	}
	startHour, startMin, err1 := parseHHMM(qh.Start)
	endHour, endMin, err2 := parseHHMM(qh.End)
	if err1 != nil || err2 != nil {
		return false
	}

	local := now.In(quietHoursLocation)
	curMinute := local.Hour()*60 + local.Minute()
	startMinute := startHour*60 + startMin
	endMinute := endHour*60 + endMin

	if startMinute < endMinute {
		return curMinute >= startMinute && curMinute < endMinute
	}
	// 跨午夜：如 23:00–07:00
	return curMinute >= startMinute || curMinute < endMinute
}

// shouldMuteByQuietHours 免打扰时段内且等级不在穿透列表时静音。
func shouldMuteByQuietHours(cfg model.NotifyConfig, severity model.Severity) bool {
	if !isInQuietHours(cfg.QuietHours, time.Now()) {
		return false
	}
	for _, lvl := range cfg.QuietHours.AllowLevels {
		if lvl == severity {
			return false
		}
	}
	return true
}

// effectiveTargets 优先 Targets；空时由 ChatIDs 兼容包装。
func effectiveTargets(cfg model.NotifyConfig) []model.NotifyTarget {
	if len(cfg.Targets) > 0 {
		return cfg.Targets
	}
	if len(cfg.ChatIDs) == 0 {
		return nil
	}
	targets := make([]model.NotifyTarget, 0, len(cfg.ChatIDs))
	for _, id := range cfg.ChatIDs {
		targets = append(targets, model.NotifyTarget{
			ID:       id,
			Name:     id,
			ChatID:   id,
			Enabled:  true,
			MinLevel: model.SeverityInfo,
		})
	}
	return targets
}

// routeTargets 按免打扰、最低等级、分类订阅筛选接收目标。
// muted=true 表示被免打扰整体静音（调用方应跳过发送）。
func routeTargets(cfg model.NotifyConfig, severity model.Severity, category string) (targets []model.NotifyTarget, muted bool) {
	if shouldMuteByQuietHours(cfg, severity) {
		return nil, true
	}
	for _, t := range effectiveTargets(cfg) {
		if !t.Enabled || strings.TrimSpace(t.ChatID) == "" {
			continue
		}
		if !isLevelAllowed(t.MinLevel, severity) {
			continue
		}
		if category != "" && !isCategorySubscribed(t.SubscribedCategories, category) {
			continue
		}
		targets = append(targets, t)
	}
	return targets, false
}

// Dispatch 派发统一事件，根据 Severity、Category、QuietHours、Targets 订阅矩阵进行精准路由分发。
func Dispatch(ctx context.Context, evt model.NotifyEvent) {
	_ = ctx
	go safePublish(func() {
		cfg := GetConfig()
		if !cfg.Enabled {
			return
		}
		// 事件特定开关校验
		if evt.Key != "" && !eventEnabled(cfg, evt.Key) {
			return
		}

		// 限流控制
		if evt.Key != "" {
			interval := time.Duration(cfg.MinIntervalSec) * time.Second
			if evt.Key == model.NotifyEventCollectBatchSummary {
				interval = 0 // 摘要按批次自然频率
			}
			entityID := ""
			if id, ok := evt.Data["entity_id"].(string); ok {
				entityID = id
			}
			limitKey := evt.Key
			if entityID != "" {
				limitKey = evt.Key + ":" + entityID
			}
			if interval > 0 && !allowOrLog(limitKey, interval) {
				return
			}
		}

		// 格式化 HTML 内容
		htmlText := formatEventHTML(siteName(), evt)
		sendMessagesWithMarkup(cfg, evt.Severity, evt.Category, htmlText, nil)
	})
}

func sendMessagesWithMarkup(cfg model.NotifyConfig, severity model.Severity, category, text string, markup *InlineKeyboardMarkup) {
	targets, muted := routeTargets(cfg, severity, category)
	if muted {
		log.Printf("[Notify] muted by quiet hours severity=%s category=%s", severity, category)
		return
	}
	sendMessagesToTargets(cfg, targets, text, markup)
}

func sendMessagesToTargets(cfg model.NotifyConfig, targets []model.NotifyTarget, text string, markup *InlineKeyboardMarkup) {
	if strings.TrimSpace(text) == "" || len(targets) == 0 || strings.TrimSpace(cfg.BotToken) == "" {
		return
	}
	ctx, cancel := context.WithTimeout(context.Background(), 30*time.Second)
	defer cancel()
	var (
		wg       sync.WaitGroup
		failMu   sync.Mutex
		failN    int
		lastFail string
	)
	for _, target := range targets {
		if !target.Enabled || strings.TrimSpace(target.ChatID) == "" {
			continue
		}
		wg.Add(1)
		go func(t model.NotifyTarget) {
			defer wg.Done()
			sendSem <- struct{}{}
			defer func() { <-sendSem }()
			if err := client.sendMessageToTarget(ctx, cfg.BotToken, t.ChatID, t.ThreadID, text, markup); err != nil {
				failMu.Lock()
				failN++
				lastFail = err.Error()
				failMu.Unlock()
				syslog.Errorf("[Notify] Telegram 发送失败 target=%s chat=%s thread=%s err=%v", t.Name, t.ChatID, t.ThreadID, err)
			}
		}(target)
	}
	wg.Wait()
	if failN > 0 {
		log.Printf("[Notify] 本轮发送完成 targets=%d failures=%d last_err=%s",
			len(targets), failN, lastFail)
	}
}

func safePublish(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			syslog.Errorf("[Notify] 发送协程 panic: %v", r)
		}
	}()
	fn()
}

// BuildSourceResult 从进度与 Acc 计数组装 SourceNotifyResult。
// FilmsTotal=本源变更次数；明细 mid 在 MySQL 变更批次（跨源去重）。
func BuildSourceResult(source model.FilmSource, progress model.CollectProgress, errMsg string) model.SourceNotifyResult {
	status := strings.TrimSpace(progress.Status)
	if status == "" {
		status = "done"
	}
	_, total, _ := Acc.DrainSource(source.Id)
	return model.SourceNotifyResult{
		SourceID:    source.Id,
		SourceName:  source.Name,
		Grade:       int(source.Grade),
		Status:      status,
		Error:       errMsg,
		PageTotal:   progress.Total,
		PageCurrent: progress.Current,
		SuccessCnt:  progress.Success,
		FailedCnt:   progress.Failed,
		FilmsTotal:  total,
	}
}

// BuildSourceResultDirect 不依赖进度快照时组装结果（如单片更新）。
func BuildSourceResultDirect(source model.FilmSource, status, errMsg string) model.SourceNotifyResult {
	if status == "" {
		status = "done"
	}
	_, total, _ := Acc.DrainSource(source.Id)
	successCnt, failedCnt := 0, 0
	if status == "done" {
		successCnt = 1
	} else if status == "failed" {
		failedCnt = 1
	}
	return model.SourceNotifyResult{
		SourceID:   source.Id,
		SourceName: source.Name,
		Grade:      int(source.Grade),
		Status:     status,
		Error:      errMsg,
		SuccessCnt: successCnt,
		FailedCnt:  failedCnt,
		FilmsTotal: total,
	}
}

// BuildBatchPayload 组装批次摘要。
// TotalFilms 优先用批次去重条数，否则分源 FilmsTotal 之和；ChangeBatchID 随批次显式写入。
func BuildBatchPayload(batch *ChangeBatch, trigger string, sources []model.SourceNotifyResult, startedAt, finishedAt time.Time, finalizeErr string) model.CollectBatchNotifyPayload {
	if finishedAt.IsZero() {
		finishedAt = time.Now()
	}
	var duration int64
	if !startedAt.IsZero() {
		duration = int64(finishedAt.Sub(startedAt).Seconds())
		if duration < 0 {
			duration = 0
		}
	}
	success, failed, sumSource := 0, 0, 0
	for _, s := range sources {
		switch s.Status {
		case "failed", "stopped":
			failed++
		case "done":
			success++
		default:
			if s.Status != "" {
				failed++
			}
		}
		sumSource += s.FilmsTotal
	}
	filmTotal := sumSource
	if batch != nil {
		if n := batch.Count(); n > 0 {
			filmTotal = n
		}
	}
	return model.CollectBatchNotifyPayload{
		Trigger:            trigger,
		StartedAt:          startedAt,
		FinishedAt:         finishedAt,
		DurationSec:        duration,
		Sources:            sources,
		TotalSources:       len(sources),
		SuccessSources:     success,
		FailedSources:      failed,
		TotalFilms:         filmTotal,
		IncludeFilmDetails: true,
		FinalizeError:      finalizeErr,
		ChangeBatchID:      batch.ID(),
	}
}
