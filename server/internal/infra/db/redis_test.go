package db

import (
	"testing"

	"server/internal/config"

	"github.com/alicebob/miniredis/v2"
)

// 已建好的客户端不会被替换：业务协程无锁读取 Rdb，运行期替换会产生数据竞争。
func TestInitRedisConn_DoesNotReplaceExistingClient(t *testing.T) {
	mr := miniredis.RunT(t)
	prevAddr, prevRdb := config.RedisAddr, Rdb
	config.RedisAddr = mr.Addr()
	Rdb = nil
	t.Cleanup(func() {
		if Rdb != nil && Rdb != prevRdb {
			_ = Rdb.Close()
		}
		config.RedisAddr, Rdb = prevAddr, prevRdb
	})

	if err := InitRedisConn(); err != nil {
		t.Fatal(err)
	}
	first := Rdb
	if first == nil {
		t.Fatal("client not created")
	}
	if err := InitRedisConn(); err != nil {
		t.Fatal(err)
	}
	if Rdb != first {
		t.Fatal("second init must keep the existing client")
	}

	// Redis 挂掉时只报错，不换客户端；恢复后同一个客户端自动重连。
	mr.Close()
	if err := InitRedisConn(); err == nil {
		t.Fatal("expected ping error while redis is down")
	}
	if Rdb != first {
		t.Fatal("client must not be replaced on failure")
	}
	if err := mr.Restart(); err != nil {
		t.Fatal(err)
	}
	if err := Rdb.Ping(Cxt).Err(); err != nil {
		t.Fatalf("pool should reconnect by itself: %v", err)
	}
}
