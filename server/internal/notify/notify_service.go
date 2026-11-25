package notify

import (
	"context"
	"errors"
	"fmt"
	"html"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"server/internal/infra/syslog"
	"server/internal/film/catalog"
	"server/internal/model"
	"server/internal/film"
	"server/internal/film/snapshot"
	"server/internal/repobase"
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


// GetConfig 获取完整通知配置（内部使用，含明文 BotToken）
func (s *NotifyService) GetConfig() (model.NotifyConfig, error) {
	return GetNotifyConfig()
}

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
func buildNotifyBatchSession(payload model.CollectBatchNotifyPayload, pageSize int) (model.NotifyBatchSession, error) {
	sess, _, err := buildNotifyBatchSessionWithCount(payload, pageSize)
	return sess, err
}

func buildNotifyBatchSessionWithCount(payload model.CollectBatchNotifyPayload, pageSize int) (model.NotifyBatchSession, int, error) {
	if pageSize <= 0 {
		pageSize = model.DefaultNotifyPageSize
	} else if pageSize > model.MaxNotifyPageSize {
		pageSize = model.MaxNotifyPageSize
	}
	basic, err := repobase.GetSiteBasic()
	if err != nil {
		return model.NotifyBatchSession{}, 0, err
	}
	siteName := basic.SiteName

	var activeSourceID, activeSourceName string
	if active := repobase.CurrentPrimarySource(); active != nil {
		activeSourceID = strings.TrimSpace(active.Id)
		activeSourceName = strings.TrimSpace(active.Name)
		if activeSourceName == "" {
			activeSourceName = activeSourceID
		}
	}

	collectBatchID := strings.TrimSpace(payload.CollectBatchID)
	useLedger := false
	var statsBySource map[string][]BatchSourceCategoryStatRow
	if collectBatchID != "" {
		hasLedger, err := HasCollectBatchLedger(collectBatchID)
		if err != nil {
			return model.NotifyBatchSession{}, 0, err
		}
		if hasLedger {
			statsRows, err := LoadBatchSourceCategoryStats(collectBatchID)
			if err != nil {
				return model.NotifyBatchSession{}, 0, err
			}
			useLedger = true
			statsBySource = make(map[string][]BatchSourceCategoryStatRow)
			for _, r := range statsRows {
				sid := strings.TrimSpace(r.SourceID)
				statsBySource[sid] = append(statsBySource[sid], r)
			}
		}
	}

	var (
		metaByMid map[int64]NotifyFilmMeta
		orderRank map[int64]int
	)
	if !(useLedger && len(payload.Sources) > 0) {
		uniqueMIDs := collectBatchUniqueMIDs(payload)
		metaByMid = make(map[int64]NotifyFilmMeta, len(uniqueMIDs))
		orderRank = make(map[int64]int, len(uniqueMIDs))
		if len(uniqueMIDs) > 0 {
			metas, err := LoadNotifyFilmMetas(uniqueMIDs)
			if err != nil {
				return model.NotifyBatchSession{}, 0, err
			}
			for idx, m := range metas {
				metaByMid[m.Mid] = m
				orderRank[m.Mid] = idx
			}
		}
	}

	// 有 Sources 时每个站点已各自保存轻量化计数或 ID 列表，不再把主站全量片名重复写入 AllFilms/Categories
	var (
		allFilms     []model.NotifyFilmItem
		categories   []model.NotifyCategoryGroup
		visibleCount int
	)
	if len(payload.Sources) > 0 {
		if useLedger {
			visibleCount = countPrimaryVisibleFromStats(activeSourceID, statsBySource[activeSourceID])
		} else {
			visibleCount = len(filterPrimaryVisibleMetas(activeSourceID, payload.NotifyMIDs, metaByMid, orderRank))
		}
	} else {
		allFilms, categories, err = buildPrimaryVisibleGroups(activeSourceID, payload.NotifyMIDs, metaByMid, orderRank)
		if err != nil {
			return model.NotifyBatchSession{}, 0, err
		}
		visibleCount = len(allFilms)
	}

	sources := make([]model.NotifySourceSession, 0, len(payload.Sources))
	for _, src := range payload.Sources {
		srcID := strings.TrimSpace(src.SourceID)
		isPrimary := activeSourceID != "" && srcID == activeSourceID
		var (
			srcCats     []model.NotifySourceCategoryGroup
			srcAllMids  []int64
			srcAllCount int
		)
		if useLedger {
			srcCats, srcAllCount, err = buildSourceCategoryCountsFromStats(srcID, statsBySource[srcID])
		} else {
			srcMetas := resolveOrderedMetas(src.FilmMIDs, metaByMid, orderRank)
			srcCats, srcAllMids, err = buildSourceCategoryMids(srcID, srcMetas)
			srcAllCount = len(srcAllMids)
		}
		if err != nil {
			return model.NotifyBatchSession{}, 0, err
		}
		displayTotal := src.FilmsTotal
		if srcAllCount > displayTotal {
			displayTotal = srcAllCount
		}
		sources = append(sources, model.NotifySourceSession{
			SourceID:     srcID,
			SourceName:   strings.TrimSpace(src.SourceName),
			IsPrimary:    isPrimary,
			Status:       src.Status,
			Error:        src.Error,
			PageTotal:    src.PageTotal,
			SuccessCnt:   src.SuccessCnt,
			FailedCnt:    src.FailedCnt,
			FilmsTotal:   displayTotal,
			AllCount:     srcAllCount,
			CategorySkip: src.CategorySkip,
			OverviewHTML: formatSourceDetailHTML(siteName, src, isPrimary, srcAllCount),
			Categories:   srcCats,
			AllMids:      srcAllMids,
		})
	}

	batchID, err := utils.GenerateSalt()
	if err != nil {
		return model.NotifyBatchSession{}, 0, err
	}
	if len(batchID) > 10 {
		batchID = batchID[:10]
	}
	overview := formatBatchOverviewHTML(siteName, payload, activeSourceName, visibleCount)

	sessionCollectBatchID := ""
	if useLedger {
		sessionCollectBatchID = collectBatchID
	}
	return model.NotifyBatchSession{
		BatchID:        batchID,
		CollectBatchID: sessionCollectBatchID,
		SiteName:       siteName,
		OverviewHTML:   overview,
		PageSize:       pageSize,
		Categories:     categories,
		AllFilms:       allFilms,
		Sources:        sources,
	}, visibleCount, nil
}

func collectBatchUniqueMIDs(payload model.CollectBatchNotifyPayload) []int64 {
	seen := make(map[int64]struct{}, len(payload.NotifyMIDs))
	out := make([]int64, 0, len(payload.NotifyMIDs))
	add := func(mids []int64) {
		for _, mid := range mids {
			if mid <= 0 {
				continue
			}
			if _, ok := seen[mid]; ok {
				continue
			}
			seen[mid] = struct{}{}
			out = append(out, mid)
		}
	}
	add(payload.NotifyMIDs)
	for _, src := range payload.Sources {
		add(src.FilmMIDs)
	}
	return out
}

func resolveOrderedMetas(mids []int64, metaByMid map[int64]NotifyFilmMeta, orderRank map[int64]int) []NotifyFilmMeta {
	if len(mids) == 0 || len(metaByMid) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(mids))
	out := make([]NotifyFilmMeta, 0, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		m, exists := metaByMid[mid]
		if !exists {
			continue
		}
		seen[mid] = struct{}{}
		out = append(out, m)
	}
	sort.SliceStable(out, func(i, j int) bool {
		return orderRank[out[i].Mid] < orderRank[out[j].Mid]
	})
	return out
}

func filterPrimaryVisibleMetas(activeSourceID string, notifyMIDs []int64, metaByMid map[int64]NotifyFilmMeta, orderRank map[int64]int) []NotifyFilmMeta {
	if activeSourceID == "" || len(notifyMIDs) == 0 {
		return nil
	}
	metas := resolveOrderedMetas(notifyMIDs, metaByMid, orderRank)
	if len(metas) == 0 {
		return nil
	}
	visible := snapshot.SourceCategoryVisibility(activeSourceID)
	filtered := make([]NotifyFilmMeta, 0, len(metas))
	for _, m := range metas {
		if strings.TrimSpace(m.FirstSourceId) != activeSourceID {
			continue
		}
		if !visible(m.CategoryKey, m.RootCategoryKey, m.Cid, m.Pid) {
			continue
		}
		filtered = append(filtered, m)
	}
	return filtered
}

func buildPrimaryVisibleGroups(activeSourceID string, notifyMIDs []int64, metaByMid map[int64]NotifyFilmMeta, orderRank map[int64]int) ([]model.NotifyFilmItem, []model.NotifyCategoryGroup, error) {
	filtered := filterPrimaryVisibleMetas(activeSourceID, notifyMIDs, metaByMid, orderRank)
	if len(filtered) == 0 {
		return nil, nil, nil
	}
	srcCats, allMids, err := buildSourceCategoryMids(activeSourceID, filtered)
	if err != nil {
		return nil, nil, err
	}
	allFilms := make([]model.NotifyFilmItem, 0, len(allMids))
	for _, mid := range allMids {
		m := metaByMid[mid]
		allFilms = append(allFilms, model.NotifyFilmItem{Mid: m.Mid, Name: m.Name})
	}
	categories := make([]model.NotifyCategoryGroup, 0, len(srcCats))
	for _, sc := range srcCats {
		items := make([]model.NotifyFilmItem, 0, len(sc.Mids))
		for _, mid := range sc.Mids {
			m := metaByMid[mid]
			items = append(items, model.NotifyFilmItem{Mid: m.Mid, Name: m.Name})
		}
		categories = append(categories, model.NotifyCategoryGroup{
			CategoryID:   sc.CategoryID,
			CategoryName: sc.CategoryName,
			Films:        items,
		})
	}
	return allFilms, categories, nil
}

type sourceCategoryMatcher struct {
	nav           []model.Category
	navSet        map[int64]struct{}
	keyToRoot     map[string]int64
	keysByRoot    map[int64][]string
	allKeys       []string
	allPids       []int64
	useSourceKeys bool
}

func loadSourceCategoryMatcher(sourceID string) (sourceCategoryMatcher, error) {
	nav, err := film.NavTopCategories(sourceID)
	if err != nil {
		return sourceCategoryMatcher{}, err
	}
	m := sourceCategoryMatcher{
		nav:        nav,
		navSet:     make(map[int64]struct{}, len(nav)),
		keyToRoot:  make(map[string]int64, len(nav)*4),
		keysByRoot: make(map[int64][]string, len(nav)),
	}
	for _, c := range nav {
		if c.Id <= 0 {
			continue
		}
		m.navSet[c.Id] = struct{}{}
		m.allPids = append(m.allPids, c.Id)
		if sourceID != "" {
			for _, typeID := range catalog.PublicSourceTypeIDs(sourceID, "pid", c.Id) {
				if k := repobase.BuildSourceCategoryKey(sourceID, typeID); k != "" {
					if _, exists := m.keyToRoot[k]; !exists {
						m.keyToRoot[k] = c.Id
						m.keysByRoot[c.Id] = append(m.keysByRoot[c.Id], k)
						m.allKeys = append(m.allKeys, k)
					}
				}
			}
		}
	}
	m.useSourceKeys = len(m.keyToRoot) > 0
	return m, nil
}

func (m sourceCategoryMatcher) matchRootID(categoryKey, rootCategoryKey string, pid int64) (int64, bool) {
	if m.useSourceKeys {
		if rootID, ok := m.keyToRoot[categoryKey]; ok && rootID > 0 {
			return rootID, true
		}
		if rootID, ok := m.keyToRoot[rootCategoryKey]; ok && rootID > 0 {
			return rootID, true
		}
		return 0, false
	}
	if pid > 0 {
		if _, ok := m.navSet[pid]; ok {
			return pid, true
		}
	}
	return 0, false
}

func (m sourceCategoryMatcher) buildPageQuery(batchID, sourceID string, catIdx int, cat model.NotifySourceCategoryGroup, limit, offset int) BatchSourceCategoryPageQuery {
	q := BatchSourceCategoryPageQuery{
		BatchID:         batchID,
		SourceID:        sourceID,
		UseCategoryKeys: m.useSourceKeys,
		KnownKeys:       m.allKeys,
		KnownPids:       m.allPids,
		Limit:           limit,
		Offset:          offset,
	}
	if catIdx < 0 {
		q.CategoryAll = true
		return q
	}
	if cat.CategoryID == 0 {
		q.IsOther = true
		return q
	}
	q.IncludeKeys = m.keysByRoot[cat.CategoryID]
	q.IncludePids = []int64{cat.CategoryID}
	return q
}

func countPrimaryVisibleFromStats(activeSourceID string, rows []BatchSourceCategoryStatRow) int {
	if activeSourceID == "" || len(rows) == 0 {
		return 0
	}
	visible := snapshot.SourceCategoryVisibility(activeSourceID)
	total := 0
	for _, r := range rows {
		if r.Count <= 0 || strings.TrimSpace(r.FirstSourceId) != activeSourceID {
			continue
		}
		if visible(r.CategoryKey, r.RootCategoryKey, r.Cid, r.Pid) {
			total += r.Count
		}
	}
	return total
}

func buildSourceCategoryCountsFromStats(sourceID string, rows []BatchSourceCategoryStatRow) ([]model.NotifySourceCategoryGroup, int, error) {
	if len(rows) == 0 {
		return nil, 0, nil
	}
	matcher, err := loadSourceCategoryMatcher(sourceID)
	if err != nil {
		return nil, 0, err
	}
	allCount := 0
	buckets := make(map[int64]int, len(matcher.nav))
	otherCount := 0
	for _, r := range rows {
		if r.Count <= 0 {
			continue
		}
		allCount += r.Count
		if rootID, ok := matcher.matchRootID(r.CategoryKey, r.RootCategoryKey, r.Pid); ok {
			buckets[rootID] += r.Count
		} else {
			otherCount += r.Count
		}
	}
	if allCount == 0 {
		return nil, 0, nil
	}
	groups := make([]model.NotifySourceCategoryGroup, 0, len(matcher.nav)+1)
	for _, c := range matcher.nav {
		if cnt := buckets[c.Id]; cnt > 0 {
			groups = append(groups, model.NotifySourceCategoryGroup{
				CategoryID:   c.Id,
				CategoryName: c.Name,
				Count:        cnt,
			})
		}
	}
	if otherCount > 0 {
		groups = append(groups, model.NotifySourceCategoryGroup{
			CategoryID:   0,
			CategoryName: "其他",
			Count:        otherCount,
		})
	}
	return groups, allCount, nil
}

func buildSourceCategoryMids(sourceID string, metas []NotifyFilmMeta) ([]model.NotifySourceCategoryGroup, []int64, error) {
	if len(metas) == 0 {
		return nil, nil, nil
	}
	matcher, err := loadSourceCategoryMatcher(sourceID)
	if err != nil {
		return nil, nil, err
	}

	allMids := make([]int64, 0, len(metas))
	buckets := make(map[int64][]int64, len(matcher.nav))
	var others []int64
	for _, m := range metas {
		allMids = append(allMids, m.Mid)
		if rootID, ok := matcher.matchRootID(m.CategoryKey, m.RootCategoryKey, m.Pid); ok {
			buckets[rootID] = append(buckets[rootID], m.Mid)
		} else {
			others = append(others, m.Mid)
		}
	}

	groups := make([]model.NotifySourceCategoryGroup, 0, len(matcher.nav)+1)
	for _, c := range matcher.nav {
		if list := buckets[c.Id]; len(list) > 0 {
			groups = append(groups, model.NotifySourceCategoryGroup{
				CategoryID:   c.Id,
				CategoryName: c.Name,
				Count:        len(list),
				Mids:         list,
			})
		}
	}
	if len(others) > 0 {
		groups = append(groups, model.NotifySourceCategoryGroup{
			CategoryID:   0,
			CategoryName: "其他",
			Count:        len(others),
			Mids:         others,
		})
	}
	return groups, allMids, nil
}
