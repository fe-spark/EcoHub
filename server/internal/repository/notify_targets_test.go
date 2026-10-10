package repository

import (
	"testing"

	"server/internal/model"
)

func TestNormalizeChatIDs(t *testing.T) {
	in := []string{"  -100123  ", "-100123", "@channel", "", "  ", "-100123:456"}
	got := NormalizeChatIDs(in)
	expected := []string{"-100123", "@channel", "-100123:456"}
	if len(got) != len(expected) {
		t.Fatalf("expected len %d, got %d (%v)", len(expected), len(got), got)
	}
	for i := range expected {
		if got[i] != expected[i] {
			t.Errorf("expected[%d] = %q, got %q", i, expected[i], got[i])
		}
	}
}

func TestNormalizeNotifyConfig(t *testing.T) {
	cfg := model.NotifyConfig{
		ChatIDs:           []string{"  -100123:1  ", "-100123:1"},
		MaxFilmsInMessage: 100,
		MinIntervalSec:    -10,
	}
	out := normalizeNotifyConfig(cfg)
	if len(out.ChatIDs) != 1 || out.ChatIDs[0] != "-100123:1" {
		t.Fatalf("unexpected ChatIDs: %v", out.ChatIDs)
	}
	if out.MaxFilmsInMessage != model.MaxFilmsInMessageCap {
		t.Fatalf("MaxFilmsInMessage should cap at %d, got %d", model.MaxFilmsInMessageCap, out.MaxFilmsInMessage)
	}
	if out.MinIntervalSec != 0 {
		t.Fatalf("MinIntervalSec should clamp to 0, got %d", out.MinIntervalSec)
	}
}
