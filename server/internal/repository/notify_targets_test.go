package repository

import (
	"testing"

	"server/internal/model"
)

func TestRebuildTargetsFromChatIDs_DropsStaleAndKeepsMeta(t *testing.T) {
	sources := []model.NotifyTarget{
		{
			ID:       "old-a",
			Name:     "群A",
			ChatID:   "A",
			ThreadID: "99",
			Enabled:  true,
			MinLevel: model.SeverityWarn,
		},
		{
			ID:      "old-b",
			Name:    "群B",
			ChatID:  "B",
			Enabled: true,
		},
	}
	// 用户把成员从 A,B 改成 C,A：应丢弃 B，保留 A 的 thread/等级，并新建 C
	got := RebuildTargetsFromChatIDs([]string{"C", "A"}, sources)
	if len(got) != 2 {
		t.Fatalf("want 2 targets, got %+v", got)
	}
	if got[0].ChatID != "C" || got[0].ThreadID != "" || !got[0].Enabled {
		t.Fatalf("new C default: %+v", got[0])
	}
	if got[1].ChatID != "A" || got[1].ThreadID != "99" || got[1].MinLevel != model.SeverityWarn {
		t.Fatalf("preserve A meta: %+v", got[1])
	}
	if got[1].Name != "群A" {
		t.Fatalf("preserve A name: %+v", got[1])
	}
}

func TestRebuildTargetsFromChatIDs_EmptyMembership(t *testing.T) {
	got := RebuildTargetsFromChatIDs(nil, []model.NotifyTarget{{ChatID: "A"}})
	if len(got) != 0 {
		t.Fatalf("empty chatIDs should clear targets, got %+v", got)
	}
}

func TestRebuildTargetsFromChatIDs_MultiThreadSameChat(t *testing.T) {
	sources := []model.NotifyTarget{
		{ChatID: "A", ThreadID: "2", Enabled: true, Name: "t2"},
		{ChatID: "A", ThreadID: "", Enabled: true, Name: "main"},
		{ChatID: "A", ThreadID: "1", Enabled: true, Name: "t1"},
	}
	got := RebuildTargetsFromChatIDs([]string{"A"}, sources)
	if len(got) != 3 {
		t.Fatalf("want 3 thread targets, got %+v", got)
	}
	if got[0].ThreadID != "" || got[0].Name != "main" {
		t.Fatalf("empty thread first: %+v", got[0])
	}
	if got[1].ThreadID != "1" || got[2].ThreadID != "2" {
		t.Fatalf("threads sorted: %+v", got)
	}
}

func TestNormalizeNotifyConfig_SyncsDrift(t *testing.T) {
	cfg := model.NotifyConfig{
		ChatIDs: []string{"C"},
		Targets: []model.NotifyTarget{
			{ChatID: "A", Enabled: true, Name: "stale"},
		},
		MaxFilmsInMessage: 15,
		MinIntervalSec:    60,
	}
	out := normalizeNotifyConfig(cfg)
	if len(out.ChatIDs) != 1 || out.ChatIDs[0] != "C" {
		t.Fatalf("chatIDs: %v", out.ChatIDs)
	}
	if len(out.Targets) != 1 || out.Targets[0].ChatID != "C" {
		t.Fatalf("targets should follow chatIDs, got %+v", out.Targets)
	}
}

func TestNormalizeNotifyConfig_DeriveChatIDsFromTargets(t *testing.T) {
	cfg := model.NotifyConfig{
		ChatIDs: nil,
		Targets: []model.NotifyTarget{
			{ChatID: "X", Enabled: true, ThreadID: "7"},
		},
		MaxFilmsInMessage: 15,
	}
	out := normalizeNotifyConfig(cfg)
	if len(out.ChatIDs) != 1 || out.ChatIDs[0] != "X" {
		t.Fatalf("derived chatIDs: %v", out.ChatIDs)
	}
	if len(out.Targets) != 1 || out.Targets[0].ThreadID != "7" {
		t.Fatalf("targets: %+v", out.Targets)
	}
}
