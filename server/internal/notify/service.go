package notify

import (
	"context"
	"fmt"
	"log"
	"regexp"
	"strings"
	"sync"
	"time"

	"server/internal/model"
	"server/internal/repository"
)

var (
	// 数字 chat id（含负数群组）或 @username
	chatIDPattern = regexp.MustCompile(`^(-?\d+|@[A-Za-z0-9_]{5,})$`)
	sendSem       = make(chan struct{}, 4)
	client        = newTelegramClient()
)

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
	for _, id := range chatIDs {
		if !chatIDPattern.MatchString(id) {
			return model.NotifyConfig{}, fmt.Errorf("无效的 Chat ID: %s", id)
		}
	}
	cfg.ChatIDs = chatIDs

	if cfg.MaxFilmsInMessage <= 0 {
		cfg.MaxFilmsInMessage = 30
	}
	if cfg.MaxFilmsInMessage > 80 {
		return model.NotifyConfig{}, fmt.Errorf("maxFilmsInMessage 范围为 1-80")
	}
	if cfg.MinIntervalSec < 0 || cfg.MinIntervalSec > 3600 {
		return model.NotifyConfig{}, fmt.Errorf("minIntervalSec 范围为 0-3600")
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
	default:
		return false
	}
}

// IsEventEnabled 读取当前配置判断某事件是否开启（总开关 + 子开关）。
func IsEventEnabled(event string) bool {
	return eventEnabled(GetConfig(), event)
}

// PublishBatchSummary 异步发送采集批次摘要。
// 每批摘要不走 MinInterval 限流：合法批次不得因同 trigger 被静默丢弃。
func PublishBatchSummary(payload model.CollectBatchNotifyPayload) {
	go safePublish(func() {
		cfg := GetConfig()
		if !eventEnabled(cfg, model.NotifyEventCollectBatchSummary) {
			return
		}
		payload.SiteName = siteName()
		if payload.Trigger == model.NotifyTriggerSingleUpdate {
			payload.IncludeFilmDetails = false
		} else {
			payload.IncludeFilmDetails = payload.IncludeFilmDetails && cfg.IncludeFilmDetails
		}
		// 补全影片名
		if payload.IncludeFilmDetails {
			enrichFilmNames(&payload)
		} else {
			for i := range payload.Sources {
				payload.Sources[i].Films = nil
			}
		}
		sendBatchSummaryWithPager(cfg, payload)
	})
}

// sendBatchSummaryWithPager 发送总览 + 带「上一页/下一页」按钮的影片列表（单条可翻页）。
func sendBatchSummaryWithPager(cfg model.NotifyConfig, payload model.CollectBatchNotifyPayload) {
	pageSize := cfg.MaxFilmsInMessage
	if pageSize <= 0 {
		pageSize = 30
	}
	if pageSize > 80 {
		pageSize = 80
	}

	sess := buildFilmPageSessionFromPayload(payload, pageSize)
	overview := formatBatchOverview(payload, len(sess.Items), pageSize)
	// 总览可能较长，仍按 4096 拆分（无按钮）
	sendMessages(cfg, splitTelegramMessages(overview))

	if !payload.IncludeFilmDetails || len(sess.Items) == 0 {
		return
	}

	sessionID, err := saveFilmPageSession(sess)
	if err != nil {
		log.Printf("[Notify] 保存影片分页会话失败: %v", err)
		// 降级：只发第一页纯文本
		sendMessages(cfg, []string{formatFilmListPage(sess, 1)})
		return
	}
	totalPages := sess.totalPages()
	text := formatFilmListPage(sess, 1)
	markup := buildPageKeyboard(sessionID, 1, totalPages)
	sendMessagesWithMarkup(cfg, text, markup)
	// 确保 bot 在处理回调
	EnsureBotPoller()
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
		sendMessages(cfg, messages)
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
		sendMessages(cfg, messages)
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
		sendMessages(cfg, messages)
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
		sendMessages(cfg, messages)
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
		sendMessages(cfg, messages)
	})
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, chatID := range ids {
		if err := client.sendMessage(ctx, token, chatID, text); err != nil {
			result.Failed = append(result.Failed, model.NotifyChatError{
				ChatID: chatID,
				Error:  err.Error(),
			})
			log.Printf("[Notify] 测试发送失败 chat=%s err=%v", chatID, err)
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

func enrichFilmNames(payload *model.CollectBatchNotifyPayload) {
	all := make([]int64, 0)
	for _, src := range payload.Sources {
		for _, f := range src.Films {
			all = append(all, f.Mid)
		}
	}
	names := ResolveFilmNames(all)
	for i := range payload.Sources {
		for j := range payload.Sources[i].Films {
			mid := payload.Sources[i].Films[j].Mid
			payload.Sources[i].Films[j].Name = filmDisplayName(mid, names)
		}
	}
}

func sendMessages(cfg model.NotifyConfig, messages []string) {
	for _, msg := range messages {
		sendMessagesWithMarkup(cfg, msg, nil)
	}
}

func sendMessagesWithMarkup(cfg model.NotifyConfig, text string, markup *InlineKeyboardMarkup) {
	if strings.TrimSpace(text) == "" || len(cfg.ChatIDs) == 0 || strings.TrimSpace(cfg.BotToken) == "" {
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
	for _, chatID := range cfg.ChatIDs {
		wg.Add(1)
		go func(chatID string) {
			defer wg.Done()
			sendSem <- struct{}{}
			defer func() { <-sendSem }()
			if err := client.sendMessageWithMarkup(ctx, cfg.BotToken, chatID, text, markup); err != nil {
				failMu.Lock()
				failN++
				lastFail = err.Error()
				failMu.Unlock()
				log.Printf("[Notify] Telegram 发送失败 chat=%s err=%v", chatID, err)
			}
		}(chatID)
	}
	wg.Wait()
	if failN > 0 {
		log.Printf("[Notify] 本轮发送完成 chats=%d failures=%d last_err=%s",
			len(cfg.ChatIDs), failN, lastFail)
	}
}

func safePublish(fn func()) {
	defer func() {
		if r := recover(); r != nil {
			log.Printf("[Notify] 发送协程 panic: %v", r)
		}
	}()
	fn()
}

// BuildSourceResult 从进度与 mid 累计组装 SourceNotifyResult。
// FilmsTotal 仅来自 mid 累计，不用页成功数冒充影片数。
func BuildSourceResult(source model.FilmSource, progress model.CollectProgress, errMsg string) model.SourceNotifyResult {
	status := strings.TrimSpace(progress.Status)
	if status == "" {
		status = "done"
	}
	mids, total, truncated := Acc.DrainSource(source.Id)
	films := make([]model.FilmNotifyItem, 0, len(mids))
	for _, mid := range mids {
		films = append(films, model.FilmNotifyItem{Mid: mid})
	}
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
		Films:       films,
		FilmsTotal:  total,
		FilmsTrunc:  truncated,
	}
}

// BuildSourceResultDirect 不依赖进度快照时组装结果（如单片更新）。
func BuildSourceResultDirect(source model.FilmSource, status, errMsg string) model.SourceNotifyResult {
	if status == "" {
		status = "done"
	}
	mids, total, truncated := Acc.DrainSource(source.Id)
	films := make([]model.FilmNotifyItem, 0, len(mids))
	for _, mid := range mids {
		films = append(films, model.FilmNotifyItem{Mid: mid})
	}
	successCnt, failedCnt := 0, 0
	if status == "done" {
		successCnt = 1
		if total == 0 {
			total = 1
		}
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
		Films:      films,
		FilmsTotal: total,
		FilmsTrunc: truncated,
	}
}

// BuildBatchPayload 组装批次摘要。
func BuildBatchPayload(trigger string, sources []model.SourceNotifyResult, startedAt, finishedAt time.Time, finalizeErr string) model.CollectBatchNotifyPayload {
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
	success, failed, filmTotal := 0, 0, 0
	seenFilm := make(map[int64]struct{})
	for _, s := range sources {
		switch s.Status {
		case "failed", "stopped":
			// stopped 计入失败侧，避免头行 ✅/❌ 与列表条数对不上
			failed++
		case "done":
			success++
		default:
			// starting/running 等异常残留按失败统计
			if s.Status != "" {
				failed++
			}
		}
		filmTotal += s.FilmsTotal
		for _, f := range s.Films {
			seenFilm[f.Mid] = struct{}{}
		}
	}
	uniqFilms := filmTotal
	if len(seenFilm) > 0 {
		uniqFilms = len(seenFilm)
		// 若有截断，仍以 FilmsTotal 之和为准更贴近实际
		sum := 0
		for _, s := range sources {
			sum += s.FilmsTotal
		}
		if sum > uniqFilms {
			uniqFilms = sum
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
		TotalFilms:         uniqFilms,
		IncludeFilmDetails: true,
		FinalizeError:      finalizeErr,
	}
}

