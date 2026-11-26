package notify

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/utils"
	"strings"
	"sync"
	"time"
)

// NotifyService Telegram 消息通知领域服务
type NotifyService struct {
	alertMu   sync.Mutex
	alertLast map[string]time.Time
}

// NotifySvc Telegram 通知服务全局单例
var NotifySvc = &NotifyService{
	alertLast: make(map[string]time.Time),
}

const alertCooldown = 60 * time.Second

// PublicConfig 获取脱敏后的通知配置（供管理后台展示）
func (s *NotifyService) PublicConfig() (model.NotifyConfig, error) {
	cfg, err := GetNotifyConfig()
	if err != nil {
		return model.NotifyConfig{}, err
	}
	return PublicNotifyConfig(cfg), nil
}

// UpdateConfig 校验并保存通知配置，同时自动重载轮询器
func (s *NotifyService) UpdateConfig(incoming model.NotifyConfig) (model.NotifyConfig, error) {
	stored, err := GetNotifyConfig()
	if err != nil {
		return model.NotifyConfig{}, err
	}
	merged, err := validateAndMergeNotifyConfig(stored, incoming)
	if err != nil {
		return model.NotifyConfig{}, err
	}
	if err := SaveNotifyConfig(merged); err != nil {
		return model.NotifyConfig{}, err
	}
	s.EnsureBotPoller()
	return PublicNotifyConfig(merged), nil
}

func validateAndMergeNotifyConfig(stored, incoming model.NotifyConfig) (model.NotifyConfig, error) {
	incoming.BotToken = strings.TrimSpace(incoming.BotToken)
	// 仅当提交的是脱敏占位串时保留原 Token；空字符串视为显式清空
	if IsMaskedBotToken(incoming.BotToken) {
		incoming.BotToken = stored.BotToken
	}
	incoming = NormalizeNotifyConfig(incoming)

	for _, cid := range incoming.ChatIDs {
		if _, _, err := parseChatTarget(cid); err != nil {
			return model.NotifyConfig{}, err
		}
	}
	if incoming.Enabled {
		if incoming.BotToken == "" {
			return model.NotifyConfig{}, errors.New("启用通知时 Bot Token 不能为空")
		}
		if len(incoming.ChatIDs) == 0 {
			return model.NotifyConfig{}, errors.New("启用通知时请至少配置一个 Chat ID")
		}
	}
	return incoming, nil
}

// SendTest 发送测试通知（支持传入未保存的草稿 Token 和 ChatIDs）
func (s *NotifyService) SendTest(req model.NotifyTestReq) (model.NotifyTestResult, error) {
	stored, err := GetNotifyConfig()
	if err != nil {
		return model.NotifyTestResult{}, err
	}
	token := strings.TrimSpace(req.BotToken)
	if token == "" || IsMaskedBotToken(token) {
		token = stored.BotToken
	}
	if token == "" {
		return model.NotifyTestResult{}, errors.New("请先填写有效的 Telegram Bot Token")
	}

	chatIDs := req.ChatIDs
	if len(chatIDs) == 0 {
		chatIDs = stored.ChatIDs
	}
	cleaned := make([]string, 0, len(chatIDs))
	seen := make(map[string]struct{}, len(chatIDs))
	for _, raw := range chatIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	if len(cleaned) == 0 {
		return model.NotifyTestResult{}, errors.New("请至少填写一个接收通知的 Chat ID")
	}

	basic, err := repobase.GetSiteBasic()
	if err != nil {
		return model.NotifyTestResult{}, err
	}
	siteName := basic.SiteName
	text := fmt.Sprintf(
		"<b>%s 通知通道测试</b>\n✅ Telegram 机器人连接正常\n🕒 时间：<code>%s</code>",
		formatTitlePrefix(siteName),
		time.Now().Format("2006-01-02 15:04:05"),
	)

	// 每个接收目标独立超时与有界并发发送，避免单个慢目标拖垮整批测试
	const maxSendConc = 5
	sem := make(chan struct{}, maxSendConc)
	errs := make([]error, len(cleaned))
	var wg sync.WaitGroup
	for i, cid := range cleaned {
		wg.Add(1)
		go func(idx int, target string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			errs[idx] = sendTelegramMessage(ctx, token, target, text, nil)
		}(i, cid)
	}
	wg.Wait()

	var res model.NotifyTestResult
	for i, cid := range cleaned {
		if errs[i] != nil {
			res.Failed = append(res.Failed, model.NotifyChatError{
				ChatID: cid,
				Error:  errs[i].Error(),
			})
		} else {
			res.Sent++
		}
	}
	if len(res.Failed) > 0 {
		return res, fmt.Errorf("测试发送存在失败（成功 %d，失败 %d）：%s", res.Sent, len(res.Failed), res.Failed[0].Error)
	}
	return res, nil
}

// PublishBatchSummary 异步构建并推送采集批次完成汇总（含各站分类与内联翻页）
func (s *NotifyService) PublishBatchSummary(payload model.CollectBatchNotifyPayload) {
	utils.GoSafe("NotifyBatchSummary", func() {
		cfg, err := GetNotifyConfig()
		if err != nil {
			syslog.Errorf("[Notify] 读取通知配置失败: %v", err)
			return
		}
		if !cfg.Enabled || !cfg.Events.CollectBatchSummary || cfg.BotToken == "" || len(cfg.ChatIDs) == 0 {
			return
		}

		failedSources := 0
		totalStored := 0
		for _, src := range payload.Sources {
			if src.Status == model.CollectStatusFailed || src.Error != "" {
				failedSources++
			}
			totalStored += src.FilmsTotal
		}
		if cfg.OnlyNotifyOnUpdate && totalStored == 0 && len(payload.NotifyMIDs) == 0 && failedSources == 0 {
			return
		}

		sess, visibleCount, err := buildNotifyBatchSessionWithCount(payload, cfg.MaxFilmsInMessage)
		if err != nil {
			syslog.Errorf("[Notify] 读取站点配置失败: %v", err)
			return
		}
		if cfg.OnlyNotifyOnUpdate && totalStored == 0 && visibleCount == 0 && failedSources == 0 {
			return
		}

		var markup *InlineKeyboardMarkup
		if len(sess.Sources) > 0 || len(sess.AllFilms) > 0 {
			_ = SaveNotifyBatchSession(sess)
			markup = buildOverviewKeyboard(sess, 1)
		}

		broadcastTelegram(cfg.BotToken, cfg.ChatIDs, sess.OverviewHTML, markup)
	})
}

// PublishSourceFailed 发送单源采集失败告警
func (s *NotifyService) PublishSourceFailed(sourceID, sourceName, reason string) {
	cfg, err := GetNotifyConfig()
	if err != nil {
		syslog.Errorf("[Notify] 读取通知配置失败: %v", err)
		return
	}
	if !cfg.Enabled || !cfg.Events.CollectSourceFailed || cfg.BotToken == "" || len(cfg.ChatIDs) == 0 {
		return
	}
	basic, err := repobase.GetSiteBasic()
	if err != nil {
		syslog.Errorf("[Notify] 读取站点配置失败: %v", err)
		return
	}
	if !s.allowAlert("src_fail:" + sourceID) {
		return
	}
	siteName := basic.SiteName
	text := fmt.Sprintf(
		"<b>%s 采集源异常告警</b>\n❌ 站点：<b>%s</b>\n⚠️ 原因：<code>%s</code>\n🕒 时间：<code>%s</code>",
		formatTitlePrefix(siteName),
		html.EscapeString(sourceName),
		html.EscapeString(truncateRunes(reason, 200)),
		time.Now().Format("2006-01-02 15:04:05"),
	)
	go broadcastTelegram(cfg.BotToken, cfg.ChatIDs, text, nil)
}

// PublishCronFailed 发送定时任务执行失败告警
func (s *NotifyService) PublishCronFailed(taskID, remark, reason string) {
	cfg, err := GetNotifyConfig()
	if err != nil {
		syslog.Errorf("[Notify] 读取通知配置失败: %v", err)
		return
	}
	if !cfg.Enabled || !cfg.Events.CronTaskFailed || cfg.BotToken == "" || len(cfg.ChatIDs) == 0 {
		return
	}
	basic, err := repobase.GetSiteBasic()
	if err != nil {
		syslog.Errorf("[Notify] 读取站点配置失败: %v", err)
		return
	}
	if !s.allowAlert("cron_fail:" + taskID) {
		return
	}
	siteName := basic.SiteName
	title := strings.TrimSpace(remark)
	if title == "" {
		title = taskID
	}
	text := fmt.Sprintf(
		"<b>%s 定时任务失败告警</b>\n📌 任务：<b>%s</b>\n⚠️ 原因：%s\n🕒 时间：<code>%s</code>",
		formatTitlePrefix(siteName),
		html.EscapeString(singleLine(title)),
		formatReasonCodeLines(reason, cronReasonLineMax, cronReasonTotalMax),
		time.Now().Format("2006-01-02 15:04:05"),
	)
	go broadcastTelegram(cfg.BotToken, cfg.ChatIDs, text, nil)
}

// PublishPrimaryChange 发送主站切换通知。
// 配置字段沿用 SourceOrderChange 命名，保证已保存的通知配置兼容（D7）。
func (s *NotifyService) PublishPrimaryChange(oldPrimary, newPrimary *model.FilmSource, enabledSources []model.FilmSource) {
	cfg, err := GetNotifyConfig()
	if err != nil {
		syslog.Errorf("[Notify] 读取通知配置失败: %v", err)
		return
	}
	if !cfg.Enabled || !cfg.Events.SourceOrderChange || cfg.BotToken == "" || len(cfg.ChatIDs) == 0 {
		return
	}
	basic, err := repobase.GetSiteBasic()
	if err != nil {
		syslog.Errorf("[Notify] 读取站点配置失败: %v", err)
		return
	}
	siteName := basic.SiteName
	text := formatPrimaryChangeHTML(siteName, oldPrimary, newPrimary, enabledSources)
	go broadcastTelegram(cfg.BotToken, cfg.ChatIDs, text, nil)
}

// PublishSourceOrderChange 发送采集站排序调整通知（复用 SourceOrderChange 事件开关）。
func (s *NotifyService) PublishSourceOrderChange(enabledSources []model.FilmSource) {
	cfg, err := GetNotifyConfig()
	if err != nil {
		syslog.Errorf("[Notify] 读取通知配置失败: %v", err)
		return
	}
	if !cfg.Enabled || !cfg.Events.SourceOrderChange || cfg.BotToken == "" || len(cfg.ChatIDs) == 0 {
		return
	}
	basic, err := repobase.GetSiteBasic()
	if err != nil {
		syslog.Errorf("[Notify] 读取站点配置失败: %v", err)
		return
	}
	text := formatSourceOrderChangeHTML(basic.SiteName, enabledSources)
	go broadcastTelegram(cfg.BotToken, cfg.ChatIDs, text, nil)
}

func formatEnabledOrderLine(enabledSources []model.FilmSource) string {
	var orderNames []string
	for i, src := range enabledSources {
		name := strings.TrimSpace(src.Name)
		if name == "" {
			name = src.Id
		}
		if i == 0 {
			orderNames = append(orderNames, "<b>"+html.EscapeString(singleLine(name))+"</b>")
		} else {
			orderNames = append(orderNames, html.EscapeString(singleLine(name)))
		}
	}
	if len(orderNames) == 0 {
		return "无"
	}
	return strings.Join(orderNames, " → ")
}

func formatSourceOrderChangeHTML(siteName string, enabledSources []model.FilmSource) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 采集站排序已调整</b>\n", formatTitlePrefix(siteName))
	fmt.Fprintf(&b, "📋 启用顺序：%s\n", formatEnabledOrderLine(enabledSources))
	fmt.Fprintf(&b, "🕒 时间：<code>%s</code>", time.Now().Format("2006-01-02 15:04:05"))
	return b.String()
}

func formatPrimaryChangeHTML(siteName string, oldPrimary, newPrimary *model.FilmSource, enabledSources []model.FilmSource) string {
	oldName := "无"
	if oldPrimary != nil {
		if name := strings.TrimSpace(oldPrimary.Name); name != "" {
			oldName = name
		}
	}
	newName := "无"
	if newPrimary != nil {
		if name := strings.TrimSpace(newPrimary.Name); name != "" {
			newName = name
		}
	}
	orderLine := formatEnabledOrderLine(enabledSources)

	var b strings.Builder
	if newPrimary == nil {
		fmt.Fprintf(&b, "<b>%s 主站已置空</b>\n", formatTitlePrefix(siteName))
		fmt.Fprintf(&b, "⚠️ 主站：<b>%s</b> → 无\n", html.EscapeString(singleLine(oldName)))
	} else {
		fmt.Fprintf(&b, "<b>%s 主站已切换</b>\n", formatTitlePrefix(siteName))
		fmt.Fprintf(&b, "🔄 主站：<b>%s</b> → <b>%s</b>\n", html.EscapeString(singleLine(oldName)), html.EscapeString(singleLine(newName)))
	}
	fmt.Fprintf(&b, "📋 启用顺序：%s\n", orderLine)
	fmt.Fprintf(&b, "🕒 时间：<code>%s</code>", time.Now().Format("2006-01-02 15:04:05"))
	return b.String()
}

func (s *NotifyService) allowAlert(key string) bool {
	now := time.Now()
	s.alertMu.Lock()
	defer s.alertMu.Unlock()
	if last, ok := s.alertLast[key]; ok && now.Sub(last) < alertCooldown {
		return false
	}
	s.alertLast[key] = now
	if len(s.alertLast) > 64 {
		for k, t := range s.alertLast {
			if now.Sub(t) >= alertCooldown {
				delete(s.alertLast, k)
			}
		}
	}
	return true
}

func broadcastTelegram(token string, chatIDs []string, htmlText string, markup *InlineKeyboardMarkup) {
	const maxSendConc = 5
	sem := make(chan struct{}, maxSendConc)
	var wg sync.WaitGroup
	for _, cid := range chatIDs {
		wg.Add(1)
		go func(target string) {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()
			ctx, cancel := context.WithTimeout(context.Background(), 15*time.Second)
			defer cancel()
			if err := sendTelegramMessage(ctx, token, target, htmlText, markup); err != nil {
				log.Printf("[Notify] 发送 Telegram 消息失败 chat=%s err=%v", target, err)
			}
		}(cid)
	}
	wg.Wait()
}

// buildNotifyBatchSession 按前台「每日更新」同源分类树与排序规则构建批次翻页会话
