package config

import "testing"

func TestApplyWriteSlots(t *testing.T) {
	prev := collectProfileSnapshot()
	t.Cleanup(func() { restoreCollectProfile(prev) })

	ApplyWriteSlots(1)
	if got := collectProfileSnapshot(); got.inflight != 2 || got.pages != 2 || got.fetch != 4 || got.perSrc != 32 || got.global != 64 {
		t.Fatalf("低于下限: %+v", got)
	}

	ApplyWriteSlots(8)
	if got := collectProfileSnapshot(); got.inflight != 8 || got.pages != 8 || got.fetch != 64 || got.perSrc != 128 || got.global != 1024 {
		t.Fatalf("8 路: %+v", got)
	}

	ApplyWriteSlots(16)
	if got := collectProfileSnapshot(); got.inflight != 16 || got.pages != 16 || got.fetch != 128 || got.perSrc != 256 || got.global != 4096 {
		t.Fatalf("顶格: %+v", got)
	}

	ApplyWriteSlots(32)
	if got := collectProfileSnapshot(); got.inflight != CollectWriteSlotMax || got.fetch != 128 {
		t.Fatalf("超过硬顶: %+v", got)
	}
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
