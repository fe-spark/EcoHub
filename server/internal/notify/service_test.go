package notify

import (
	"strings"
	"testing"
	"time"

	"server/internal/model"
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
	if !strings.Contains(joined, "影片一") {
		t.Fatalf("missing film: %s", joined)
	}
	if strings.Contains(joined, "<script>") {
		t.Fatal("should escape html")
	}
	if !strings.Contains(joined, "timeout") {
		t.Fatal("missing error")
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
