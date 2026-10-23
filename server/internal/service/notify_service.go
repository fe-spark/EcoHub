package service

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/film"
	"server/internal/repository/support"
	"server/internal/spider"
	"server/internal/spider/progress"
	"server/internal/utils"
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

func init() {
	spider.RegisterNotifyDispatcher(spider.NotifyDispatcher{
		OnBatchSummary: func(payload model.CollectBatchNotifyPayload) {
			NotifySvc.PublishBatchSummary(payload)
		},
		OnSourceFailed: func(sourceID, sourceName, reason string) {
			NotifySvc.PublishSourceFailed(sourceID, sourceName, reason)
		},
		OnCronFailed: func(taskID, remark, reason string) {
			NotifySvc.PublishCronFailed(taskID, remark, reason)
		},
	})
}

// GetConfig 获取完整通知配置（内部使用，含明文 BotToken）
func (s *NotifyService) GetConfig() (model.NotifyConfig, error) {
	return repository.GetNotifyConfig()
}

// PublicConfig 获取脱敏后的通知配置（供管理后台展示）
func (s *NotifyService) PublicConfig() (model.NotifyConfig, error) {
	cfg, err := repository.GetNotifyConfig()
	if err != nil {
		return model.NotifyConfig{}, err
	}
	return repository.PublicNotifyConfig(cfg), nil
}

// UpdateConfig 校验并保存通知配置，同时自动重载轮询器
func (s *NotifyService) UpdateConfig(incoming model.NotifyConfig) (model.NotifyConfig, error) {
	stored, err := repository.GetNotifyConfig()
	if err != nil {
		return model.NotifyConfig{}, err
	}
	merged, err := validateAndMergeNotifyConfig(stored, incoming)
	if err != nil {
		return model.NotifyConfig{}, err
	}
	if err := repository.SaveNotifyConfig(merged); err != nil {
		return model.NotifyConfig{}, err
	}
	s.EnsureBotPoller()
	return repository.PublicNotifyConfig(merged), nil
}

func validateAndMergeNotifyConfig(stored, incoming model.NotifyConfig) (model.NotifyConfig, error) {
	incoming.BotToken = strings.TrimSpace(incoming.BotToken)
	if incoming.BotToken == "" || repository.IsMaskedBotToken(incoming.BotToken) {
		incoming.BotToken = stored.BotToken
	}
	incoming = repository.NormalizeNotifyConfig(incoming)

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
	stored, err := repository.GetNotifyConfig()
	if err != nil {
		return model.NotifyTestResult{}, err
	}
	token := strings.TrimSpace(req.BotToken)
	if token == "" || repository.IsMaskedBotToken(token) {
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

	basic, err := repository.GetSiteBasic()
	if err != nil {
		return model.NotifyTestResult{}, err
	}
	siteName := basic.SiteName
	text := fmt.Sprintf(
		"<b>%s 通知通道测试</b>\n✅ Telegram 机器人连接正常\n🕒 时间：<code>%s</code>",
		formatTitlePrefix(siteName),
		time.Now().Format("2006-01-02 15:04:05"),
	)

	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()

	var res model.NotifyTestResult
	for _, cid := range cleaned {
		if err := sendTelegramMessage(ctx, token, cid, text, nil); err != nil {
			res.Failed = append(res.Failed, model.NotifyChatError{
				ChatID: cid,
				Error:  err.Error(),
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

// PublishBatchSummary 异步构建并推送采集批次完成汇总（含与前台同源的分类更新片单及内联翻页）
func (s *NotifyService) PublishBatchSummary(payload model.CollectBatchNotifyPayload) {
	cfg, err := repository.GetNotifyConfig()
	if err != nil {
		syslog.Errorf("[Notify] 读取通知配置失败: %v", err)
		return
	}
	if !cfg.Enabled || !cfg.Events.CollectBatchSummary || cfg.BotToken == "" || len(cfg.ChatIDs) == 0 {
		return
	}

	failedSources := 0
	for _, src := range payload.Sources {
		if src.Status == progress.StatusFailed || src.Error != "" {
			failedSources++
		}
	}

	sess, err := buildNotifyBatchSession(payload, cfg.MaxFilmsInMessage)
	if err != nil {
		syslog.Errorf("[Notify] 读取站点配置失败: %v", err)
		return
	}
	if cfg.OnlyNotifyOnUpdate && len(sess.AllFilms) == 0 && failedSources == 0 {
		return
	}

	var markup *InlineKeyboardMarkup
	if len(sess.AllFilms) > 0 {
		_ = repository.SaveNotifyBatchSession(sess)
		markup, err = buildCategoryKeyboard(sess.Categories, len(sess.AllFilms))
		if err != nil {
			syslog.Errorf("[Notify] 读取站点配置失败: %v", err)
			return
		}
	}

	go broadcastTelegram(cfg.BotToken, cfg.ChatIDs, sess.OverviewHTML, markup)
}

// PublishSourceFailed 发送单源连续失败终止告警
func (s *NotifyService) PublishSourceFailed(sourceID, sourceName, reason string) {
	cfg, err := repository.GetNotifyConfig()
	if err != nil {
		syslog.Errorf("[Notify] 读取通知配置失败: %v", err)
		return
	}
	if !cfg.Enabled || !cfg.Events.CollectSourceFailed || cfg.BotToken == "" || len(cfg.ChatIDs) == 0 {
		return
	}
	if !s.allowAlert("src_fail:" + sourceID) {
		return
	}
	basic, err := repository.GetSiteBasic()
	if err != nil {
		syslog.Errorf("[Notify] 读取站点配置失败: %v", err)
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
	cfg, err := repository.GetNotifyConfig()
	if err != nil {
		syslog.Errorf("[Notify] 读取通知配置失败: %v", err)
		return
	}
	if !cfg.Enabled || !cfg.Events.CronTaskFailed || cfg.BotToken == "" || len(cfg.ChatIDs) == 0 {
		return
	}
	if !s.allowAlert("cron_fail:" + taskID) {
		return
	}
	basic, err := repository.GetSiteBasic()
	if err != nil {
		syslog.Errorf("[Notify] 读取站点配置失败: %v", err)
		return
	}
	siteName := basic.SiteName
	title := strings.TrimSpace(remark)
	if title == "" {
		title = taskID
	}
	text := fmt.Sprintf(
		"<b>%s 定时任务失败告警</b>\n📌 任务：<b>%s</b>\n⚠️ 原因：<code>%s</code>\n🕒 时间：<code>%s</code>",
		formatTitlePrefix(siteName),
		html.EscapeString(title),
		html.EscapeString(truncateRunes(reason, 200)),
		time.Now().Format("2006-01-02 15:04:05"),
	)
	go broadcastTelegram(cfg.BotToken, cfg.ChatIDs, text, nil)
}

// PublishSourceOrderChange 发送采集站排序更新或首选站切换通知
func (s *NotifyService) PublishSourceOrderChange(oldPrimary, newPrimary *model.FilmSource, enabledSources []model.FilmSource) {
	cfg, err := repository.GetNotifyConfig()
	if err != nil {
		syslog.Errorf("[Notify] 读取通知配置失败: %v", err)
		return
	}
	if !cfg.Enabled || !cfg.Events.SourceOrderChange || cfg.BotToken == "" || len(cfg.ChatIDs) == 0 {
		return
	}
	basic, err := repository.GetSiteBasic()
	if err != nil {
		syslog.Errorf("[Notify] 读取站点配置失败: %v", err)
		return
	}
	siteName := basic.SiteName
	text := formatSourceOrderChangeHTML(siteName, oldPrimary, newPrimary, enabledSources)
	go broadcastTelegram(cfg.BotToken, cfg.ChatIDs, text, nil)
}

func formatSourceOrderChangeHTML(siteName string, oldPrimary, newPrimary *model.FilmSource, enabledSources []model.FilmSource) string {
	oldID, oldName := "", "无"
	if oldPrimary != nil {
		oldID = oldPrimary.Id
		if name := strings.TrimSpace(oldPrimary.Name); name != "" {
			oldName = name
		}
	}
	newID, newName := "", "无"
	if newPrimary != nil {
		newID = newPrimary.Id
		if name := strings.TrimSpace(newPrimary.Name); name != "" {
			newName = name
		}
	}

	var orderNames []string
	for i, src := range enabledSources {
		name := strings.TrimSpace(src.Name)
		if name == "" {
			name = src.Id
		}
		if i == 0 {
			orderNames = append(orderNames, "<b>"+html.EscapeString(name)+"</b>")
		} else {
			orderNames = append(orderNames, html.EscapeString(name))
		}
	}
	orderLine := "无"
	if len(orderNames) > 0 {
		orderLine = strings.Join(orderNames, " → ")
	}

	var b strings.Builder
	if oldID != newID {
		fmt.Fprintf(&b, "<b>%s 主站已切换</b>\n", formatTitlePrefix(siteName))
		fmt.Fprintf(&b, "🔄 主站：<b>%s</b> → <b>%s</b>\n", html.EscapeString(oldName), html.EscapeString(newName))
	} else {
		fmt.Fprintf(&b, "<b>%s 采集站排序已更新</b>\n", formatTitlePrefix(siteName))
		fmt.Fprintf(&b, "⭐ 主站：<b>%s</b>\n", html.EscapeString(newName))
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
	ctx, cancel := context.WithTimeout(context.Background(), 20*time.Second)
	defer cancel()
	for _, cid := range chatIDs {
		if err := sendTelegramMessage(ctx, token, cid, htmlText, markup); err != nil {
			log.Printf("[Notify] 发送 Telegram 消息失败 chat=%s err=%v", cid, err)
		}
	}
}

// buildNotifyBatchSession 按前台「每日更新」同源分类树与排序规则构建批次翻页会话
func buildNotifyBatchSession(payload model.CollectBatchNotifyPayload, pageSize int) (model.NotifyBatchSession, error) {
	if pageSize <= 0 {
		pageSize = model.DefaultNotifyPageSize
	} else if pageSize > model.MaxNotifyPageSize {
		pageSize = model.MaxNotifyPageSize
	}
	basic, err := repository.GetSiteBasic()
	if err != nil {
		return model.NotifyBatchSession{}, err
	}
	siteName := basic.SiteName
	metas, err := repository.LoadNotifyFilmMetas(payload.NotifyMIDs)
	if err != nil {
		return model.NotifyBatchSession{}, err
	}

	var activeSourceID string
	if active := repository.GetActiveCollectSource(); active != nil {
		activeSourceID = strings.TrimSpace(active.Id)
	}
	nav, err := film.NavTopCategories(activeSourceID)
	if err != nil {
		return model.NotifyBatchSession{}, err
	}
	navSet := make(map[int64]struct{}, len(nav))
	keyToRoot := make(map[string]int64, len(nav)*4)
	for _, c := range nav {
		if c.Id <= 0 {
			continue
		}
		navSet[c.Id] = struct{}{}
		if activeSourceID != "" {
			for _, typeID := range repository.PublicSourceTypeIDs(activeSourceID, "pid", c.Id) {
				if k := support.BuildSourceCategoryKey(activeSourceID, typeID); k != "" {
					keyToRoot[k] = c.Id
				}
			}
		}
	}

	allFilms := make([]model.NotifyFilmItem, 0, len(metas))
	buckets := make(map[int64][]model.NotifyFilmItem, len(nav))
	var others []model.NotifyFilmItem

	for _, m := range metas {
		item := model.NotifyFilmItem{Mid: m.Mid, Name: m.Name}
		allFilms = append(allFilms, item)
		if rootID, ok := keyToRoot[m.CategoryKey]; ok && rootID > 0 {
			buckets[rootID] = append(buckets[rootID], item)
			continue
		}
		if m.Pid > 0 {
			if _, ok := navSet[m.Pid]; ok {
				buckets[m.Pid] = append(buckets[m.Pid], item)
				continue
			}
		}
		others = append(others, item)
	}

	categories := make([]model.NotifyCategoryGroup, 0, len(nav)+1)
	for _, c := range nav {
		if list := buckets[c.Id]; len(list) > 0 {
			categories = append(categories, model.NotifyCategoryGroup{
				CategoryID:   c.Id,
				CategoryName: c.Name,
				Films:        list,
			})
		}
	}
	if len(others) > 0 {
		categories = append(categories, model.NotifyCategoryGroup{
			CategoryID:   0,
			CategoryName: "其他",
			Films:        others,
		})
	}

	batchID, err := utils.GenerateSalt()
	if err != nil {
		return model.NotifyBatchSession{}, err
	}
	if len(batchID) > 10 {
		batchID = batchID[:10]
	}
	overview := formatBatchOverviewHTML(siteName, payload, len(allFilms))

	return model.NotifyBatchSession{
		BatchID:      batchID,
		SiteName:     siteName,
		OverviewHTML: overview,
		PageSize:     pageSize,
		Categories:   categories,
		AllFilms:     allFilms,
	}, nil
}
