package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"slices"
	"strings"
	"sync"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/support"
	"server/internal/spider/progress"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupNotifyConfigDB(t *testing.T) {
	t.Helper()
	dsn := testutil.MemDSN(t)
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.SiteConfigRecord{}, &model.NotifyConfigRecord{}, &model.ProxyConfigRecord{}, &model.FilmIndex{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })
}

func TestPublishFailed_DisabledDoesNotBurnCooldown(t *testing.T) {
	setupNotifyConfigDB(t)
	s := &NotifyService{alertLast: map[string]time.Time{}}
	s.PublishSourceFailed("src1", "站", "超时")
	s.PublishCronFailed("task1", "清理", "失败")
	s.alertMu.Lock()
	defer s.alertMu.Unlock()
	if len(s.alertLast) != 0 {
		t.Fatalf("disabled notify recorded cooldown: %+v", s.alertLast)
	}
}

func TestMaskAndMergeNotifyConfig(t *testing.T) {
	rawToken := "123456789:AAHdqTcvCH1vGWJxfSeofSAs0K5PALDsaw"
	masked := repository.MaskBotToken(rawToken)
	if masked == rawToken || !strings.Contains(masked, "***") {
		t.Fatalf("expected masked token, got %q", masked)
	}
	if !repository.IsMaskedBotToken(masked) {
		t.Fatalf("expected IsMaskedBotToken(%q) to be true", masked)
	}

	stored := repository.DefaultNotifyConfig()
	stored.BotToken = rawToken
	stored.ChatIDs = []string{"-100123456"}

	incoming := model.NotifyConfig{
		Enabled:            true,
		BotToken:           masked,
		ChatIDs:            []string{" -100123456 ", "-100999:42", "-100123456"},
		Events:             stored.Events,
		OnlyNotifyOnUpdate: true,
		MaxFilmsInMessage:  25,
	}
	merged, err := validateAndMergeNotifyConfig(stored, incoming)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if merged.BotToken != rawToken {
		t.Fatalf("expected stored token preserved, got %q", merged.BotToken)
	}
	if len(merged.ChatIDs) != 2 || merged.ChatIDs[1] != "-100999:42" {
		t.Fatalf("unexpected chatIds: %+v", merged.ChatIDs)
	}
	if merged.MaxFilmsInMessage != model.MaxNotifyPageSize {
		t.Fatalf("expected capped pageSize %d, got %d", model.MaxNotifyPageSize, merged.MaxFilmsInMessage)
	}

	// 显式清空 BotToken（禁用状态下）应保留为空，不再回填旧值
	cleared := model.NotifyConfig{
		Enabled:           false,
		BotToken:          "",
		ChatIDs:           []string{"-100123456"},
		Events:            stored.Events,
		MaxFilmsInMessage: 15,
	}
	mergedCleared, err := validateAndMergeNotifyConfig(stored, cleared)
	if err != nil {
		t.Fatalf("unexpected error clearing token: %v", err)
	}
	if mergedCleared.BotToken != "" {
		t.Fatalf("expected BotToken to be cleared, got %q", mergedCleared.BotToken)
	}

	// 测试非法 threadId
	bad := incoming
	bad.ChatIDs = []string{"-100123:abc"}
	if _, err := validateAndMergeNotifyConfig(stored, bad); err == nil {
		t.Fatalf("expected error on invalid threadId")
	}
}

func TestNotifyCallbackAndPagination(t *testing.T) {
	setupNotifyConfigDB(t)
	sess := model.NotifyBatchSession{
		BatchID:      "b123",
		SiteName:     "EcoHub",
		OverviewHTML: "<b>[EcoHub] 采集汇总</b>",
		PageSize:     2,
		Categories: []model.NotifyCategoryGroup{
			{
				CategoryID:   1,
				CategoryName: "电影",
				Films: []model.NotifyFilmItem{
					{Mid: 101, Name: "影片A"},
					{Mid: 102, Name: "影片B"},
					{Mid: 103, Name: "影片C"},
				},
			},
		},
		AllFilms: []model.NotifyFilmItem{
			{Mid: 101, Name: "影片A"},
			{Mid: 102, Name: "影片B"},
			{Mid: 103, Name: "影片C"},
		},
	}
	if err := repository.SaveNotifyBatchSession(sess); err != nil {
		t.Fatalf("SaveNotifyBatchSession failed: %v", err)
	}

	// 无论是否配置 SiteURL，分类与“全部”按钮均应生成内联翻页 CallbackData
	kb := buildCategoryKeyboard(sess.BatchID, sess.Categories, len(sess.AllFilms))
	if kb == nil || len(kb.InlineKeyboard) == 0 {
		t.Fatalf("expected non-empty category keyboard")
	}
	if got := kb.InlineKeyboard[0][0].CallbackData; got != "nfp:b123:0:1" {
		t.Fatalf("unexpected category button callback: %q", got)
	}
	if got := kb.InlineKeyboard[0][1].CallbackData; got != "nfp:b123:-1:1" {
		t.Fatalf("unexpected all button callback: %q", got)
	}

	batchID, action, catIdx, page, ok := parseNotifyCallback("nfp:b123:0:1")
	if !ok || batchID != "b123" || action != "page" || catIdx != 0 || page != 1 {
		t.Fatalf("unexpected callback parse result: batch=%s action=%s catIdx=%d page=%d ok=%v", batchID, action, catIdx, page, ok)
	}

	// 未配置 SiteURL 时，片单正常展示但不附带超链接
	pageHTML, totalPages, err := formatFilmListPageHTML(sess, 0, 1)
	if err != nil {
		t.Fatal(err)
	}
	if totalPages != 2 || !strings.Contains(pageHTML, "影片A") || !strings.Contains(pageHTML, "影片B") || strings.Contains(pageHTML, "影片C") {
		t.Fatalf("unexpected page 1 html (totalPages=%d): %s", totalPages, pageHTML)
	}
	if strings.Contains(pageHTML, "<a href=") {
		t.Fatalf("did not expect film link when siteURL is empty: %s", pageHTML)
	}

	// 配置 SiteURL 后，每部影片可直接跳转播放页
	if err := repository.SaveSiteBasic(model.BasicConfig{SiteName: "EcoHub", SiteURL: "http://localhost:3300/"}); err != nil {
		t.Fatal(err)
	}
	linkedHTML, _, err := formatFilmListPageHTML(sess, -1, 1)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(linkedHTML, `<a href="http://localhost:3300/play?id=101">影片A</a>`) {
		t.Fatalf("expected film play link in page html: %s", linkedHTML)
	}

	page2HTML, _, err := formatFilmListPageHTML(sess, 0, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(page2HTML, "影片C") {
		t.Fatalf("expected page 2 to contain 影片C: %s", page2HTML)
	}
}

func TestSendTestAndCallbackEditWithMockServer(t *testing.T) {
	setupNotifyConfigDB(t)
	var (
		mu      sync.Mutex
		methods []string
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		methods = append(methods, r.URL.Path)
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{
			"ok":     true,
			"result": map[string]any{"message_id": 1},
		})
	}))
	defer ts.Close()

	oldBase := tgAPIBaseURL
	tgAPIBaseURL = ts.URL
	defer func() { tgAPIBaseURL = oldBase }()

	res, err := NotifySvc.SendTest(model.NotifyTestReq{
		BotToken: "123456:ABCDEF",
		ChatIDs:  []string{"-100111", "-100222:5"},
	})
	if err != nil {
		t.Fatalf("SendTest failed: %v", err)
	}
	if res.Sent != 2 || len(res.Failed) != 0 {
		t.Fatalf("unexpected SendTest result: %+v", res)
	}

	sess := model.NotifyBatchSession{
		BatchID:      "t999",
		SiteName:     "TestSite",
		OverviewHTML: "<b>Overview</b>",
		PageSize:     15,
		AllFilms:     []model.NotifyFilmItem{{Mid: 1, Name: "Film1"}},
	}
	_ = repository.SaveNotifyBatchSession(sess)

	handleNotifyCallback(context.Background(), "123456:ABCDEF", &tgCallbackQuery{
		ID:   "cb1",
		Data: "nfp:t999:-1:1",
		Message: &tgMessage{
			MessageID: 88,
			Chat:      &tgChat{ID: -100111},
		},
	})

	mu.Lock()
	defer mu.Unlock()
	if len(methods) < 4 {
		t.Fatalf("expected at least 4 telegram API calls, got %v", methods)
	}
}

func TestFormatBatchOverviewHTML(t *testing.T) {
	now := time.Now()
	payload := model.CollectBatchNotifyPayload{
		Trigger:    model.CollectTriggerCron,
		StartedAt:  now.Add(-65 * time.Second),
		FinishedAt: now,
		Sources: []model.SourceNotifyResult{
			{
				SourceID:   "s1",
				SourceName: "资源站A",
				Status:     progress.StatusDone,
				PageTotal:  5,
				SuccessCnt: 5,
				FilmsTotal: 9,
			},
			{
				SourceID:   "s2",
				SourceName: "资源站B",
				Status:     progress.StatusFailed,
				Error:      "connection timeout",
			},
		},
	}
	out := formatBatchOverviewHTML("EcoHub", payload, 12)
	if !strings.Contains(out, "定时任务") || !strings.Contains(out, "1分5秒") || !strings.Contains(out, "前台可见：<b>12</b> 部") || !strings.Contains(out, "资源站A</b> (入库 9 部)") || !strings.Contains(out, "connection timeout") {
		t.Fatalf("unexpected overview HTML: %s", out)
	}
}

func TestFormatPrimaryChangeHTML(t *testing.T) {
	oldPrimary := &model.FilmSource{Id: "s1", Name: "量子资源"}
	newPrimary := &model.FilmSource{Id: "s2", Name: "非凡资源"}
	enabled := []model.FilmSource{
		{Id: "s2", Name: "非凡资源", State: true},
		{Id: "s1", Name: "量子资源", State: true},
		{Id: "s3", Name: "暴风资源", State: true},
	}

	switchedHTML := formatPrimaryChangeHTML("EcoHub", oldPrimary, newPrimary, enabled)
	if !strings.Contains(switchedHTML, "主站已切换") ||
		!strings.Contains(switchedHTML, "<b>量子资源</b> → <b>非凡资源</b>") ||
		!strings.Contains(switchedHTML, "<b>非凡资源</b> → 量子资源 → 暴风资源") {
		t.Fatalf("unexpected switched HTML: %s", switchedHTML)
	}

	clearedHTML := formatPrimaryChangeHTML("EcoHub", oldPrimary, nil, enabled)
	if !strings.Contains(clearedHTML, "主站已置空") ||
		!strings.Contains(clearedHTML, "<b>量子资源</b> → 无") {
		t.Fatalf("unexpected cleared HTML: %s", clearedHTML)
	}
}

func TestSafeTruncateHTMLAndOverviewLimit(t *testing.T) {
	// 多行超长 HTML 截断不应留下半截标签
	var lines []string
	for i := 0; i < 30; i++ {
		lines = append(lines, "· <b>测试影片</b> (<code>#100</code>)")
	}
	truncated := safeTruncateHTML(strings.Join(lines, "\n"), 200)
	if strings.Count(truncated, "<b>") != strings.Count(truncated, "</b>") {
		t.Fatalf("unbalanced <b> tags after truncate: %s", truncated)
	}
	if strings.Count(truncated, "<code>") != strings.Count(truncated, "</code>") {
		t.Fatalf("unbalanced <code> tags after truncate: %s", truncated)
	}

	// 单行超长兜底剥离标签并保持合法长度
	longSingle := "<b>" + strings.Repeat("A", 500) + "</b>"
	singleTruncated := safeTruncateHTML(longSingle, 100)
	if strings.Contains(singleTruncated, "<b>") {
		t.Fatalf("expected tags stripped on single-line overflow: %s", singleTruncated)
	}

	// 超多站点汇总应自动折叠且不超过 Telegram 消息长度限制
	var sources []model.SourceNotifyResult
	for i := 0; i < 120; i++ {
		sources = append(sources, model.SourceNotifyResult{
			SourceID:   "s",
			SourceName: "超长采集站点名称测试_" + strings.Repeat("X", 20),
			Status:     progress.StatusFailed,
			Error:      strings.Repeat("err_", 15),
		})
	}
	overview := formatBatchOverviewHTML("EcoHub", model.CollectBatchNotifyPayload{
		Trigger:    model.CollectTriggerManual,
		StartedAt:  time.Now().Add(-10 * time.Second),
		FinishedAt: time.Now(),
		Sources:    sources,
	}, 5)
	if len([]rune(overview)) > telegramMaxMessageLen {
		t.Fatalf("overview exceeded telegram max len: %d", len([]rune(overview)))
	}
	if !strings.Contains(overview, "已省略") {
		t.Fatalf("expected omitted hint in oversized overview: %s", overview)
	}
}

func TestFormatOverviewCategorySkipAndMultilineSafe(t *testing.T) {
	now := time.Now()
	payload := model.CollectBatchNotifyPayload{
		Trigger:    model.CollectTriggerCron,
		StartedAt:  now.Add(-5 * time.Second),
		FinishedAt: now,
		Sources: []model.SourceNotifyResult{
			{
				SourceID:     "s1",
				SourceName:   "站\n名",
				Status:       progress.StatusDone,
				FilmsTotal:   8,
				CategorySkip: 2,
			},
			{
				SourceID:   "s2",
				SourceName: "报错站",
				Status:     progress.StatusFailed,
				Error:      "line1\nline2",
			},
		},
	}
	out := formatBatchOverviewHTML("EcoHub", payload, 8)
	if !strings.Contains(out, "✅ <b>站 名</b> (入库 8 部，跳过未映射 2 部)") {
		t.Fatalf("category skip should stay success icon without error: %s", out)
	}
	if !strings.Contains(out, "成功 <b>1</b> 个，失败 <b>1</b> 个") {
		t.Fatalf("unexpected source counts: %s", out)
	}
	if strings.Contains(out, "line1\nline2") || !strings.Contains(out, "<code>line1 line2</code>") {
		t.Fatalf("multiline error should be single-lined inside <code>: %s", out)
	}

	reasonHTML := formatReasonCodeLines("first error\nsecond error", 200, 600)
	if reasonHTML != "<code>first error</code>\n<code>second error</code>" {
		t.Fatalf("unexpected reason code lines: %s", reasonHTML)
	}
	cut := safeTruncateHTML(reasonHTML, 32)
	if strings.Count(cut, "<code>") != strings.Count(cut, "</code>") {
		t.Fatalf("truncated multiline code tags unbalanced: %s", cut)
	}

	orderHTML := formatSourceOrderChangeHTML("EcoHub", []model.FilmSource{
		{Id: "s1", Name: "主站", State: true},
		{Id: "s2", Name: "备站", State: true},
	})
	if !strings.Contains(orderHTML, "采集站排序已调整") || !strings.Contains(orderHTML, "<b>主站</b> → 备站") {
		t.Fatalf("unexpected order change HTML: %s", orderHTML)
	}
}

func TestBuildNotifyBatchSession_FiltersHiddenCategory(t *testing.T) {
	setupNotifyConfigDB(t)
	gdb := db.Mdb
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.Category{}, &model.SourceCategory{}, &model.CategoryMapping{}, &model.PrimaryState{}, &model.MappingRule{}); err != nil {
		t.Fatal(err)
	}
	prevRdb := db.Rdb
	db.Rdb = nil
	prevSnap := support.CurrentMappingSnapshotForTest()
	support.InvalidatePrimarySourceCache()
	repository.InvalidatePublicSourceCache()
	repository.MarkCategoryChanged()
	t.Cleanup(func() {
		db.Rdb = prevRdb
		support.StoreMappingSnapshotForTest(prevSnap)
		support.InvalidatePrimarySourceCache()
		repository.InvalidatePublicSourceCache()
		repository.MarkCategoryChanged()
	})

	if err := gdb.Create(&model.FilmSource{Id: "src_notify_hide", Name: "主站", Uri: "http://1", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.PrimaryState{Id: 1, PrimarySourceId: "src_notify_hide"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.Category{Id: 10, Pid: 0, Name: "电影", StableKey: "cat_10", Show: true, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.Category{Id: 20, Pid: 0, Name: "隐藏区", StableKey: "cat_20", Show: true, Sort: 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&model.Category{}).Where("id = ?", 20).Update("show", false).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.SourceCategory{SourceId: "src_notify_hide", SourceTypeId: 1, RawName: "电影"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.SourceCategory{SourceId: "src_notify_hide", SourceTypeId: 2, RawName: "隐藏区"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: "src_notify_hide", SourceTypeId: 1, CategoryId: 10}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: "src_notify_hide", SourceTypeId: 2, CategoryId: 20}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101, FirstSourceId: "src_notify_hide"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 10, Cid: 10, CategoryKey: "source:src_notify_hide:1", RootCategoryKey: "source:src_notify_hide:1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "可见电影", UpdateStamp: 200},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 102, FirstSourceId: "src_notify_hide"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 20, Cid: 20, CategoryKey: "source:src_notify_hide:2", RootCategoryKey: "source:src_notify_hide:2"},
		FilmIndexContent:  model.FilmIndexContent{Name: "隐藏电影", UpdateStamp: 200},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src_peer_other", Name: "非主站", Uri: "http://2", State: true, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.SourceCategory{SourceId: "src_peer_other", SourceTypeId: 99, RawName: "电影"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: "src_peer_other", SourceTypeId: 99, CategoryId: 10}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 103, FirstSourceId: "src_peer_other"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 10, Cid: 10, CategoryKey: "source:src_peer_other:99", RootCategoryKey: "source:src_peer_other:99"},
		FilmIndexContent:  model.FilmIndexContent{Name: "非主站电影", UpdateStamp: 300},
	}).Error; err != nil {
		t.Fatal(err)
	}
	// 主站内存在未知分类键的影片，即使其本地 Pid 数值恰好等于主站某 source_type_id，也不应误归桶
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 104, FirstSourceId: "src_notify_hide"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 1, CategoryKey: "source:src_notify_hide:999", RootCategoryKey: "source:src_notify_hide:999"},
		FilmIndexContent:  model.FilmIndexContent{Name: "未映射主站影片", UpdateStamp: 150},
	}).Error; err != nil {
		t.Fatal(err)
	}

	_ = repository.RefreshCollectSourceCatalog()
	repository.ReloadMappingRules()
	repository.RefreshCategoryCache()
	support.InvalidatePrimarySourceCache()
	repository.InvalidatePublicSourceCache()
	repository.MarkCategoryChanged()

	sess, err := buildNotifyBatchSession(model.CollectBatchNotifyPayload{
		NotifyMIDs: []int64{101, 102, 103, 104},
	}, 15)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.AllFilms) != 2 || sess.AllFilms[0].Mid != 101 || sess.AllFilms[1].Mid != 104 {
		t.Fatalf("expected only primary visible films [101 104], got %+v", sess.AllFilms)
	}
	if !strings.Contains(sess.OverviewHTML, "前台可见：<b>2</b> 部") {
		t.Fatalf("expected overview count 2, got: %s", sess.OverviewHTML)
	}
	if len(sess.Categories) != 2 || sess.Categories[0].CategoryName != "电影" || len(sess.Categories[0].Films) != 1 || sess.Categories[1].CategoryName != "其他" || len(sess.Categories[1].Films) != 1 {
		t.Fatalf("unexpected categories: %+v", sess.Categories)
	}

	// 置空主站时，前台可见应严格为 0
	if err := gdb.Where("id = ?", 1).Delete(&model.PrimaryState{}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&model.FilmSource{}).Where("1 = 1").Update("state", false).Error; err != nil {
		t.Fatal(err)
	}
	support.InvalidatePrimarySourceCache()
	emptySess, err := buildNotifyBatchSession(model.CollectBatchNotifyPayload{
		NotifyMIDs: []int64{101, 102, 103},
	}, 15)
	if err != nil {
		t.Fatal(err)
	}
	if len(emptySess.AllFilms) != 0 || len(emptySess.Categories) != 0 {
		t.Fatalf("expected empty session without primary source, got films=%+v cats=%+v", emptySess.AllFilms, emptySess.Categories)
	}
	if !strings.Contains(emptySess.OverviewHTML, "前台可见：<b>0</b> 部") {
		t.Fatalf("expected overview count 0 without primary source, got: %s", emptySess.OverviewHTML)
	}
}

func TestEnabledSourceOrderIgnoresDisabledReorder(t *testing.T) {
	before := []model.FilmSource{
		{Id: "s1", State: true},
		{Id: "d1", State: false},
		{Id: "s2", State: true},
		{Id: "d2", State: false},
	}
	afterDisabledMoved := []model.FilmSource{
		{Id: "s1", State: true},
		{Id: "d2", State: false},
		{Id: "d1", State: false},
		{Id: "s2", State: true},
	}
	if !slices.Equal(sourceIDOrder(enabledSources(before)), sourceIDOrder(enabledSources(afterDisabledMoved))) {
		t.Fatal("disabled source reorder must keep enabled source order equal")
	}
	afterEnabledMoved := []model.FilmSource{
		{Id: "s2", State: true},
		{Id: "s1", State: true},
		{Id: "d1", State: false},
	}
	if slices.Equal(sourceIDOrder(enabledSources(before)), sourceIDOrder(enabledSources(afterEnabledMoved))) {
		t.Fatal("enabled source reorder must be detected")
	}
}
