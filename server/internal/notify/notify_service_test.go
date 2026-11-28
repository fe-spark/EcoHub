package notify

import (
	"context"
	"errors"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"server/internal/film/catalog"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupNotifyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(
		&model.SiteConfigRecord{},
		&model.NotifyConfigRecord{},
		&model.ProxyConfigRecord{},
		&model.FilmSource{},
		&model.PrimaryState{},
		&model.Category{},
		&model.SourceCategory{},
		&model.CategoryMapping{},
		&model.CollectRecord{},
		&model.FilmIndex{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	prevMdb := db.Mdb
	prevRdb := db.Rdb
	db.Mdb = gdb
	db.Rdb = nil
	t.Cleanup(func() {
		db.Mdb = prevMdb
		db.Rdb = prevRdb
	})
	return gdb
}

func setupNotifyConfigDB(t *testing.T) {
	t.Helper()
	setupNotifyTestDB(t)
}

func TestParseNotifyCallbackCmd_TwoLevelPrimaryProtocol(t *testing.T) {
	cases := []struct {
		raw    string
		ok     bool
		action string
		catIdx int
		page   int
	}{
		{"nfp:abc123:noop:0", true, "noop", -1, 0},
		{"nfp:abc123:info:0", true, "info", -1, 0},
		{"nfp:abc123:home:0", true, "home", -1, 0},
		{"nfp:abc123:c:-1:1", true, "cat_page", -1, 1},
		{"nfp:abc123:c:2:3", true, "cat_page", 2, 3},
		{"nfp:abc123:c:0:0", false, "", 0, 0},
		{"nfp:abc123:s:0", false, "", 0, 0},
		{"invalid", false, "", 0, 0},
	}
	for _, tc := range cases {
		cmd, ok := parseNotifyCallbackCmd(tc.raw)
		if ok != tc.ok {
			t.Fatalf("raw=%q ok=%v want %v", tc.raw, ok, tc.ok)
		}
		if ok && (cmd.action != tc.action || cmd.catIdx != tc.catIdx || cmd.page != tc.page) {
			t.Fatalf("raw=%q got %+v want action=%s cat=%d page=%d", tc.raw, cmd, tc.action, tc.catIdx, tc.page)
		}
	}
}

func TestBuildPrimaryOverviewAndSingleTablePagination(t *testing.T) {
	gdb := setupNotifyTestDB(t)

	_ = gdb.Create(&model.SiteConfigRecord{
		SiteName: "测试影视",
		SiteURL:  "https://eco.example.com",
	}).Error

	primarySrc := model.FilmSource{Id: "src_primary", Name: "主站A", Uri: "https://a.example", State: true, Sort: 0}
	otherSrc := model.FilmSource{Id: "src_other", Name: "副站B", Uri: "https://b.example", State: true, Sort: 1}
	_ = gdb.Create(&primarySrc).Error
	_ = gdb.Create(&otherSrc).Error
	_ = gdb.Create(&model.PrimaryState{Id: 1, PrimarySourceId: primarySrc.Id}).Error
	_ = sourcecatalog.RefreshCollectSourceCatalog()

	_ = gdb.Create(&model.Category{Id: 1, Name: "电影", StableKey: "movie", Show: true}).Error
	_ = gdb.Create(&model.Category{Id: 2, Name: "剧集", StableKey: "series", Show: true}).Error
	_ = gdb.Create(&model.SourceCategory{SourceId: primarySrc.Id, SourceTypeId: 1, RawName: "电影"}).Error
	_ = gdb.Create(&model.SourceCategory{SourceId: primarySrc.Id, SourceTypeId: 11, ParentSourceTypeId: 1, RawName: "动作片"}).Error
	_ = gdb.Create(&model.SourceCategory{SourceId: primarySrc.Id, SourceTypeId: 2, RawName: "剧集"}).Error
	_ = gdb.Create(&model.SourceCategory{SourceId: primarySrc.Id, SourceTypeId: 21, ParentSourceTypeId: 2, RawName: "国产剧"}).Error
	_ = gdb.Create(&model.CategoryMapping{SourceId: primarySrc.Id, SourceTypeId: 11, CategoryId: 1}).Error
	_ = gdb.Create(&model.CategoryMapping{SourceId: primarySrc.Id, SourceTypeId: 21, CategoryId: 2}).Error
	catalog.ClearCategoryCache()
	taxonomy.RefreshCategoryCache()
	t.Cleanup(catalog.ClearCategoryCache)

	rec := &model.CollectRecord{
		BatchID:   "batch_primary_1",
		Trigger:   model.CollectTriggerCron,
		Status:    model.CollectRecordStatusPartial,
		StartedAt: time.Now().Add(-30 * time.Second),
	}
	if err := gdb.Create(rec).Error; err != nil {
		t.Fatal(err)
	}

	// 主站更新 3 部（电影 2 部、剧集 1 部），副站更新 1 部不应混入主站片单
	films := []model.FilmIndex{
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 101, FirstSourceId: primarySrc.Id, LastRecordID: rec.ID},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 11, RootCategoryKey: taxonomy.BuildSourceCategoryKey(primarySrc.Id, 1), CategoryKey: taxonomy.BuildSourceCategoryKey(primarySrc.Id, 11)},
			FilmIndexContent:  model.FilmIndexContent{Name: "流浪地球"},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 102, FirstSourceId: primarySrc.Id, LastRecordID: rec.ID},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 11, RootCategoryKey: taxonomy.BuildSourceCategoryKey(primarySrc.Id, 1), CategoryKey: taxonomy.BuildSourceCategoryKey(primarySrc.Id, 11)},
			FilmIndexContent:  model.FilmIndexContent{Name: "星际穿越"},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 103, FirstSourceId: primarySrc.Id, LastRecordID: rec.ID},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 2, Cid: 21, RootCategoryKey: taxonomy.BuildSourceCategoryKey(primarySrc.Id, 2), CategoryKey: taxonomy.BuildSourceCategoryKey(primarySrc.Id, 21)},
			FilmIndexContent:  model.FilmIndexContent{Name: "三体"},
		},
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 104, FirstSourceId: otherSrc.Id, LastRecordID: rec.ID},
			FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 11, RootCategoryKey: taxonomy.BuildSourceCategoryKey(otherSrc.Id, 1), CategoryKey: taxonomy.BuildSourceCategoryKey(otherSrc.Id, 11)},
			FilmIndexContent:  model.FilmIndexContent{Name: "副站影片"},
		},
	}
	for i := range films {
		if err := gdb.Create(&films[i]).Error; err != nil {
			t.Fatal(err)
		}
	}

	payload := model.CollectBatchNotifyPayload{
		CollectRecordID: rec.ID,
		CollectBatchID:  rec.BatchID,
		Trigger:         model.CollectTriggerCron,
		StartedAt:       time.Now().Add(-30 * time.Second),
		FinishedAt:      time.Now(),
		Sources: []model.SourceNotifyResult{
			{SourceID: primarySrc.Id, SourceName: primarySrc.Name, Status: model.CollectRecordStatusSuccess, FilmsTotal: 3, PageTotal: 2, SuccessCnt: 2},
			{SourceID: otherSrc.Id, SourceName: otherSrc.Name, Status: model.CollectRecordStatusFailed, FilmsTotal: 1, FailedCnt: 2, Error: "请求超时"},
		},
	}

	sess, err := buildNotifyBatchSession(payload, 2)
	if err != nil {
		t.Fatalf("buildNotifyBatchSession: %v", err)
	}
	if sess.PrimarySourceID != primarySrc.Id || sess.PrimaryTotal != 3 || len(sess.Categories) != 2 {
		t.Fatalf("unexpected primary session: %+v", sess)
	}
	if !strings.Contains(sess.OverviewHTML, "主站A") || !strings.Contains(sess.OverviewHTML, "副站B") || !strings.Contains(sess.OverviewHTML, "请求超时") {
		t.Fatalf("overview missing expected info: %s", sess.OverviewHTML)
	}

	manageURL := buildManageRecordURL("https://eco.example.com", rec.BatchID)
	kb := buildPrimaryOverviewKeyboard(sess, manageURL)
	if kb == nil || len(kb.InlineKeyboard) < 2 {
		t.Fatalf("expected category buttons + manage URL button, got %+v", kb)
	}
	lastRow := kb.InlineKeyboard[len(kb.InlineKeyboard)-1]
	if len(lastRow) != 1 || lastRow[0].URL != manageURL {
		t.Fatalf("expected manage URL button %q, got %+v", manageURL, lastRow)
	}

	// 验证单表分页：全部第 1 页应返回 2 条，第 2 页返回 1 条且不含副站影片
	page1, cur1, total1, err := formatPrimaryFilmListPageHTML(context.Background(), sess, -1, 1)
	if err != nil || cur1 != 1 || total1 != 2 {
		t.Fatalf("page1 cur=%d total=%d err=%v", cur1, total1, err)
	}
	if !strings.Contains(page1, "流浪地球") || !strings.Contains(page1, "星际穿越") || strings.Contains(page1, "副站影片") {
		t.Fatalf("unexpected page1 html: %s", page1)
	}

	page2, cur2, total2, err := formatPrimaryFilmListPageHTML(context.Background(), sess, -1, 2)
	if err != nil || cur2 != 2 || total2 != 2 || !strings.Contains(page2, "三体") {
		t.Fatalf("unexpected page2 cur=%d total=%d err=%v html=%s", cur2, total2, err, page2)
	}

	// 验证分类筛选（剧集 catIdx=1）
	catPage, _, catTotal, err := formatPrimaryFilmListPageHTML(context.Background(), sess, 1, 1)
	if err != nil || catTotal != 1 || !strings.Contains(catPage, "三体") || strings.Contains(catPage, "流浪地球") {
		t.Fatalf("unexpected cat page total=%d err=%v html=%s", catTotal, err, catPage)
	}

	// 验证主站切换后翻看旧通知仍查原主站，不会串数据
	if err := sourcecatalog.SetPrimarySourceID(otherSrc.Id); err != nil {
		t.Fatalf("SetPrimarySourceID: %v", err)
	}
	oldSessPage, _, _, err := formatPrimaryFilmListPageHTML(context.Background(), sess, -1, 1)
	if err != nil || !strings.Contains(oldSessPage, "流浪地球") || strings.Contains(oldSessPage, "副站影片") {
		t.Fatalf("old session after primary switch got unexpected html: err=%v html=%s", err, oldSessPage)
	}

	// 验证当部分影片被后续批次覆写 last_record_id 导致第 2 页变空时，回退渲染第 1 页而非展示空尾页
	_ = gdb.Model(&model.FilmIndex{}).Where("mid IN ?", []int64{102, 103}).Update("last_record_id", rec.ID+99).Error
	fallbackHTML, curF, totalF, err := formatPrimaryFilmListPageHTML(context.Background(), sess, -1, 2)
	if err != nil || curF != 1 || totalF != 1 || !strings.Contains(fallbackHTML, "流浪地球") {
		t.Fatalf("expected fallback to page 1/1 with remaining film, got cur=%d total=%d err=%v html=%s", curF, totalF, err, fallbackHTML)
	}
}

func TestBuildPrimaryOverview_ZeroPrimaryWithFailedSecondary(t *testing.T) {
	gdb := setupNotifyTestDB(t)
	_ = gdb.Create(&model.SiteConfigRecord{SiteName: "测试站", SiteURL: "https://eco.example.com"}).Error
	primarySrc := model.FilmSource{Id: "src_a", Name: "主站A", Uri: "https://a.example", State: true, Sort: 0}
	otherSrc := model.FilmSource{Id: "src_b", Name: "副站B", Uri: "https://b.example", State: true, Sort: 1}
	_ = gdb.Create(&primarySrc).Error
	_ = gdb.Create(&otherSrc).Error
	_ = gdb.Create(&model.PrimaryState{Id: 1, PrimarySourceId: primarySrc.Id}).Error
	_ = sourcecatalog.RefreshCollectSourceCatalog()

	rec := &model.CollectRecord{BatchID: "batch_zero_p", Trigger: model.CollectTriggerManual, Status: model.CollectRecordStatusPartial, StartedAt: time.Now()}
	_ = gdb.Create(rec).Error

	payload := model.CollectBatchNotifyPayload{
		CollectRecordID: rec.ID,
		CollectBatchID:  rec.BatchID,
		Trigger:         model.CollectTriggerManual,
		StartedAt:       time.Now().Add(-time.Second),
		FinishedAt:      time.Now(),
		Sources: []model.SourceNotifyResult{
			{SourceID: primarySrc.Id, SourceName: primarySrc.Name, Status: model.CollectRecordStatusSuccess, FilmsTotal: 0},
			{SourceID: otherSrc.Id, SourceName: otherSrc.Name, Status: model.CollectStatusFailed, FilmsTotal: 0, FailedCnt: 3, Error: "连接重置"},
		},
	}
	sess, err := buildNotifyBatchSession(payload, 15)
	if err != nil {
		t.Fatalf("buildNotifyBatchSession: %v", err)
	}
	if sess.PrimaryTotal != 0 || len(sess.Categories) != 0 {
		t.Fatalf("expected 0 primary total & no categories, got %+v", sess)
	}
	if !strings.Contains(sess.OverviewHTML, "副站B") || !strings.Contains(sess.OverviewHTML, "连接重置") {
		t.Fatalf("overview missing failed source details: %s", sess.OverviewHTML)
	}
	kb := buildPrimaryOverviewKeyboard(sess, buildManageRecordURL("https://eco.example.com", rec.BatchID))
	if kb == nil || len(kb.InlineKeyboard) != 1 || kb.InlineKeyboard[0][0].CallbackData != "" || kb.InlineKeyboard[0][0].URL == "" {
		t.Fatalf("expected only manage URL button when primaryTotal=0, got %+v", kb)
	}
}

func TestBuildPrimaryOverview_StatsTimeoutDegrades(t *testing.T) {
	gdb := setupNotifyTestDB(t)
	_ = gdb.Create(&model.SiteConfigRecord{SiteName: "测试站", SiteURL: "https://eco.example.com"}).Error
	primarySrc := model.FilmSource{Id: "src_t", Name: "主站T", Uri: "https://t.example", State: true, Sort: 0}
	_ = gdb.Create(&primarySrc).Error
	_ = gdb.Create(&model.PrimaryState{Id: 1, PrimarySourceId: primarySrc.Id}).Error
	_ = sourcecatalog.RefreshCollectSourceCatalog()

	prev := loadBatchPrimaryCategoryStatsFn
	loadBatchPrimaryCategoryStatsFn = func(context.Context, uint, string) ([]BatchSourceCategoryStatRow, error) {
		return nil, context.DeadlineExceeded
	}
	t.Cleanup(func() { loadBatchPrimaryCategoryStatsFn = prev })

	sess, err := buildNotifyBatchSession(model.CollectBatchNotifyPayload{
		CollectRecordID: 1,
		CollectBatchID:  "b_timeout",
		Trigger:         model.CollectTriggerCron,
		StartedAt:       time.Now().Add(-time.Second),
		FinishedAt:      time.Now(),
		Sources:         []model.SourceNotifyResult{{SourceID: primarySrc.Id, SourceName: primarySrc.Name, Status: model.CollectRecordStatusSuccess, FilmsTotal: 5}},
	}, 15)
	if err != nil {
		t.Fatalf("buildNotifyBatchSession: %v", err)
	}
	if !strings.Contains(sess.OverviewHTML, "统计超时") {
		t.Fatalf("expected timeout notice in overview, got %s", sess.OverviewHTML)
	}
}

func TestPublishBatchSummary_OnlyNotifyOnUpdateHandlesPartialAndInterrupted(t *testing.T) {
	gdb := setupNotifyTestDB(t)
	_ = gdb.Create(&model.SiteConfigRecord{SiteName: "测试站"}).Error
	if err := SaveNotifyConfig(model.NotifyConfig{
		Enabled:            true,
		BotToken:           "123456:TESTTOKEN",
		ChatIDs:            []string{"10001"},
		Events:             model.NotifyEventSwitches{CollectBatchSummary: true},
		OnlyNotifyOnUpdate: true,
	}); err != nil {
		t.Fatal(err)
	}

	sent := make(chan string, 4)
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		sent <- r.URL.Path
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":{"message_id":1}}`))
	}))
	defer srv.Close()
	prevBase := tgAPIBaseURL
	tgAPIBaseURL = srv.URL
	t.Cleanup(func() { tgAPIBaseURL = prevBase })

	for _, status := range []string{model.CollectRecordStatusPartial, model.CollectRecordStatusInterrupted} {
		NotifySvc.PublishBatchSummary(model.CollectBatchNotifyPayload{
			CollectBatchID: "b_" + status,
			Trigger:        model.CollectTriggerCron,
			StartedAt:      time.Now().Add(-time.Second),
			FinishedAt:     time.Now(),
			Sources: []model.SourceNotifyResult{
				{SourceID: "s1", SourceName: "站1", Status: status, FilmsTotal: 0},
			},
		})
		select {
		case <-sent:
		case <-time.After(2 * time.Second):
			t.Fatalf("expected OnlyNotifyOnUpdate to send alert for status=%s even with Error='' and FilmsTotal=0", status)
		}
	}
}

func TestHandleNotifyCallback_PageAndBackHome(t *testing.T) {
	gdb := setupNotifyTestDB(t)

	cfg := model.NotifyConfig{
		Enabled:  true,
		BotToken: "123456:TESTTOKEN",
		ChatIDs:  []string{"10001"},
		Events:   model.NotifyEventSwitches{CollectBatchSummary: true},
	}
	if err := SaveNotifyConfig(cfg); err != nil {
		t.Fatal(err)
	}

	var mu sync.Mutex
	var methods []string
	srv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		mu.Lock()
		parts := strings.Split(strings.Trim(r.URL.Path, "/"), "/")
		methods = append(methods, parts[len(parts)-1])
		mu.Unlock()
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"ok":true,"result":true}`))
	}))
	defer srv.Close()

	prevBase := tgAPIBaseURL
	tgAPIBaseURL = srv.URL
	t.Cleanup(func() { tgAPIBaseURL = prevBase })

	sess := model.NotifyBatchSession{
		BatchID:           "cb1234",
		CollectBatchID:    "b_cb_1",
		CollectRecordID:   9,
		SiteName:          "测试站",
		PrimarySourceID:   "src_1",
		PrimarySourceName: "主站1",
		PrimaryTotal:      1,
		OverviewHTML:      "<b>汇总</b>",
		PageSize:          10,
		Categories:        []model.NotifySourceCategoryGroup{{CategoryID: 1, CategoryName: "电影", Count: 1}},
	}
	if err := SaveNotifyBatchSession(sess); err != nil {
		t.Fatal(err)
	}
	_ = gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 201, FirstSourceId: "src_1", LastRecordID: 9},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1},
		FilmIndexContent:  model.FilmIndexContent{Name: "测试新片"},
	}).Error

	cbPage := &tgCallbackQuery{
		ID:      "qid-1",
		Data:    "nfp:cb1234:c:-1:1",
		Message: &tgMessage{MessageID: 55, Chat: &tgChat{ID: 10001}},
	}
	handleNotifyCallback(context.Background(), cfg.BotToken, cbPage)

	cbHome := &tgCallbackQuery{
		ID:      "qid-2",
		Data:    "nfp:cb1234:home:0",
		Message: &tgMessage{MessageID: 55, Chat: &tgChat{ID: 10001}},
	}
	handleNotifyCallback(context.Background(), cfg.BotToken, cbHome)

	mu.Lock()
	got := strings.Join(methods, ",")
	mu.Unlock()
	if !strings.Contains(got, "editMessageText") || !strings.Contains(got, "answerCallbackQuery") {
		t.Fatalf("expected editMessageText & answerCallbackQuery, got %s", got)
	}
}

func TestClampPageRange_CapsAtMaxNotifyListPages(t *testing.T) {
	cur, totalPages, start, end := clampPageRange(5000, 15, 999)
	if totalPages != maxNotifyListPages || cur != maxNotifyListPages || start != (maxNotifyListPages-1)*15 || end != maxNotifyListPages*15 {
		t.Fatalf("unexpected clamp result cur=%d totalPages=%d start=%d end=%d", cur, totalPages, start, end)
	}
	var _ = errors.New
}
