package notify

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
)

var (
	sendSem = make(chan struct{}, 4)
	client  = newTelegramClient()
)

// SourceConfigChangeItem 批量源配置变更通知项（结构定义见 model 包，此处导出别名）。
type SourceConfigChangeItem = model.SourceConfigChangeItem

func allowOrLog(key string, minInterval time.Duration) bool {
	if globalRate.allow(key, minInterval) {
		return true
	}
	log.Printf("[Notify] rate-limited key=%s interval=%s", key, minInterval)
	return false
}

// PublishBatchSummary 异步发送采集批次摘要（不走 MinInterval）。
// 批次由 BuildBatchPayload 在同步阶段写入 payload.ChangeBatchID，异步发送不再读全局状态。
func PublishBatchSummary(payload model.CollectBatchNotifyPayload) {
	safePublish(func() {
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

// sendBatchSummary 发送采集批次摘要。
func sendBatchSummary(cfg model.NotifyConfig, payload model.CollectBatchNotifyPayload) {
	pageSize := clampPageSize(cfg.MaxFilmsInMessage)
	listN := payload.TotalFilms

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
	overview := formatBatchOverview(payload, listN, pageSize)

	// 若开启影片明细且有更新影片，生成分类会话并在消息尾部挂载分类入口键盘
	if payload.IncludeFilmDetails && listN > 0 && len(payload.Films) > 0 {
		items := make([]ChangeMidItem, 0, len(payload.Films))
		for _, f := range payload.Films {
			items = append(items, ChangeMidItem{Mid: f.Mid, SourceName: f.SourceName})
		}
		cats, catMids, err := BuildCategoryPlanForMids(items)
		if err != nil {
			syslog.Errorf("[Notify] 计算批次分类计划失败: %v", err)
		}
		sess := FilmBatchSession{
			BatchID:      payload.ChangeBatchID,
			SiteName:     payload.SiteName,
			PageSize:     pageSize,
			OverviewText: overview,
			Total:        listN,
			AllItems:     items,
			Cats:         cats,
			CatMids:      catMids,
		}
		if err := SaveChangeBatchSession(sess); err != nil {
			syslog.Errorf("[Notify] 保存变更批次会话失败: %v", err)
		}

		parts := splitTelegramMessages(overview)
		markup := buildCategoryKeyboard(callbackPrefix, payload.ChangeBatchID, cats)
		for i, part := range parts {
			var btnMarkup *InlineKeyboardMarkup
			if i == len(parts)-1 {
				btnMarkup = markup
			}
			sendMessagesWithMarkup(cfg, severity, model.CategoryCollect, part, btnMarkup)
		}
		return
	}

	sendMessages(cfg, severity, model.CategoryCollect, splitTelegramMessages(overview))
}

// PublishSourceFailed 单源失败即时告警。
func PublishSourceFailed(sourceID, sourceName, reason string) {
	safePublish(func() {
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
	safePublish(func() {
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
	safePublish(func() {
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
	safePublish(func() {
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
	safePublish(func() {
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
	safePublish(func() {
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
	safePublish(func() {
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

// Dispatch 派发统一事件，根据 Severity、Category、QuietHours、Targets 订阅矩阵进行精准路由分发。
func Dispatch(ctx context.Context, evt model.NotifyEvent) {
	_ = ctx
	safePublish(func() {
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

var publishWg sync.WaitGroup

func safePublish(fn func()) {
	publishWg.Add(1)
	go func() {
		defer publishWg.Done()
		defer func() {
			if r := recover(); r != nil {
				syslog.Errorf("[Notify] 发送协程 panic: %v", r)
			}
		}()
		fn()
	}()
}

// WaitPendingPublishes 等待在途的异步通知协程完成，支持 context 超时控制。
func WaitPendingPublishes(ctx context.Context) error {
	done := make(chan struct{})
	go func() {
		publishWg.Wait()
		close(done)
	}()
	select {
	case <-done:
		return nil
	case <-ctx.Done():
		return ctx.Err()
	}
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
