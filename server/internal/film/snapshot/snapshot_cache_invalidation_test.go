package snapshot

import (
	"fmt"
	"testing"
	"time"

	"server/internal/config"
	"server/internal/infra/db"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

func TestFireDebouncedSnapshotCacheInvalidation_ClearsIndexAndClassify(t *testing.T) {
	client := mountSnapshotRedis(t)

	indexKey := config.IndexPageCacheKey + ":v1:src_a:s1:r1:ck4"
	classifyKey := fmt.Sprintf("%s:v1:P10:C1:S24:src_a:st3", config.FilmClassifyCacheKey)
	listKey := fmt.Sprintf("%s:v1:src_a:pid:10:14:0:ck5", config.FilmCategoryCachePrefix)
	searchKey := fmt.Sprintf("%s:v1:sv1:仙逆::p1:s12", config.FilmSearchCachePrefix)
	tokenKey := fmt.Sprintf(config.UserTokenKey, 7)
	for _, key := range []string{indexKey, classifyKey, listKey, searchKey, tokenKey} {
		if err := client.Set(db.Cxt, key, "old", 0).Err(); err != nil {
			t.Fatalf("预置 %s 失败: %v", key, err)
		}
	}

	fireDebouncedSnapshotCacheInvalidation()

	for _, key := range []string{indexKey, classifyKey, listKey} {
		if client.Exists(db.Cxt, key).Val() != 0 {
			t.Errorf("5 秒失效应删除 %s", key)
		}
	}
	if client.Exists(db.Cxt, searchKey).Val() != 1 {
		t.Errorf("搜索结果应留给版本号失效，不应被 5 秒删除扫掉")
	}
	if client.Exists(db.Cxt, tokenKey).Val() != 1 {
		t.Errorf("用户令牌应保留")
	}
}

func TestClearDerivedFilmListCaches_KeepsSearchAndToken(t *testing.T) {
	client := mountSnapshotRedis(t)

	indexKey := config.IndexPageCacheKey + ":v9:hot"
	classifyKey := config.FilmClassifyCacheKey + ":v9:P1:C1:S12"
	hotKey := config.FilmHotPoolCachePrefix + ":v9:src_a:pid:1:50:ck4"
	searchKey := config.FilmSearchCachePrefix + ":v9:sv1:测试::p1:s10"
	tokenKey := fmt.Sprintf(config.UserTokenKey, 8)
	for _, key := range []string{indexKey, classifyKey, hotKey, searchKey, tokenKey} {
		if err := client.Set(db.Cxt, key, "old", 0).Err(); err != nil {
			t.Fatalf("预置 %s 失败: %v", key, err)
		}
	}

	ClearDerivedFilmListCaches()

	for _, key := range []string{indexKey, classifyKey, hotKey} {
		if client.Exists(db.Cxt, key).Val() != 0 {
			t.Errorf("列表失效应删除 %s", key)
		}
	}
	if client.Exists(db.Cxt, searchKey).Val() != 1 || client.Exists(db.Cxt, tokenKey).Val() != 1 {
		t.Errorf("搜索结果和用户令牌应保留")
	}
}

func TestStoreCacheIfCurrent_DropsStaleWrite(t *testing.T) {
	client := mountSnapshotRedis(t)

	key := config.IndexPageCacheKey + ":stale-check"
	staleGen := GetSearchCacheVersion()
	BumpSearchCacheVersion()
	StoreCacheIfCurrent(key, `{"hot":"old"}`, time.Hour, staleGen)
	if client.Exists(db.Cxt, key).Val() != 0 {
		t.Fatal("世代已变时不得写回首页缓存")
	}

	fresh := GetSearchCacheVersion()
	StoreCacheIfCurrent(key, `{"hot":"new"}`, time.Hour, fresh)
	if client.Get(db.Cxt, key).Val() != `{"hot":"new"}` {
		t.Fatal("当前世代应能写入首页缓存")
	}
}

func mountSnapshotRedis(t *testing.T) *redis.Client {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	t.Cleanup(mr.Close)

	orig := db.Rdb
	client := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = client
	t.Cleanup(func() {
		_ = client.Close()
		db.Rdb = orig
		ResetSearchCacheVersionForTest()
	})
	ResetSearchCacheVersionForTest()
	return client
}
