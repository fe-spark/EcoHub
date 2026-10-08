package spider

import (
	"reflect"
	"sort"
	"testing"
	"time"

	"server/internal/model"
)

func TestStreamPublishBuffer_PreferredFlushesByCount(t *testing.T) {
	buf := newStreamPublishBuffer()
	mids := make([]int64, 0, streamPreferredCount)
	for i := 1; i <= streamPreferredCount; i++ {
		mids = append(mids, int64(i))
	}
	buf.add(true, mids)
	got, flags := buf.take(time.Now(), false)
	if len(got) != streamPreferredCount {
		t.Fatalf("首选站满 %d 应刷窗, got %d", streamPreferredCount, len(got))
	}
	if !flags[1] || buf.pendingCount() != 0 {
		t.Fatalf("刷窗后缓冲应清空, pending=%d", buf.pendingCount())
	}
}

func TestStreamPublishBuffer_BackupWaitsForLargerWindow(t *testing.T) {
	buf := newStreamPublishBuffer()
	buf.add(false, seqMIDs(1, 300))
	got, _ := buf.take(time.Now(), false)
	if len(got) != 0 {
		t.Fatalf("备用站 300 条未到阈值不应刷, got %d", len(got))
	}
	buf.add(false, seqMIDs(301, 1000))
	got, flags := buf.take(time.Now(), false)
	if len(got) != streamWindowMax {
		t.Fatalf("备用站满 1000 应刷 1000, got %d", len(got))
	}
	if flags[1] {
		t.Fatal("备用站 mid 不应记为首选")
	}
}

func TestStreamPublishBuffer_PreferredTimeWindow(t *testing.T) {
	buf := newStreamPublishBuffer()
	buf.lastPref = time.Now().Add(-streamPreferredWait - time.Second)
	buf.add(true, []int64{11, 12})
	got, _ := buf.take(time.Now(), false)
	if !reflect.DeepEqual(got, []int64{11, 12}) {
		t.Fatalf("首选站超时应刷出挂起 mid, got %v", got)
	}
}

func TestStreamPublishBuffer_ForceAndPutBack(t *testing.T) {
	buf := newStreamPublishBuffer()
	buf.add(true, []int64{1, 2})
	buf.add(false, []int64{3})
	got, flags := buf.take(time.Now(), true)
	if !reflect.DeepEqual(got, []int64{1, 2, 3}) {
		t.Fatalf("force 应收干, got %v", got)
	}
	buf.putBack(flags)
	if buf.pendingCount() != 3 {
		t.Fatalf("putBack 应恢复 3 条, got %d", buf.pendingCount())
	}
}

func TestStreamPublishBuffer_BackpressureForcesWindow(t *testing.T) {
	buf := newStreamPublishBuffer()
	buf.add(false, seqMIDs(1, streamBackpressureN))
	got, _ := buf.take(time.Now(), false)
	if len(got) != streamWindowMax {
		t.Fatalf("背压应按窗刷 %d, got %d", streamWindowMax, len(got))
	}
}

func TestCollectBatchContext_StreamEnqueueKeepsTagMIDsIsolated(t *testing.T) {
	resetStreamPublishBufferForTest()
	origPub := publishStreamWindowFn
	origPref := preferredSourceIDFn
	publishStreamWindowFn = func(mids []int64) error { return nil }
	preferredSourceIDFn = func() string { return "source-a" }
	t.Cleanup(func() {
		resetStreamPublishBufferForTest()
		publishStreamWindowFn = origPub
		preferredSourceIDFn = origPref
	})

	sourceA := model.FilmSource{Id: "source-a", Name: "Source A", Sort: 0}
	sourceB := model.FilmSource{Id: "source-b", Name: "Source B", Sort: 1}
	batchA := newCollectBatchContext(model.NotifyTriggerManual, "全量", []model.FilmSource{sourceA}, nil, time.Now(), true)
	batchB := newCollectBatchContext(model.NotifyTriggerCron, "定时", []model.FilmSource{sourceB}, nil, time.Now(), false)
	defer unregisterActiveBatch(batchA)
	defer unregisterActiveBatch(batchB)

	batchA.addAffectedMIDs(&sourceA, -1, []int64{100, 200})
	batchB.addAffectedMIDs(&sourceB, 24, []int64{300, 400})

	if got := sortedMIDSet(batchA.masterAffectedMIDs); !reflect.DeepEqual(got, []int64{100, 200}) {
		t.Fatalf("batch A 标签集 = %v; want [100 200]", got)
	}
	if got := sortedMIDSet(batchB.masterAffectedMIDs); !reflect.DeepEqual(got, []int64{300, 400}) {
		t.Fatalf("batch B 标签集 = %v; want [300 400]", got)
	}
}

func TestEnqueueStreamPublish_PreferredCountFlushes(t *testing.T) {
	resetStreamPublishBufferForTest()
	var published []int64
	origPub := publishStreamWindowFn
	origPref := preferredSourceIDFn
	publishStreamWindowFn = func(mids []int64) error {
		published = append(published, mids...)
		return nil
	}
	preferredSourceIDFn = func() string { return "pref" }
	t.Cleanup(func() {
		resetStreamPublishBufferForTest()
		publishStreamWindowFn = origPub
		preferredSourceIDFn = origPref
	})

	if err := enqueueStreamPublish("pref", seqMIDs(1, streamPreferredCount)); err != nil {
		t.Fatalf("enqueue: %v", err)
	}
	if len(published) != streamPreferredCount {
		t.Fatalf("首选站满窗应立即发布 %d, got %d", streamPreferredCount, len(published))
	}
	if streamBuf.pendingCount() != 0 {
		t.Fatalf("发布后缓冲应空, pending=%d", streamBuf.pendingCount())
	}
}

func seqMIDs(from, to int64) []int64 {
	out := make([]int64, 0, to-from+1)
	for i := from; i <= to; i++ {
		out = append(out, i)
	}
	return out
}

func sortedMIDSet(set map[int64]struct{}) []int64 {
	if len(set) == 0 {
		return nil
	}
	out := make([]int64, 0, len(set))
	for mid := range set {
		out = append(out, mid)
	}
	sort.Slice(out, func(i, j int) bool { return out[i] < out[j] })
	return out
}
