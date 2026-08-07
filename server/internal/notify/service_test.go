package notify

import (
	"fmt"
	"strings"
	"testing"
	"time"

	"server/internal/model"
	"server/internal/repository"
)

func TestMaskBotToken(t *testing.T) {
	if MaskBotToken("") != "" {
		t.Fatal("empty token")
	}
	masked := MaskBotToken("123456:ABCDEFGHIJKLMNOP")
	if !strings.Contains(masked, "***") {
		t.Fatalf("expected mask, got %s", masked)
	}
	if strings.Contains(masked, "ABCDEF") {
		t.Fatalf("should not leak middle: %s", masked)
	}
	if !IsMaskedToken(masked) {
		t.Fatalf("masked form should be detected: %s", masked)
	}
	if IsMaskedToken("123456:ABCDEFGHIJKLMNOP") {
		t.Fatal("real token should not be treated as masked")
	}
	if IsMaskedToken("foo***bar") {
		t.Fatal("arbitrary *** should not match mask shape")
	}
}

func TestSanitizeTelegramErr(t *testing.T) {
	token := "1234567890:AAFakeTokenForUnitTestOnly_xyz"
	raw := fmt.Errorf(`Post "https://api.telegram.org/bot%s/sendMessage": context deadline exceeded`, token)
	got := sanitizeTelegramErr(raw, token, "").Error()
	if strings.Contains(got, token) {
		t.Fatalf("token leaked: %s", got)
	}
	if strings.Contains(got, "AAFakeToken") {
		t.Fatalf("token fragment leaked: %s", got)
	}
	if !strings.Contains(got, "TELEGRAM_PROXY") {
		t.Fatalf("expected proxy hint: %s", got)
	}
	got2 := sanitizeTelegramErr(raw, token, "http://127.0.0.1:7890").Error()
	if !strings.Contains(got2, "7890") || strings.Contains(got2, token) {
		t.Fatalf("proxy path: %s", got2)
	}
}

func TestValidateAndMergeUpdateKeepToken(t *testing.T) {
	old := model.NotifyConfig{
		BotToken: "123456:REALTOKENVALUE",
		ChatIDs:  []string{"111"},
	}
	incoming := model.NotifyConfig{
		Enabled:  true,
		BotToken: MaskBotToken(old.BotToken),
		ChatIDs:  []string{"111", "222"},
		Events: model.NotifyEventSwitches{
			CollectBatchSummary: true,
		},
		MaxFilmsInMessage: 15,
		MinIntervalSec:    60,
	}
	merged, err := ValidateAndMergeUpdate(old, incoming)
	if err != nil {
		t.Fatal(err)
	}
	if merged.BotToken != old.BotToken {
		t.Fatalf("token should be preserved, got %s", merged.BotToken)
	}
	if len(merged.ChatIDs) != 2 {
		t.Fatalf("chat ids: %v", merged.ChatIDs)
	}
}

func TestValidateChatID(t *testing.T) {
	old := model.NotifyConfig{}
	_, err := ValidateAndMergeUpdate(old, model.NotifyConfig{
		Enabled:           false,
		ChatIDs:           []string{"not valid!"},
		MaxFilmsInMessage: 15,
	})
	if err == nil {
		t.Fatal("expected invalid chat id error")
	}
	cfg, err := ValidateAndMergeUpdate(old, model.NotifyConfig{
		Enabled:           false,
		ChatIDs:           []string{"-100123", "@mychannel"},
		MaxFilmsInMessage: 15,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ChatIDs) != 2 {
		t.Fatalf("got %v", cfg.ChatIDs)
	}
}

func TestValidateMaxFilmsInMessageCap(t *testing.T) {
	old := model.NotifyConfig{}
	_, err := ValidateAndMergeUpdate(old, model.NotifyConfig{
		Enabled:           false,
		MaxFilmsInMessage: 21,
	})
	if err == nil {
		t.Fatal("expected maxFilmsInMessage over cap to fail")
	}
	cfg, err := ValidateAndMergeUpdate(old, model.NotifyConfig{
		Enabled:           false,
		MaxFilmsInMessage: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxFilmsInMessage != 15 {
		t.Fatalf("default want 15, got %d", cfg.MaxFilmsInMessage)
	}
}

func TestNormalizeChatIDsViaRepo(t *testing.T) {
	got := repository.NormalizeChatIDs([]string{" 111 ", "", "111", "222"})
	if len(got) != 2 || got[0] != "111" || got[1] != "222" {
		t.Fatalf("got %v", got)
	}
	if len(repository.NormalizeChatIDs(nil)) != 0 {
		t.Fatal("nil should yield empty slice")
	}
}

func TestMidAccumulatorCountsOnly(t *testing.T) {
	acc := NewMidAccumulator()
	// 无 DB 时 batch 为 nil，落库为空操作，计数仍工作
	acc.Add(nil, "src1", 1, 2, 2, 3)
	_, total, _ := acc.DrainSource("src1")
	if total != 3 {
		t.Fatalf("total=%d", total)
	}
	_, total2, _ := acc.DrainSource("src1")
	if total2 != 0 {
		t.Fatalf("after drain total=%d", total2)
	}
}

func TestFormatBatchOverview(t *testing.T) {
	payload := model.CollectBatchNotifyPayload{
		Trigger:        model.NotifyTriggerManual,
		SiteName:       "测试站",
		DurationSec:    90,
		SuccessSources: 1,
		FailedSources:  1,
		TotalFilms:     2,
		Sources: []model.SourceNotifyResult{
			{
				SourceName: "主站A",
				Grade:      0,
				Status:     "done",
				SuccessCnt: 2,
				FilmsTotal: 2,
			},
			{
				SourceName: "附属B",
				Grade:      1,
				Status:     "failed",
				Error:      "timeout <script>",
				FailedCnt:  3,
			},
		},
	}
	joined := formatBatchOverview(payload, 2, 15)
	if !strings.Contains(joined, "测试站") {
		t.Fatalf("missing site name: %s", joined)
	}
	if !strings.Contains(joined, "变更") || !strings.Contains(joined, "失败") || !strings.Contains(joined, "成功页") {
		t.Fatalf("expected source metrics: %s", joined)
	}
	if !strings.Contains(joined, "更新列表") {
		t.Fatalf("expected film list hint: %s", joined)
	}
	if strings.Contains(joined, "<script>") {
		t.Fatal("should escape html")
	}
	if !strings.Contains(joined, "timeout") {
		t.Fatal("missing error")
	}
	if strings.Contains(joined, "<pre>") {
		t.Fatalf("overview must not use pre/code block: %s", joined)
	}
	if !strings.Contains(joined, "主站A") || !strings.Contains(joined, "合计") {
		t.Fatalf("expected source list + total: %s", joined)
	}
}

func TestParsePageCallback(t *testing.T) {
	prev := sitePlayBaseURLFn
	sitePlayBaseURLFn = func() string { return "" }
	t.Cleanup(func() { sitePlayBaseURLFn = prev })

	sid, page, kind, ok := parsePagedCallback(callbackPrefix, "nfp:abc123:2")
	if !ok || sid != "abc123" || page != 2 || kind != "page" {
		t.Fatalf("parse page: %v %v %v %v", sid, page, kind, ok)
	}
	_, _, kind, ok = parsePagedCallback(callbackPrefix, "nfp:abc123:open")
	if !ok || kind != "open" {
		t.Fatalf("parse open: %v %v", kind, ok)
	}
	_, _, kind, ok = parsePagedCallback(callbackPrefix, "nfp:abc123:back")
	if !ok || kind != "back" {
		t.Fatalf("parse back: %v %v", kind, ok)
	}
	ov := buildOverviewKeyboard("abc123")
	if ov == nil || len(ov.InlineKeyboard) != 1 || !strings.Contains(ov.InlineKeyboard[0][0].Text, "更新列表") {
		t.Fatalf("overview keyboard: %+v", ov)
	}
	kb := buildPagedKeyboard(callbackPrefix, "abc123", 2, 3, true)
	if kb == nil || len(kb.InlineKeyboard) < 2 {
		t.Fatalf("keyboard: %+v", kb)
	}
}

func TestIsAllowedChatUsernameMatch(t *testing.T) {
	cfg := model.NotifyConfig{ChatIDs: []string{"-100123", "@mychannel"}}
	if !chatAllowed(cfg, "-100123", "") {
		t.Fatal("numeric chat id should match")
	}
	if !chatAllowed(cfg, "-100456", "mychannel") {
		t.Fatal("@username should match via chat username")
	}
	if !chatAllowed(cfg, "-100456", "@mychannel") {
		t.Fatal("@username with @ prefix should match")
	}
	if chatAllowed(cfg, "-100789", "") {
		t.Fatal("unknown chat should be rejected")
	}
	if chatAllowed(cfg, "-100789", "other") {
		t.Fatal("unknown username should be rejected")
	}
}

func TestParseBotCommand(t *testing.T) {
	cmd, args := parseBotCommand("/search 流浪地球")
	if cmd != "search" || args != "流浪地球" {
		t.Fatalf("got %q %q", cmd, args)
	}
	cmd, args = parseBotCommand("/s@MyBot 关键词")
	if cmd != "s" || args != "关键词" {
		t.Fatalf("got %q %q", cmd, args)
	}
	cmd, args = parseBotCommand("hello")
	if cmd != "" || args != "" {
		t.Fatalf("non-cmd: %q %q", cmd, args)
	}
}

func TestFormatFilmLine(t *testing.T) {
	prev := sitePlayBaseURLFn
	t.Cleanup(func() { sitePlayBaseURLFn = prev })

	sitePlayBaseURLFn = func() string { return "" }
	line := formatFilmLine(model.FilmNotifyItem{Mid: 42, Name: "测试片"})
	if !strings.Contains(line, "测试片") || !strings.Contains(line, "#42") {
		t.Fatalf("unexpected film line: %s", line)
	}
	if strings.Contains(line, "href=") {
		t.Fatalf("empty siteUrl should not link: %s", line)
	}

	sitePlayBaseURLFn = func() string { return "https://demo.example.com" }
	linked := formatFilmLine(model.FilmNotifyItem{Mid: 42, Name: "测试片"})
	if !strings.Contains(linked, `href="https://demo.example.com/play?id=42"`) {
		t.Fatalf("expected play link: %s", linked)
	}
	if !strings.Contains(linked, ">测试片<") {
		t.Fatalf("name should be link text: %s", linked)
	}
}

func TestFormatProgressStale(t *testing.T) {
	msgs := formatProgressStale("站", "源A", "sid", "age=1m", time.Now())
	joined := strings.Join(msgs, "\n")
	if !strings.Contains(joined, "采集进度超时") {
		t.Fatalf("expected stale title: %s", joined)
	}
}

func TestBuildBatchPayloadCountsStopped(t *testing.T) {
	payload := BuildBatchPayload(nil, model.NotifyTriggerManual, []model.SourceNotifyResult{
		{Status: "done", FilmsTotal: 1},
		{Status: "stopped"},
		{Status: "failed"},
	}, time.Now().Add(-time.Minute), time.Now(), "")
	if payload.SuccessSources != 1 || payload.FailedSources != 2 {
		t.Fatalf("success=%d failed=%d", payload.SuccessSources, payload.FailedSources)
	}
}

func TestRateLimiter(t *testing.T) {
	r := &rateLimiter{last: make(map[string]time.Time)}
	if !r.allow("k", time.Minute) {
		t.Fatal("first should allow")
	}
	if r.allow("k", time.Minute) {
		t.Fatal("second should block")
	}
	if !r.allow("other", time.Minute) {
		t.Fatal("other key should allow")
	}
}

func TestFormatSearchListPage(t *testing.T) {
	prev := sitePlayBaseURLFn
	sitePlayBaseURLFn = func() string { return "" }
	t.Cleanup(func() { sitePlayBaseURLFn = prev })

	mids := make([]int64, 0, 15)
	for i := 1; i <= 15; i++ {
		mids = append(mids, int64(i))
	}
	sess := searchSession{
		SiteName: "搜站",
		PageSize: 10,
		Mids:     mids,
		Keyword:  "片",
		HitTotal: 15,
	}
	p1 := formatSearchListPage(sess, 1)
	if !strings.Contains(p1, "1/2") || !strings.Contains(p1, "#1") {
		t.Fatalf("page1: %s", p1)
	}
	kb := buildPagedKeyboard(searchCallbackPrefix, "sid", 1, 2, false)
	if kb == nil || len(kb.InlineKeyboard) != 1 || !strings.Contains(kb.InlineKeyboard[0][2].Text, "下一页") {
		t.Fatalf("search kb: %+v", kb)
	}
}
