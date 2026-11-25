package notify

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"server/internal/film/catalog"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
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
	masked := MaskBotToken(rawToken)
	if masked == rawToken || !strings.Contains(masked, "***") {
		t.Fatalf("expected masked token, got %q", masked)
	}
	if !IsMaskedBotToken(masked) {
		t.Fatalf("expected IsMaskedBotToken(%q) to be true", masked)
	}

	stored := DefaultNotifyConfig()
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
	if err := SaveNotifyBatchSession(sess); err != nil {
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
	if err := repobase.SaveSiteBasic(model.BasicConfig{SiteName: "EcoHub", SiteURL: "http://localhost:3300/"}); err != nil {
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
	_ = SaveNotifyBatchSession(sess)

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
				Status:     model.CollectStatusDone,
				PageTotal:  5,
				SuccessCnt: 5,
				FilmsTotal: 9,
			},
			{
				SourceID:   "s2",
				SourceName: "资源站B",
				Status:     model.CollectStatusFailed,
				Error:      "connection timeout",
			},
		},
	}
	out := formatBatchOverviewHTML("EcoHub", payload, "资源站A", 12)
	if !strings.Contains(out, "定时任务") || !strings.Contains(out, "1分5秒") || !strings.Contains(out, "成功 <b>1</b> 个，失败 <b>1</b> 个") || !strings.Contains(out, "共入库 <b>9</b> 部") || !strings.Contains(out, "⭐ 主站：<b>资源站A</b> · 前台可见 <b>12</b> 部") {
		t.Fatalf("unexpected compact overview HTML: %s", out)
	}
	if strings.Contains(out, "connection timeout") {
		t.Fatalf("compact overview should not expand full source error lines: %s", out)
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

	// 超多站点时首屏文字精简且站点按钮自动分页，不超 Telegram 单条 100 按钮与 4096 字符限制
	var sources []model.SourceNotifyResult
	var sourceSessions []model.NotifySourceSession
	for i := 0; i < 120; i++ {
		name := fmt.Sprintf("采集站点_%03d_%s", i, strings.Repeat("X", 15))
		sources = append(sources, model.SourceNotifyResult{
			SourceID:   fmt.Sprintf("s_%d", i),
			SourceName: name,
			Status:     model.CollectStatusDone,
			FilmsTotal: i,
		})
		sourceSessions = append(sourceSessions, model.NotifySourceSession{
			SourceID:   fmt.Sprintf("s_%d", i),
			SourceName: name,
			Status:     model.CollectStatusDone,
			FilmsTotal: i,
		})
	}
	overview := formatBatchOverviewHTML("EcoHub", model.CollectBatchNotifyPayload{
		Trigger:    model.CollectTriggerManual,
		StartedAt:  time.Now().Add(-10 * time.Second),
		FinishedAt: time.Now(),
		Sources:    sources,
	}, "", 5)
	if len([]rune(overview)) > telegramMaxMessageLen {
		t.Fatalf("overview exceeded telegram max len: %d", len([]rune(overview)))
	}
	kb := buildOverviewKeyboard(model.NotifyBatchSession{BatchID: "b120", Sources: sourceSessions}, 1)
	if kb == nil {
		t.Fatal("expected overview keyboard")
	}
	btnCount := 0
	for _, r := range kb.InlineKeyboard {
		btnCount += len(r)
	}
	if btnCount > 100 {
		t.Fatalf("overview keyboard exceeded telegram 100 button limit: %d", btnCount)
	}
	lastRow := kb.InlineKeyboard[len(kb.InlineKeyboard)-1]
	if len(lastRow) != 3 || lastRow[1].CallbackData != "nfp:b120:info:-2" || lastRow[2].CallbackData != "nfp:b120:sp:2" {
		t.Fatalf("expected site pagination row, got %+v", lastRow)
	}
}

func TestFormatOverviewCategorySkipAndMultilineSafe(t *testing.T) {
	now := time.Now()
	src1 := model.SourceNotifyResult{
		SourceID:     "s1",
		SourceName:   "站\n名",
		Status:       model.CollectStatusDone,
		FilmsTotal:   8,
		CategorySkip: 2,
	}
	src2 := model.SourceNotifyResult{
		SourceID:   "s2",
		SourceName: "报错站",
		Status:     model.CollectStatusFailed,
		Error:      "line1\nline2",
	}
	payload := model.CollectBatchNotifyPayload{
		Trigger:    model.CollectTriggerCron,
		StartedAt:  now.Add(-5 * time.Second),
		FinishedAt: now,
		Sources:    []model.SourceNotifyResult{src1, src2},
	}
	out := formatBatchOverviewHTML("EcoHub", payload, "站 名", 8)
	if !strings.Contains(out, "成功 <b>1</b> 个，失败 <b>1</b> 个") || !strings.Contains(out, "共入库 <b>8</b> 部") {
		t.Fatalf("unexpected source counts: %s", out)
	}
	d1 := formatSourceDetailHTML("EcoHub", src1, true, 8)
	if !strings.Contains(d1, "⭐ 站 名") || !strings.Contains(d1, "跳过未映射 2 部") || !strings.Contains(d1, "✅ 状态：完成") {
		t.Fatalf("unexpected primary source detail HTML: %s", d1)
	}
	d2 := formatSourceDetailHTML("EcoHub", src2, false, 0)
	if strings.Contains(d2, "line1\nline2") || !strings.Contains(d2, "<code>line1 line2</code>") {
		t.Fatalf("multiline error should be single-lined inside <code>: %s", d2)
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
	prevSnap := taxonomy.CurrentMappingSnapshotForTest()
	sourcecatalog.InvalidatePrimarySourceCache()
	catalog.InvalidatePublicSourceCache()
	catalog.MarkCategoryChanged()
	t.Cleanup(func() {
		db.Rdb = prevRdb
		taxonomy.StoreMappingSnapshotForTest(prevSnap)
		sourcecatalog.InvalidatePrimarySourceCache()
		catalog.InvalidatePublicSourceCache()
		catalog.MarkCategoryChanged()
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

	_ = sourcecatalog.RefreshCollectSourceCatalog()
	taxonomy.ReloadMappingRules()
	taxonomy.RefreshCategoryCache()
	sourcecatalog.InvalidatePrimarySourceCache()
	catalog.InvalidatePublicSourceCache()
	catalog.MarkCategoryChanged()

	sess, err := buildNotifyBatchSession(model.CollectBatchNotifyPayload{
		NotifyMIDs: []int64{101, 102, 103, 104},
	}, 15)
	if err != nil {
		t.Fatal(err)
	}
	if len(sess.AllFilms) != 2 || sess.AllFilms[0].Mid != 101 || sess.AllFilms[1].Mid != 104 {
		t.Fatalf("expected only primary visible films [101 104], got %+v", sess.AllFilms)
	}
	if !strings.Contains(sess.OverviewHTML, "前台可见 <b>2</b> 部") {
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
	sourcecatalog.InvalidatePrimarySourceCache()
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

func TestNotifyPerSiteReplyAndCategoryDrilldown(t *testing.T) {
	setupNotifyConfigDB(t)
	gdb := db.Mdb
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.Category{}, &model.SourceCategory{}, &model.CategoryMapping{}, &model.PrimaryState{}, &model.MappingRule{}); err != nil {
		t.Fatal(err)
	}
	prevRdb := db.Rdb
	db.Rdb = nil
	prevSnap := taxonomy.CurrentMappingSnapshotForTest()
	sourcecatalog.InvalidatePrimarySourceCache()
	catalog.InvalidatePublicSourceCache()
	catalog.MarkCategoryChanged()
	t.Cleanup(func() {
		db.Rdb = prevRdb
		taxonomy.StoreMappingSnapshotForTest(prevSnap)
		sourcecatalog.InvalidatePrimarySourceCache()
		catalog.InvalidatePublicSourceCache()
		catalog.MarkCategoryChanged()
	})

	if err := gdb.Create(&model.FilmSource{Id: "src_main", Name: "速博(SUBO)", Uri: "http://1", State: true, Sort: 0}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src_sub", Name: "HD(BF)", Uri: "http://2", State: true, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.PrimaryState{Id: 1, PrimarySourceId: "src_main"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.Category{Id: 10, Pid: 0, Name: "电影", StableKey: "cat_10", Show: true, Sort: 1}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.Category{Id: 11, Pid: 0, Name: "动漫", StableKey: "cat_11", Show: true, Sort: 2}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.SourceCategory{SourceId: "src_main", SourceTypeId: 1, RawName: "动漫"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.SourceCategory{SourceId: "src_sub", SourceTypeId: 2, RawName: "电影"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.SourceCategory{SourceId: "src_sub", SourceTypeId: 3, RawName: "动漫"}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: "src_main", SourceTypeId: 1, CategoryId: 11}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: "src_sub", SourceTypeId: 2, CategoryId: 10}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.CategoryMapping{SourceId: "src_sub", SourceTypeId: 3, CategoryId: 11}).Error; err != nil {
		t.Fatal(err)
	}

	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 201, FirstSourceId: "src_main"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 11, Cid: 11, CategoryKey: "source:src_main:1", RootCategoryKey: "source:src_main:1"},
		FilmIndexContent:  model.FilmIndexContent{Name: "主站动漫", UpdateStamp: 500},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 202, FirstSourceId: "src_sub"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 10, Cid: 10, CategoryKey: "source:src_sub:2", RootCategoryKey: "source:src_sub:2"},
		FilmIndexContent:  model.FilmIndexContent{Name: "备站电影1", UpdateStamp: 400},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 203, FirstSourceId: "src_sub"},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 11, Cid: 11, CategoryKey: "source:src_sub:3", RootCategoryKey: "source:src_sub:3"},
		FilmIndexContent:  model.FilmIndexContent{Name: "备站动漫1", UpdateStamp: 300},
	}).Error; err != nil {
		t.Fatal(err)
	}

	_ = sourcecatalog.RefreshCollectSourceCatalog()
	taxonomy.ReloadMappingRules()
	taxonomy.RefreshCategoryCache()
	sourcecatalog.InvalidatePrimarySourceCache()
	catalog.InvalidatePublicSourceCache()
	catalog.MarkCategoryChanged()

	sess, err := buildNotifyBatchSession(model.CollectBatchNotifyPayload{
		Trigger:    model.CollectTriggerManual,
		StartedAt:  time.Now().Add(-10 * time.Second),
		FinishedAt: time.Now(),
		NotifyMIDs: []int64{201, 202, 203},
		Sources: []model.SourceNotifyResult{
			{SourceID: "src_main", SourceName: "速博(SUBO)", Status: model.CollectStatusDone, FilmsTotal: 1, FilmMIDs: []int64{201}},
			{SourceID: "src_sub", SourceName: "HD(BF)", Status: model.CollectStatusDone, FilmsTotal: 2, FilmMIDs: []int64{202, 203}},
			{SourceID: "src_zero", SourceName: "U酷(UKU)", Status: model.CollectStatusDone, FilmsTotal: 0},
		},
	}, 15)
	if err != nil {
		t.Fatal(err)
	}
	if err := SaveNotifyBatchSession(sess); err != nil {
		t.Fatal(err)
	}
	if len(sess.AllFilms) != 0 || len(sess.Categories) != 0 {
		t.Fatalf("expected no redundant AllFilms/Categories when Sources is present, got films=%d cats=%d", len(sess.AllFilms), len(sess.Categories))
	}
	if !strings.Contains(sess.OverviewHTML, "前台可见 <b>1</b> 部") {
		t.Fatalf("expected primary visible count 1 in overview, got: %s", sess.OverviewHTML)
	}

	// 1. 汇总键盘应包含全部 3 个站点按钮，且主站带 ⭐ 标识
	overviewKB := buildOverviewKeyboard(sess, 1)
	if overviewKB == nil || len(overviewKB.InlineKeyboard) != 2 {
		t.Fatalf("expected 2 rows for 3 sites, got %+v", overviewKB)
	}
	if !strings.HasPrefix(overviewKB.InlineKeyboard[0][0].Text, "⭐ 速博(SUBO)") {
		t.Fatalf("expected primary star prefix on first button, got %q", overviewKB.InlineKeyboard[0][0].Text)
	}
	if overviewKB.InlineKeyboard[0][1].CallbackData != fmt.Sprintf("nfp:%s:s:1", sess.BatchID) {
		t.Fatalf("unexpected second site callback: %q", overviewKB.InlineKeyboard[0][1].CallbackData)
	}
	if overviewKB.InlineKeyboard[1][0].Text != "✅ U酷(UKU) (0)" {
		t.Fatalf("unexpected zero site button text: %q", overviewKB.InlineKeyboard[1][0].Text)
	}

	type apiCall struct {
		Path string
		Body map[string]any
	}
	var (
		mu    sync.Mutex
		calls []apiCall
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		calls = append(calls, apiCall{Path: r.URL.Path, Body: body})
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	}))
	defer ts.Close()
	oldBase := tgAPIBaseURL
	tgAPIBaseURL = ts.URL
	defer func() { tgAPIBaseURL = oldBase }()

	// 2. 点击站点按钮应回复新消息并附带该站点对应分类按钮
	handleNotifyCallback(context.Background(), "123456:ABCDEF", &tgCallbackQuery{
		ID:   "cb_src",
		Data: fmt.Sprintf("nfp:%s:s:1", sess.BatchID),
		Message: &tgMessage{
			MessageID:       77,
			MessageThreadID: 9,
			Chat:            &tgChat{ID: -100111},
		},
	})

	// 3. 点击该站点的「电影」分类按钮应编辑回复消息展示对应片单与返回分类按钮
	handleNotifyCallback(context.Background(), "123456:ABCDEF", &tgCallbackQuery{
		ID:   "cb_cat",
		Data: fmt.Sprintf("nfp:%s:sc:1:0:1", sess.BatchID),
		Message: &tgMessage{
			MessageID: 88,
			Chat:      &tgChat{ID: -100111},
		},
	})

	// 4. 点击返回站点分类应编辑回该站点分类页
	handleNotifyCallback(context.Background(), "123456:ABCDEF", &tgCallbackQuery{
		ID:   "cb_back",
		Data: fmt.Sprintf("nfp:%s:sb:1", sess.BatchID),
		Message: &tgMessage{
			MessageID: 88,
			Chat:      &tgChat{ID: -100111},
		},
	})

	mu.Lock()
	defer mu.Unlock()
	var sawSendReply, sawEditList, sawEditBack bool
	for _, c := range calls {
		if strings.HasSuffix(c.Path, "/sendMessage") {
			if c.Body["reply_to_message_id"] == float64(77) && c.Body["message_thread_id"] == float64(9) && c.Body["allow_sending_without_reply"] == true {
				text, _ := c.Body["text"].(string)
				if strings.Contains(text, "站点明细 · HD(BF)") {
					sawSendReply = true
				}
			}
		}
		if strings.HasSuffix(c.Path, "/editMessageText") {
			text, _ := c.Body["text"].(string)
			if strings.Contains(text, "HD(BF) · 电影") && strings.Contains(text, "备站电影1") && !strings.Contains(text, "备站动漫1") {
				sawEditList = true
			}
			if strings.Contains(text, "站点明细 · HD(BF)") {
				sawEditBack = true
			}
		}
	}
	if !sawSendReply || !sawEditList || !sawEditBack {
		t.Fatalf("missing expected interactive calls: reply=%v list=%v back=%v calls=%+v", sawSendReply, sawEditList, sawEditBack, calls)
	}
}

func TestNotifyLedgerAggregationAndSQLPagination(t *testing.T) {
	setupNotifyConfigDB(t)
	gdb := db.Mdb
	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.Category{},
		&model.SourceCategory{},
		&model.CategoryMapping{},
		&model.PrimaryState{},
		&model.MappingRule{},
		&model.CollectRecord{},
		&model.CollectRecordFilmItem{},
	); err != nil {
		t.Fatal(err)
	}
	prevRdb := db.Rdb
	db.Rdb = nil
	prevSnap := taxonomy.CurrentMappingSnapshotForTest()
	sourcecatalog.InvalidatePrimarySourceCache()
	catalog.InvalidatePublicSourceCache()
	catalog.MarkCategoryChanged()
	t.Cleanup(func() {
		db.Rdb = prevRdb
		taxonomy.StoreMappingSnapshotForTest(prevSnap)
		sourcecatalog.InvalidatePrimarySourceCache()
		catalog.InvalidatePublicSourceCache()
		catalog.MarkCategoryChanged()
	})

	for _, item := range []any{
		&model.FilmSource{Id: "src_main", Name: "速博(SUBO)", Uri: "http://1", State: true, Sort: 0},
		&model.FilmSource{Id: "src_sub", Name: "HD(BF)", Uri: "http://2", State: true, Sort: 1},
		&model.PrimaryState{Id: 1, PrimarySourceId: "src_main"},
		&model.Category{Id: 10, Pid: 0, Name: "电影", StableKey: "cat_10", Show: true, Sort: 1},
		&model.Category{Id: 12, Pid: 0, Name: "隐藏", StableKey: "cat_12", Show: true, Sort: 2},
		&model.SourceCategory{SourceId: "src_main", SourceTypeId: 1, RawName: "电影"},
		&model.SourceCategory{SourceId: "src_main", SourceTypeId: 9, RawName: "隐藏"},
		&model.SourceCategory{SourceId: "src_sub", SourceTypeId: 2, RawName: "电影"},
		&model.CategoryMapping{SourceId: "src_main", SourceTypeId: 1, CategoryId: 10},
		&model.CategoryMapping{SourceId: "src_main", SourceTypeId: 9, CategoryId: 12},
		&model.CategoryMapping{SourceId: "src_sub", SourceTypeId: 2, CategoryId: 10},
		// 主站 1 部可见 + 1 部隐藏分类
		&model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 501, FirstSourceId: "src_main"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 10, Cid: 10, CategoryKey: "source:src_main:1", RootCategoryKey: "source:src_main:1"},
			FilmIndexContent:  model.FilmIndexContent{Name: "主站可见电影", UpdateStamp: 100},
		},
		&model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 502, FirstSourceId: "src_main"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 12, Cid: 12, CategoryKey: "source:src_main:9", RootCategoryKey: "source:src_main:9"},
			FilmIndexContent:  model.FilmIndexContent{Name: "主站隐藏片", UpdateStamp: 200},
		},
		// 备站 3 部电影（202 与 203 同时间戳按 mid DESC 排序） + 1 部其他
		&model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 601, FirstSourceId: "src_sub"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 10, Cid: 10, CategoryKey: "source:src_sub:2", RootCategoryKey: "source:src_sub:2"},
			FilmIndexContent:  model.FilmIndexContent{Name: "备站电影最旧", UpdateStamp: 100},
		},
		&model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 602, FirstSourceId: "src_sub"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 10, Cid: 10, CategoryKey: "source:src_sub:2", RootCategoryKey: "source:src_sub:2"},
			FilmIndexContent:  model.FilmIndexContent{Name: "备站电影次新", UpdateStamp: 300},
		},
		&model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 603, FirstSourceId: "src_sub"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 10, Cid: 10, CategoryKey: "source:src_sub:2", RootCategoryKey: "source:src_sub:2"},
			FilmIndexContent:  model.FilmIndexContent{Name: "备站电影最新", UpdateStamp: 300},
		},
		&model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 604, FirstSourceId: "src_sub"},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 0, Cid: 0, CategoryKey: "source:src_sub:999", RootCategoryKey: "source:src_sub:999"},
			FilmIndexContent:  model.FilmIndexContent{Name: "备站未映射片", UpdateStamp: 250},
		},
	} {
		if err := gdb.Create(item).Error; err != nil {
			t.Fatal(err)
		}
	}

	if err := gdb.Model(&model.Category{}).Where("id = ?", 12).Update("show", false).Error; err != nil {
		t.Fatal(err)
	}

	const collectBatchID = "batch_ledger_01"
	now := time.Now()
	if err := gdb.Create(&model.CollectRecord{
		BatchID:   collectBatchID,
		Trigger:   model.CollectTriggerManual,
		Status:    "done",
		Ledger:    true,
		StartedAt: now,
	}).Error; err != nil {
		t.Fatal(err)
	}
	ledgerItems := []model.CollectRecordFilmItem{
		{BatchID: collectBatchID, SourceID: "src_main", Mid: 501, CreatedAt: now},
		{BatchID: collectBatchID, SourceID: "src_main", Mid: 502, CreatedAt: now},
		{BatchID: collectBatchID, SourceID: "src_sub", Mid: 601, CreatedAt: now},
		{BatchID: collectBatchID, SourceID: "src_sub", Mid: 602, CreatedAt: now},
		{BatchID: collectBatchID, SourceID: "src_sub", Mid: 603, CreatedAt: now},
		{BatchID: collectBatchID, SourceID: "src_sub", Mid: 604, CreatedAt: now},
	}
	if err := gdb.Create(&ledgerItems).Error; err != nil {
		t.Fatal(err)
	}

	_ = sourcecatalog.RefreshCollectSourceCatalog()
	taxonomy.ReloadMappingRules()
	taxonomy.RefreshCategoryCache()
	sourcecatalog.InvalidatePrimarySourceCache()
	catalog.InvalidatePublicSourceCache()
	catalog.MarkCategoryChanged()

	// 不传入 NotifyMIDs 与 FilmMIDs，仅传 CollectBatchID，模拟大批量账本模式
	sess, err := buildNotifyBatchSession(model.CollectBatchNotifyPayload{
		CollectBatchID: collectBatchID,
		Trigger:        model.CollectTriggerManual,
		StartedAt:      now.Add(-15 * time.Second),
		FinishedAt:     now,
		Sources: []model.SourceNotifyResult{
			{SourceID: "src_main", SourceName: "速博(SUBO)", Status: model.CollectStatusDone, FilmsTotal: 2},
			{SourceID: "src_sub", SourceName: "HD(BF)", Status: model.CollectStatusDone, FilmsTotal: 4},
		},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if sess.CollectBatchID != collectBatchID {
		t.Fatalf("expected session CollectBatchID=%q, got %q", collectBatchID, sess.CollectBatchID)
	}
	if !strings.Contains(sess.OverviewHTML, "前台可见 <b>1</b> 部") {
		t.Fatalf("expected primary visible 1 in overview, got: %s", sess.OverviewHTML)
	}
	// 同批存在副站 NotifyMIDs 时，账本模式下主站前台可见数口径应保持一致
	sessWithSubMids, _, err := buildNotifyBatchSessionWithCount(model.CollectBatchNotifyPayload{
		CollectBatchID: collectBatchID,
		Trigger:        model.CollectTriggerManual,
		StartedAt:      now.Add(-15 * time.Second),
		FinishedAt:     now,
		NotifyMIDs:     []int64{601},
		Sources: []model.SourceNotifyResult{
			{SourceID: "src_main", SourceName: "速博(SUBO)", Status: model.CollectStatusDone, FilmsTotal: 2},
			{SourceID: "src_sub", SourceName: "HD(BF)", Status: model.CollectStatusDone, FilmsTotal: 4},
		},
	}, 2)
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(sessWithSubMids.OverviewHTML, "前台可见 <b>1</b> 部") {
		t.Fatalf("expected consistent visible count with non-empty NotifyMIDs, got: %s", sessWithSubMids.OverviewHTML)
	}
	for _, s := range sess.Sources {
		if len(s.AllMids) != 0 {
			t.Fatalf("expected empty AllMids in ledger mode for %s, got %d", s.SourceID, len(s.AllMids))
		}
		for _, c := range s.Categories {
			if len(c.Mids) != 0 {
				t.Fatalf("expected empty category Mids in ledger mode for %s/%s, got %d", s.SourceID, c.CategoryName, len(c.Mids))
			}
		}
	}
	sub := sess.Sources[1]
	if sub.TotalFilmCount() != 4 || len(sub.Categories) != 2 {
		t.Fatalf("unexpected sub source summary: total=%d cats=%+v", sub.TotalFilmCount(), sub.Categories)
	}
	if sub.Categories[0].CategoryName != "电影" || sub.Categories[0].FilmCount() != 3 ||
		sub.Categories[1].CategoryName != "其他" || sub.Categories[1].FilmCount() != 1 {
		t.Fatalf("unexpected sub source categories: %+v", sub.Categories)
	}
	if err := SaveNotifyBatchSession(sess); err != nil {
		t.Fatal(err)
	}

	var (
		mu    sync.Mutex
		edits []string
		reply string
	)
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		raw, _ := io.ReadAll(r.Body)
		var body map[string]any
		_ = json.Unmarshal(raw, &body)
		mu.Lock()
		text, _ := body["text"].(string)
		if strings.HasSuffix(r.URL.Path, "/sendMessage") {
			reply = text
		} else if strings.HasSuffix(r.URL.Path, "/editMessageText") {
			edits = append(edits, text)
		}
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_ = json.NewEncoder(w).Encode(map[string]any{"ok": true, "result": map[string]any{"message_id": 1}})
	}))
	defer ts.Close()
	oldBase := tgAPIBaseURL
	tgAPIBaseURL = ts.URL
	defer func() { tgAPIBaseURL = oldBase }()

	fire := func(data string) {
		handleNotifyCallback(context.Background(), "123456:ABCDEF", &tgCallbackQuery{
			ID:   "cb",
			Data: data,
			Message: &tgMessage{
				MessageID: 99,
				Chat:      &tgChat{ID: -100111},
			},
		})
	}
	fire(fmt.Sprintf("nfp:%s:s:1", sess.BatchID))       // 站点分类页
	fire(fmt.Sprintf("nfp:%s:sc:1:0:1", sess.BatchID))  // 电影第1页
	fire(fmt.Sprintf("nfp:%s:sc:1:0:2", sess.BatchID))  // 电影第2页
	fire(fmt.Sprintf("nfp:%s:sc:1:1:1", sess.BatchID))  // 其他第1页
	fire(fmt.Sprintf("nfp:%s:sc:1:-1:2", sess.BatchID)) // 全部第2页
	fire(fmt.Sprintf("nfp:%s:sb:1", sess.BatchID))      // 返回站点分类页

	mu.Lock()
	defer mu.Unlock()
	if !strings.Contains(reply, "站点明细 · HD(BF)") {
		t.Fatalf("expected site reply, got %q", reply)
	}
	if len(edits) != 5 {
		t.Fatalf("expected 5 edit calls, got %d: %+v", len(edits), edits)
	}
	// edit[0]: 电影第1页（最新2部：603, 602）
	if !strings.Contains(edits[0], "备站电影最新") || !strings.Contains(edits[0], "备站电影次新") || strings.Contains(edits[0], "备站电影最旧") {
		t.Fatalf("unexpected movie page 1: %s", edits[0])
	}
	// edit[1]: 电影第2页（最旧1部：601）
	if !strings.Contains(edits[1], "备站电影最旧") || strings.Contains(edits[1], "备站电影最新") {
		t.Fatalf("unexpected movie page 2: %s", edits[1])
	}
	// edit[2]: 其他第1页（604）
	if !strings.Contains(edits[2], "备站未映射片") || strings.Contains(edits[2], "备站电影") {
		t.Fatalf("unexpected other category page 1: %s", edits[2])
	}
	// edit[3]: 全部第2页（604 @250, 601 @100）
	if !strings.Contains(edits[3], "备站未映射片") || !strings.Contains(edits[3], "备站电影最旧") || strings.Contains(edits[3], "备站电影最新") {
		t.Fatalf("unexpected all category page 2: %s", edits[3])
	}
	// edit[4]: 返回站点分类页
	if !strings.Contains(edits[4], "站点明细 · HD(BF)") {
		t.Fatalf("unexpected back edit: %s", edits[4])
	}
}
