package access

import (
	"strings"
	"testing"
	"time"

	"server/internal/config"
)

func TestRecentKeysForModule(t *testing.T) {
	day := "20260904"
	web := recentKeysForModule("web", day)
	if len(web) != 1 || !strings.Contains(web[0], "web:recent:20260904") {
		t.Fatalf("web keys %+v", web)
	}
	app := recentKeysForModule("app", day)
	if len(app) != 1 || !strings.Contains(app[0], "app:recent:20260904") {
		t.Fatalf("app keys %+v", app)
	}
	tv := recentKeysForModule("tvbox", day)
	if len(tv) != 1 || !strings.Contains(tv[0], "tvbox:recent:20260904") {
		t.Fatalf("tvbox keys %+v", tv)
	}
	all := recentKeysForModule("", day)
	if len(all) != 3 {
		t.Fatalf("empty module should merge 3 keys, got %+v", all)
	}
}

func TestMatchLogSource(t *testing.T) {
	orig := config.AccessSlowMs
	config.AccessSlowMs = 500
	t.Cleanup(func() { config.AccessSlowMs = orig })

	slow := AccessEvent{LatencyMs: 800, Status: 200}
	fast := AccessEvent{LatencyMs: 20, Status: 200}
	err4 := AccessEvent{LatencyMs: 20, Status: 404}
	ok := AccessEvent{LatencyMs: 20, Status: 200}

	if !matchLogSource("slow", slow) || matchLogSource("slow", fast) {
		t.Fatal("slow source should filter by latency")
	}
	if !matchLogSource("error", err4) || matchLogSource("error", ok) {
		t.Fatal("error source should filter status>=400")
	}
	if !matchLogSource("recent", fast) || !matchLogSource("", err4) {
		t.Fatal("recent/empty source should keep all")
	}
}

func TestMergeTopItemsByCount(t *testing.T) {
	got := mergeTopItemsByCount(
		[]TopItem{{Key: "/a", Count: 3}, {Key: "/b", Count: 1}},
		[]TopItem{{Key: "/a", Count: 2}, {Key: "/c", Count: 10}},
	)
	if len(got) != 3 || got[0].Key != "/c" || got[1].Key != "/a" || got[1].Count != 5 {
		t.Fatalf("merge %+v", got)
	}
}

func TestParseRecentLogEventsSkipsBadJSON(t *testing.T) {
	now := time.Now()
	raw := []string{
		`{"path":"/a","status":200,"ts":"` + now.Format(time.RFC3339Nano) + `"}`,
		`not-json`,
		`{"path":"/b","status":500}`,
	}
	got := parseRecentLogEvents(raw)
	if len(got) != 2 {
		t.Fatalf("want 2 events, got %+v", got)
	}
}
