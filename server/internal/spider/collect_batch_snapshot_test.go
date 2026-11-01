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
	batchA := newCollectBatchContext(model.CollectTriggerManual, "全量", []model.FilmSource{sourceA}, time.Now(), true)
	batchB := newCollectBatchContext(model.CollectTriggerCron, "定时", []model.FilmSource{sourceB}, time.Now(), false)
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

func TestCollectBatchContext_OnlyPrimarySourceTriggersWarmup(t *testing.T) {
	resetStreamPublishBufferForTest()
	origPub := publishStreamWindowFn
	origPref := preferredSourceIDFn
	publishStreamWindowFn = func(mids []int64) error { return nil }
	preferredSourceIDFn = func() string { return "primary" }
	t.Cleanup(func() {
		resetStreamPublishBufferForTest()
		publishStreamWindowFn = origPub
		preferredSourceIDFn = origPref
	})

	primarySrc := model.FilmSource{Id: "primary", Name: "Primary", Sort: 0}
	backupSrc := model.FilmSource{Id: "backup", Name: "Backup", Sort: 1}

	// 1. 非首选站单站完成：primaryWarmedUp 应保持 false
	backupBatch := newCollectBatchContext(model.CollectTriggerManual, "单站采集", []model.FilmSource{backupSrc}, time.Now(), true)
	defer backupBatch.close()
	backupBatch.markSourceFinished(backupSrc)
	if err := backupBatch.flushAndFinalize(); err != nil {
		t.Fatalf("backup flushAndFinalize: %v", err)
	}
	if backupBatch.primaryWarmedUp {
		t.Fatal("非首选站不应触发首页预热标记")
	}

	// 2. 批量采集中首选站先完成：finalizePrimarySource 立即置位 primaryWarmedUp，收尾 flushAndFinalize 不再重复预热
	batch := newCollectBatchContext(model.CollectTriggerManual, "Batch-Collect", []model.FilmSource{primarySrc, backupSrc}, time.Now(), false)
	defer batch.close()
	batch.markSourceFinished(primarySrc)
	if err := batch.finalizePrimarySource(primarySrc.Id); err != nil {
		t.Fatalf("finalizePrimarySource: %v", err)
	}
	if !batch.primaryWarmedUp {
		t.Fatal("首选站完成后应立即置位 primaryWarmedUp")
	}
	batch.markSourceFinished(backupSrc)
	if err := batch.flushAndFinalize(); err != nil {
		t.Fatalf("batch flushAndFinalize: %v", err)
	}
}

func TestCollectBatchContext_ReleaseSourceOccupyPerSource(t *testing.T) {
	s1 := model.FilmSource{Id: "occ-s1", Name: "S1", State: true}
	s2 := model.FilmSource{Id: "occ-s2", Name: "S2", State: true}
	releaseCollectSourceIDs(s1.Id, s2.Id)
	t.Cleanup(func() { releaseCollectSourceIDs(s1.Id, s2.Id) })

	occupied := occupyCollectSources([]model.FilmSource{s1, s2}, "Batch-1")
	if len(occupied) != 2 {
		t.Fatalf("expected 2 occupied sources, got %d", len(occupied))
	}
	batch1 := newCollectBatchContext(model.CollectTriggerManual, "Batch-1", occupied, time.Now(), false)
	defer batch1.close()

	// s1 完成并立即释放自身占用锁，而 s2 仍在运行
	batch1.releaseSourceOccupy(s1.Id)
	if isOccupiedCollectSource(s1.Id) {
		t.Fatal("s1 完成后应立即释放占用")
	}
	if !isOccupiedCollectSource(s2.Id) {
		t.Fatal("s2 仍在运行，应保持占用")
	}

	// 新批次尝试占用 s1, s2：应仅占到已完成的 s1，跳过仍在跑的 s2
	occupied2 := occupyCollectSources([]model.FilmSource{s1, s2}, "Batch-2")
	if len(occupied2) != 1 || occupied2[0].Id != s1.Id {
		t.Fatalf("新批次应仅占用已释放的 s1，got %+v", occupied2)
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
