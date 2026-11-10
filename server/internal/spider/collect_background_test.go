package spider

import (
	"testing"
	"time"
)

// 后台采集批次 panic 被兜住，且仍从 backgroundCollects 里摘掉。
func TestGoBackgroundCollect_RecoversPanic(t *testing.T) {
	goBackgroundCollect(func() { panic("boom") })
	if !WaitBackgroundCollects(2 * time.Second) {
		t.Fatal("panicking background collect must still be marked done")
	}
}
