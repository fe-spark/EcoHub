package config

import "testing"

func TestApplyWriteSlots(t *testing.T) {
	prev := collectProfileSnapshot()
	t.Cleanup(func() { restoreCollectProfile(prev) })

	ApplyWriteSlots(1)
	if got := collectProfileSnapshot(); got.inflight != CollectWriteSlotMin || profileExceptInflight(got) != profileExceptInflight(prev) {
		t.Fatalf("低于下限: %+v", got)
	}

	ApplyWriteSlots(8)
	if got := collectProfileSnapshot(); got.inflight != 8 || profileExceptInflight(got) != profileExceptInflight(prev) {
		t.Fatalf("8 路: %+v", got)
	}

	ApplyWriteSlots(WriteSlotMax())
	if got := collectProfileSnapshot(); got.inflight != WriteSlotMax() || profileExceptInflight(got) != profileExceptInflight(prev) {
		t.Fatalf("顶格: %+v", got)
	}

	ApplyWriteSlots(WriteSlotMax() + 100)
	if got := collectProfileSnapshot(); got.inflight != WriteSlotMax() || profileExceptInflight(got) != profileExceptInflight(prev) {
		t.Fatalf("超过连接池余量: %+v", got)
	}
}

func TestLoadCollectRuntimeConfig_StartsAtFullSpeed(t *testing.T) {
	prev := collectProfileSnapshot()
	t.Cleanup(func() { restoreCollectProfile(prev) })

	loadCollectRuntimeConfig()
	got := collectProfileSnapshot()
	if got.inflight != WriteSlotMax() {
		t.Fatalf("写槽=%d 上限=%d", got.inflight, WriteSlotMax())
	}
	if profileExceptInflight(got) != profileExceptInflight(prev) {
		t.Fatalf("页并发或队列被写槽改掉: before=%+v after=%+v", prev, got)
	}
}

func profileExceptInflight(v collectProfileValues) collectProfileValues {
	v.inflight = 0
	return v
}

type collectProfileValues struct {
	source   int
	pages    int
	fetch    int
	inflight int
	perSrc   int
	global   int
}

func collectProfileSnapshot() collectProfileValues {
	return collectProfileValues{
		source:   CollectSourceConcurrency,
		pages:    CollectPageWorkers,
		fetch:    CollectFetchInFlight,
		inflight: CollectWriteMaxInflight,
		perSrc:   CollectWriteMaxPendingPagesPerSource,
		global:   CollectWriteMaxPendingPagesGlobal,
	}
}

func restoreCollectProfile(v collectProfileValues) {
	CollectSourceConcurrency = v.source
	CollectPageWorkers = v.pages
	CollectFetchInFlight = v.fetch
	CollectWriteMaxInflight = v.inflight
	CollectWriteMaxPendingPagesPerSource = v.perSrc
	CollectWriteMaxPendingPagesGlobal = v.global
}
