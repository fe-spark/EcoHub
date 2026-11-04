package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestResolveCollectProfile(t *testing.T) {
	prevProcs := runtime.GOMAXPROCS(2)
	prev := collectProfileSnapshot()
	t.Cleanup(func() {
		runtime.GOMAXPROCS(prevProcs)
		restoreCollectProfile(prev)
	})

	restoreCollectProfile(highCollectProfile())
	runtime.GOMAXPROCS(3)
	resolveCollectProfile()
	if got := collectProfileSnapshot(); got != highCollectProfile() {
		t.Fatalf("3 核应保持全速档: %+v", got)
	}

	runtime.GOMAXPROCS(2)
	resolveCollectProfile()
	if got := collectProfileSnapshot(); got != smallCollectProfile() {
		t.Fatalf("2 核应使用小档: %+v", got)
	}

	runtime.GOMAXPROCS(8)
	resolveCollectProfile()
	if got := collectProfileSnapshot(); got != smallCollectProfile() {
		t.Fatalf("启动后不再改回全速档: %+v", got)
	}
}

func TestApplyContainerCPUQuota_CgroupV2(t *testing.T) {
	dir := t.TempDir()
	cgroupV2File := filepath.Join(dir, "cpu.max")
	if err := os.WriteFile(cgroupV2File, []byte("200000 100000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	prevV2 := cgroupV2CPUPath
	cgroupV2CPUPath = cgroupV2File
	prevProcs := runtime.GOMAXPROCS(8)
	t.Cleanup(func() {
		cgroupV2CPUPath = prevV2
		runtime.GOMAXPROCS(prevProcs)
	})

	applyContainerCPUQuota()
	if got := runtime.GOMAXPROCS(0); got != 2 {
		t.Fatalf("cgroup v2 限额 2 核，GOMAXPROCS 应为 2，实际为: %d", got)
	}
}

func TestApplyContainerCPUQuota_CgroupV1(t *testing.T) {
	dir := t.TempDir()
	quotaFile := filepath.Join(dir, "cpu.cfs_quota_us")
	periodFile := filepath.Join(dir, "cpu.cfs_period_us")
	if err := os.WriteFile(quotaFile, []byte("100000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(periodFile, []byte("100000\n"), 0644); err != nil {
		t.Fatal(err)
	}
	prevV2 := cgroupV2CPUPath
	prevQuota := cgroupV1QuotaPath
	prevPeriod := cgroupV1PeriodPath
	cgroupV2CPUPath = filepath.Join(dir, "non-existent")
	cgroupV1QuotaPath = quotaFile
	cgroupV1PeriodPath = periodFile
	prevProcs := runtime.GOMAXPROCS(8)
	t.Cleanup(func() {
		cgroupV2CPUPath = prevV2
		cgroupV1QuotaPath = prevQuota
		cgroupV1PeriodPath = prevPeriod
		runtime.GOMAXPROCS(prevProcs)
	})

	applyContainerCPUQuota()
	if got := runtime.GOMAXPROCS(0); got != 1 {
		t.Fatalf("cgroup v1 限额 1 核，GOMAXPROCS 应为 1，实际为: %d", got)
	}
}

type collectProfileValues struct {
	source int
	pages  int
	solo   int
	fetch  int
	write  int
	rate   int
	burst  int
	perSrc int
	global int
}

func highCollectProfile() collectProfileValues {
	return collectProfileValues{source: 0, pages: 16, solo: 32, fetch: 128, write: 16, rate: 240, burst: 80, perSrc: 500, global: 5000}
}

func smallCollectProfile() collectProfileValues {
	return collectProfileValues{source: 2, pages: 4, solo: 8, fetch: 8, write: 2, rate: 40, burst: 12, perSrc: 32, global: 64}
}

func collectProfileSnapshot() collectProfileValues {
	return collectProfileValues{
		source: CollectSourceConcurrency,
		pages:  CollectPageWorkers,
		solo:   CollectPageWorkersSolo,
		fetch:  CollectFetchInFlight,
		write:  CollectWriteMaxInflight,
		rate:   CollectWritePagesPerSec,
		burst:  CollectWriteBurstPages,
		perSrc: CollectWriteMaxPendingPagesPerSource,
		global: CollectWriteMaxPendingPagesGlobal,
	}
}

func restoreCollectProfile(v collectProfileValues) {
	CollectSourceConcurrency = v.source
	CollectPageWorkers = v.pages
	CollectPageWorkersSolo = v.solo
	CollectFetchInFlight = v.fetch
	CollectWriteMaxInflight = v.write
	CollectWritePagesPerSec = v.rate
	CollectWriteBurstPages = v.burst
	CollectWriteMaxPendingPagesPerSource = v.perSrc
	CollectWriteMaxPendingPagesGlobal = v.global
}
