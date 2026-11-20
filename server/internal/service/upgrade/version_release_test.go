package upgrade

import (
	"errors"
	"testing"

	"server/internal/config"
	"server/internal/infra/db"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestPollRelease_FailurePreservesExistingAndRedisCache(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	prevRdb := db.Rdb
	prevFetch := fetchReleaseFn
	db.Rdb = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	t.Cleanup(func() {
		fetchReleaseFn = prevFetch
		_ = db.Rdb.Close()
		db.Rdb = prevRdb
		mr.Close()
		releaseMu.Lock()
		releaseStable = githubReleaseCache{}
		releasePre = githubReleaseCache{}
		releaseMu.Unlock()
	})

	fetchReleaseFn = func(includePre bool) (githubReleaseCache, error) {
		return githubReleaseCache{TagName: "v3.1.0", HTMLURL: "https://example.com/v3.1.0"}, nil
	}
	if !pollRelease(false) {
		t.Fatal("expected initial pollRelease to succeed")
	}

	// 单次拉取失败不得把已经缓存好的最新版本清空
	fetchReleaseFn = func(includePre bool) (githubReleaseCache, error) {
		return githubReleaseCache{}, errors.New("rate limited")
	}
	if pollRelease(false) {
		t.Fatal("expected failed pollRelease to return false")
	}
	got, ok := currentRelease(false)
	if !ok || got.TagName != "v3.1.0" {
		t.Fatalf("expected existing cached release v3.1.0 preserved, got %+v ok=%v", got, ok)
	}

	// 清空进程内存后应能从 Redis 恢复已缓存版本
	releaseMu.Lock()
	releaseStable = githubReleaseCache{}
	releaseMu.Unlock()
	if !mr.Exists(config.LatestReleaseCacheKey) {
		t.Fatal("expected release cached in Redis")
	}
	gotFromRedis, ok := currentRelease(false)
	if !ok || gotFromRedis.TagName != "v3.1.0" {
		t.Fatalf("expected Redis warmup v3.1.0, got %+v ok=%v", gotFromRedis, ok)
	}
}
