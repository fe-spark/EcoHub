package repository

import (
	"context"
	"errors"
	"testing"

	"server/internal/infra/db"

	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
)

type redisCmdHook struct {
	fail map[string]bool
}

func (h redisCmdHook) DialHook(next redis.DialHook) redis.DialHook { return next }

func (h redisCmdHook) ProcessHook(next redis.ProcessHook) redis.ProcessHook {
	return func(ctx context.Context, cmd redis.Cmder) error {
		if h.fail[cmd.Name()] {
			return errors.New(cmd.Name() + " failed")
		}
		return next(ctx, cmd)
	}
}

func (h redisCmdHook) ProcessPipelineHook(next redis.ProcessPipelineHook) redis.ProcessPipelineHook {
	return next
}

func useCacheRedis(t *testing.T) *miniredis.Miniredis {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	prev := db.Rdb
	db.Rdb = rdb
	t.Cleanup(func() {
		db.Rdb = prev
		_ = rdb.Close()
		mr.Close()
	})
	return mr
}

func TestWriteCache_SetFailureDropsStaleKey(t *testing.T) {
	mr := useCacheRedis(t)
	if err := db.Rdb.Set(db.Cxt, "site", "old", 0).Err(); err != nil {
		t.Fatal(err)
	}
	db.Rdb.AddHook(redisCmdHook{fail: map[string]bool{"set": true}})

	if err := writeCache("site", "new"); err != nil {
		t.Fatalf("删除旧键成功时不应返回错误: %v", err)
	}
	if mr.Exists("site") {
		t.Fatal("写入失败后旧键仍在")
	}
}

func TestWriteCache_ReturnsErrorWhenDropAlsoFails(t *testing.T) {
	mr := useCacheRedis(t)
	if err := db.Rdb.Set(db.Cxt, "site", "old", 0).Err(); err != nil {
		t.Fatal(err)
	}
	db.Rdb.AddHook(redisCmdHook{fail: map[string]bool{"set": true, "del": true}})

	if err := writeCache("site", "new"); err == nil {
		t.Fatal("删除旧键失败时应返回错误")
	}
	if got, _ := mr.Get("site"); got != "old" {
		t.Fatalf("删除失败时旧值被改掉: %q", got)
	}
}

func TestFillCache_SetFailureKeepsReadResult(t *testing.T) {
	mr := useCacheRedis(t)
	db.Rdb.AddHook(redisCmdHook{fail: map[string]bool{"set": true}})

	if err := fillCache("site", "from-db"); err != nil {
		t.Fatalf("回填失败不应挡住读取: %v", err)
	}
	if mr.Exists("site") {
		t.Fatal("回填失败不应写成新值")
	}
}
