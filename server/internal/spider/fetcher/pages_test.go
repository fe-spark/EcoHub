package fetcher

import "testing"

func TestShouldWrapUpAfterFetchAbort(t *testing.T) {
	if !shouldWrapUpAfterFetchAbort(10, false) {
		t.Fatal("有成功且允许发布应进入收尾")
	}
	if shouldWrapUpAfterFetchAbort(0, false) {
		t.Fatal("0 成功不应收尾，应直接失败")
	}
	if shouldWrapUpAfterFetchAbort(10, true) {
		t.Fatal("主站全量跳过发布时不应走收尾发布")
	}
}
