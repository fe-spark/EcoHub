package service

import (
	"context"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"sync"
	"testing"
	"time"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider/progress"
)

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

	// 测试非法 threadId
	bad := incoming
	bad.ChatIDs = []string{"-100123:abc"}
	if _, err := validateAndMergeNotifyConfig(stored, bad); err == nil {
		t.Fatalf("expected error on invalid threadId")
	}
}

func TestNotifyCallbackAndPagination(t *testing.T) {
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

	if kb := buildCategoryKeyboardForSite("", sess.Categories, len(sess.AllFilms)); kb != nil {
		t.Fatalf("expected nil keyboard when siteURL is empty, got %+v", kb)
	}

	kb := buildCategoryKeyboardForSite("http://localhost:3300/", sess.Categories, len(sess.AllFilms))
	if kb == nil || len(kb.InlineKeyboard) == 0 {
		t.Fatalf("expected non-empty category keyboard when siteURL is http://localhost:3300/")
	}
	if got := kb.InlineKeyboard[0][0].URL; got != "http://localhost:3300/category/filter?pid=1" {
		t.Fatalf("unexpected category button URL: %q", got)
	}
	if got := kb.InlineKeyboard[0][1].URL; got != "http://localhost:3300" {
		t.Fatalf("unexpected all button URL: %q", got)
	}

	batchID, action, catIdx, page, ok := parseNotifyCallback("nfp:b123:0:1")
	if !ok || batchID != "b123" || action != "page" || catIdx != 0 || page != 1 {
		t.Fatalf("unexpected callback parse result: batch=%s action=%s catIdx=%d page=%d ok=%v", batchID, action, catIdx, page, ok)
	}

	pageHTML, totalPages := formatFilmListPageHTML(sess, 0, 1)
	if totalPages != 2 || !strings.Contains(pageHTML, "影片A") || !strings.Contains(pageHTML, "影片B") || strings.Contains(pageHTML, "影片C") {
		t.Fatalf("unexpected page 1 html (totalPages=%d): %s", totalPages, pageHTML)
	}

	page2HTML, _ := formatFilmListPageHTML(sess, 0, 2)
	if !strings.Contains(page2HTML, "影片C") {
		t.Fatalf("expected page 2 to contain 影片C: %s", page2HTML)
	}
}

func TestSendTestAndCallbackEditWithMockServer(t *testing.T) {
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
	if !strings.Contains(out, "定时任务") || !strings.Contains(out, "1分5秒") || !strings.Contains(out, "资源站A</b> (9 部)") || !strings.Contains(out, "connection timeout") {
		t.Fatalf("unexpected overview HTML: %s", out)
	}
}

func TestFormatSourceOrderChangeHTML(t *testing.T) {
	oldPrimary := &model.FilmSource{Id: "s1", Name: "量子资源"}
	newPrimary := &model.FilmSource{Id: "s2", Name: "非凡资源"}
	enabled := []model.FilmSource{
		{Id: "s2", Name: "非凡资源", State: true},
		{Id: "s1", Name: "量子资源", State: true},
		{Id: "s3", Name: "暴风资源", State: true},
	}

	switchedHTML := formatSourceOrderChangeHTML("EcoHub", oldPrimary, newPrimary, enabled)
	if !strings.Contains(switchedHTML, "主站已切换") ||
		!strings.Contains(switchedHTML, "<b>量子资源</b> → <b>非凡资源</b>") ||
		!strings.Contains(switchedHTML, "<b>非凡资源</b> → 量子资源 → 暴风资源") {
		t.Fatalf("unexpected switched HTML: %s", switchedHTML)
	}

	reorderedHTML := formatSourceOrderChangeHTML("EcoHub", newPrimary, newPrimary, enabled)
	if !strings.Contains(reorderedHTML, "采集站排序已更新") ||
		!strings.Contains(reorderedHTML, "⭐ 主站：<b>非凡资源</b>") ||
		!strings.Contains(reorderedHTML, "<b>非凡资源</b> → 量子资源 → 暴风资源") {
		t.Fatalf("unexpected reordered HTML: %s", reorderedHTML)
	}
}
