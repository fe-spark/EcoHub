// Package testutil 测试辅助函数，只被 _test.go 引用。
package testutil

import (
	"fmt"
	"strings"
	"sync/atomic"
	"testing"
	"time"
)

var memDBSeq atomic.Uint64

// MemDSN 返回本次测试独享的内存 sqlite DSN。
// 使用 cache=shared，同一测试内的多个连接看到同一个库；库名带进程内序号和时间戳，
// 因此 -count>1、重跑或同名子测试不会复用上一次留下的数据。extra 为额外的查询参数，如 "_busy_timeout=5000"。
func MemDSN(t testing.TB, extra ...string) string {
	t.Helper()
	name := strings.NewReplacer("/", "_", " ", "_", "#", "_").Replace(t.Name())
	dsn := fmt.Sprintf("file:%s_%d_%d?mode=memory&cache=shared", name, time.Now().UnixNano(), memDBSeq.Add(1))
	for _, p := range extra {
		if p = strings.TrimSpace(p); p != "" {
			dsn += "&" + p
		}
	}
	return dsn
}
