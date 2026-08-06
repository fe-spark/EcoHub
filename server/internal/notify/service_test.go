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
		MaxFilmsInMessage: 30,
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
		MaxFilmsInMessage: 30,
	})
	if err == nil {
		t.Fatal("expected invalid chat id error")
	}
	cfg, err := ValidateAndMergeUpdate(old, model.NotifyConfig{
		Enabled:           false,
		ChatIDs:           []string{"-100123", "@mychannel"},
		MaxFilmsInMessage: 30,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ChatIDs) != 2 {
		t.Fatalf("got %v", cfg.ChatIDs)
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

func TestMidAccumulatorBound(t *testing.T) {
	acc := NewMidAccumulator()
	mids := make([]int64, 0, maxMidsPerSource+10)
	for i := int64(1); i <= int64(maxMidsPerSource)+10; i++ {
		mids = append(mids, i)
	}
	acc.Add("src1", mids...)
	got, total, truncated := acc.DrainSource("src1")
	if !truncated {
		t.Fatal("expected truncated")
	}
	if total != maxMidsPerSource+10 {
		t.Fatalf("total=%d", total)
	}
	if len(got) != maxMidsPerSource {
		t.Fatalf("len=%d", len(got))
	}
}

func TestFormatBatchSummary(t *testing.T) {
	payload := model.CollectBatchNotifyPayload{
		Trigger:            model.NotifyTriggerManual,
		SiteName:           "测试站",
		DurationSec:        90,
		SuccessSources:     1,
		FailedSources:      1,
		TotalFilms:         2,
		IncludeFilmDetails: true,
		Sources: []model.SourceNotifyResult{
			{
				SourceName:  "主站A",
				Grade:       0,
				Status:      "done",
				PageTotal:   2,
				PageCurrent: 2,
				SuccessCnt:  2,
				FilmsTotal:  2,
				Films: []model.FilmNotifyItem{
					{Mid: 1, Name: "影片一"},
					{Mid: 2, Name: "影片二"},
				},
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
	msgs := formatBatchSummary(payload, 30)
	if len(msgs) == 0 {
		t.Fatal("empty messages")
	}
	joined := strings.Join(msgs, "\n")
	if !strings.Contains(joined, "测试站") {
		t.Fatalf("missing site name: %s", joined)
	}
	// 总览不含具体片名（片名在带按钮的列表消息里）
	if strings.Contains(joined, "影片一") {
		t.Fatalf("overview should not embed film lines: %s", joined)
	}
	if !strings.Contains(joined, "上一页") && !strings.Contains(joined, "影片明细") {
		// 至少提示有明细
		if !strings.Contains(joined, "明细") {
			t.Fatalf("expected film list hint: %s", joined)
		}
	}
	if strings.Contains(joined, "<script>") {
		t.Fatal("should escape html")
	}
	if !strings.Contains(joined, "timeout") {
		t.Fatal("missing error")
	}
}

func TestFormatFilmListPageAndKeyboard(t *testing.T) {
	prev := sitePlayBaseURLFn
	sitePlayBaseURLFn = func() string { return "" }
	t.Cleanup(func() { sitePlayBaseURLFn = prev })

	items := make([]FilmPageItem, 0, 75)
	for i := 1; i <= 75; i++ {
		items = append(items, FilmPageItem{
			SourceName: "主站",
			Grade:      0,
			Mid:        int64(i),
			Name:       fmt.Sprintf("片%d", i),
		})
	}
	sess := FilmPageSession{SiteName: "分页站", PageSize: 30, TotalCount: 75, Items: items}
	if sess.totalPages() != 3 {
		t.Fatalf("totalPages=%d", sess.totalPages())
	}
	p1 := formatFilmListPage(sess, 1)
	if !strings.Contains(p1, "第 1/3 页") || !strings.Contains(p1, "片1") {
		t.Fatalf("page1: %s", p1)
	}
	p3 := formatFilmListPage(sess, 3)
	if !strings.Contains(p3, "第 3/3 页") || !strings.Contains(p3, "片75") {
		t.Fatalf("page3: %s", p3)
	}
	kb := buildPageKeyboard("abc123", 2, 3)
	if kb == nil || len(kb.InlineKeyboard) != 1 || len(kb.InlineKeyboard[0]) != 3 {
		t.Fatalf("keyboard: %+v", kb)
	}
	// 中间页应有上一页、下一页
	if !strings.Contains(kb.InlineKeyboard[0][0].Text, "上一页") {
		t.Fatalf("prev btn: %s", kb.InlineKeyboard[0][0].Text)
	}
	if !strings.Contains(kb.InlineKeyboard[0][2].Text, "下一页") {
		t.Fatalf("next btn: %s", kb.InlineKeyboard[0][2].Text)
	}
	sid, page, kind, ok := parsePageCallback("nfp:abc123:2")
	if !ok || sid != "abc123" || page != 2 || kind != "page" {
		t.Fatalf("parse page: %v %v %v %v", sid, page, kind, ok)
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
}

func TestFormatProgressStale(t *testing.T) {
	msgs := formatProgressStale("站", "源A", "sid", "age=1m", time.Now())
	joined := strings.Join(msgs, "\n")
	if !strings.Contains(joined, "采集进度超时") {
		t.Fatalf("expected stale title: %s", joined)
	}
}

func TestBuildBatchPayloadCountsStopped(t *testing.T) {
	payload := BuildBatchPayload(model.NotifyTriggerManual, []model.SourceNotifyResult{
		{Status: "done"},
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
