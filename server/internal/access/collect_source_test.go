package access

import (
	"context"
	"net/url"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestProvideCollectSource(t *testing.T) {
	prev := lookupActiveCollectSourceID
	lookupActiveCollectSourceID = func() string { return "primary01" }
	t.Cleanup(func() { lookupActiveCollectSourceID = prev })

	vod := "/api/provide/vod"
	if got := provideCollectSource("/api/provide/tvbox", url.Values{}); got != "" {
		t.Fatalf("config must not count, got %q", got)
	}
	if got := provideCollectSource(vod, url.Values{"source": {"  srcA  "}}); got != "srcA" {
		t.Fatalf("explicit source = %q", got)
	}
	if got := provideCollectSource(vod, url.Values{"source": {"bad id"}}); got != "" {
		t.Fatalf("invalid explicit source must not fall back, got %q", got)
	}
	longID := stringsRepeat("a", 33)
	if got := provideCollectSource(vod, url.Values{"source": {longID}}); got != "" {
		t.Fatalf("overlong source must not count, got %q", got)
	}
	if got := provideCollectSource(vod, url.Values{"ac": {"list"}}); got != "primary01" {
		t.Fatalf("list without source = %q", got)
	}
	if got := provideCollectSource(vod, url.Values{"ac": {"detail"}, "ids": {"9"}}); got != "" {
		t.Fatalf("detail without source = %q", got)
	}
	if got := provideCollectSource(vod, url.Values{"ac": {"videolist"}, "ids": {"9"}, "source": {"srcB"}}); got != "srcB" {
		t.Fatalf("detail with source = %q", got)
	}
	if got := provideCollectSource(vod, url.Values{"ac": {"detail"}}); got != "primary01" {
		t.Fatalf("detail without ids = %q", got)
	}
	lookupActiveCollectSourceID = func() string { return "" }
	if got := provideCollectSource(vod, url.Values{"ac": {"list"}}); got != "" {
		t.Fatalf("list without preferred = %q", got)
	}
}

func TestPageCollectSource(t *testing.T) {
	prevActive := lookupActiveCollectSourceID
	prevExists := lookupCollectSourceExists
	lookupActiveCollectSourceID = func() string { return "primary01" }
	lookupCollectSourceExists = func(id string) bool { return id == "primary01" || id == "live01" }
	t.Cleanup(func() {
		lookupActiveCollectSourceID = prevActive
		lookupCollectSourceExists = prevExists
	})

	if got := pageCollectSource(ActionBrowse, "web", ""); got != "primary01" {
		t.Fatalf("browse = %q", got)
	}
	if got := pageCollectSource(ActionSearch, "other", "庆余年"); got != "primary01" {
		t.Fatalf("search = %q", got)
	}
	if got := pageCollectSource(ActionPlay, "web", "1024"); got != "" {
		t.Fatalf("client type must not become a station, got %q", got)
	}
	if got := pageCollectSource(ActionPlay, "", "1024"); got != "" {
		t.Fatalf("play without source = %q", got)
	}
	if got := pageCollectSource(ActionPlay, "primary01", "1024"); got != "primary01" {
		t.Fatalf("play source = %q", got)
	}
	if got := pageCollectSource(ActionPlay, "missing", "1024"); got != "" {
		t.Fatalf("unknown play source = %q", got)
	}
	if got := pageCollectSource(ActionPlay, "", "live01:88"); got != "live01" {
		t.Fatalf("live resource = %q", got)
	}
	if got := pageCollectSource(ActionPlay, "", "gone:88"); got != "" {
		t.Fatalf("unknown live resource = %q", got)
	}
}

func TestQuerySourceCallsOrdersKnownThenUnknown(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)
	prevRdb, prevCxt := db.Rdb, db.Cxt
	db.Rdb = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Cxt = context.Background()
	t.Cleanup(func() {
		_ = db.Rdb.Close()
		db.Rdb = prevRdb
		db.Cxt = prevCxt
	})

	prevList := listCollectSources
	prevActive := lookupActiveCollectSourceID
	listCollectSources = func() []model.FilmSource {
		return []model.FilmSource{
			{Id: "srcA", Name: "甲", State: true},
			{Id: "srcB", Name: "乙", State: false},
		}
	}
	lookupActiveCollectSourceID = func() string { return "srcA" }
	t.Cleanup(func() {
		listCollectSources = prevList
		lookupActiveCollectSourceID = prevActive
	})

	day := time.Now().In(time.Local).Format("20060102")
	key := collectSourceKey(day)
	if err := db.Rdb.HSet(db.Cxt, key, "srcB", "2", "ghost", "5", "srcA", "1").Err(); err != nil {
		t.Fatalf("seed: %v", err)
	}

	rows, err := QuerySourceCalls("")
	if err != nil {
		t.Fatalf("query: %v", err)
	}
	if len(rows) != 3 {
		t.Fatalf("rows = %+v", rows)
	}
	if rows[0].Id != "srcA" || !rows[0].IsPrimary || rows[0].Count != 1 || !rows[0].Enabled {
		t.Fatalf("primary row = %+v", rows[0])
	}
	if rows[1].Id != "srcB" || rows[1].Enabled || rows[1].Count != 2 {
		t.Fatalf("disabled row = %+v", rows[1])
	}
	if rows[2].Id != "ghost" || rows[2].Name != "ghost" || rows[2].Count != 5 {
		t.Fatalf("unknown row = %+v", rows[2])
	}
}

func stringsRepeat(s string, n int) string {
	out := make([]byte, 0, len(s)*n)
	for i := 0; i < n; i++ {
		out = append(out, s...)
	}
	return string(out)
}
