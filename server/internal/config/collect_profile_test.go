package config

import (
	"os"
	"path/filepath"
	"runtime"
	"testing"
)

func TestScaleCollectProfile(t *testing.T) {
	cases := []struct {
		name     string
		cores    int
		memBytes int64
		want     collectScale
	}{
		{
			name:     "1核1GiB",
			cores:    1,
			memBytes: 1 << 30,
			want:     collectScale{inflight: 1, pagesPerSec: 8, burst: 2, pageWorkers: 4, fetch: 4, perSource: 32, global: 64},
		},
		{
			name:     "2核2GiB",
			cores:    2,
			memBytes: 2 << 30,
			want:     collectScale{inflight: 2, pagesPerSec: 16, burst: 4, pageWorkers: 4, fetch: 8, perSource: 32, global: 64},
		},
		{
			name:     "7核3654MiB",
			cores:    7,
			memBytes: 3654 << 20,
			want:     collectScale{inflight: 7, pagesPerSec: 56, burst: 14, pageWorkers: 7, fetch: 49, perSource: 56, global: 392},
		},
		{
			name:     "8核2GiB",
			cores:    8,
			memBytes: 2 << 30,
			want:     collectScale{inflight: 4, pagesPerSec: 32, burst: 8, pageWorkers: 4, fetch: 16, perSource: 32, global: 128},
		},
		{
			name:     "16核32GiB",
			cores:    16,
			memBytes: 32 << 30,
			want:     collectScale{inflight: 16, pagesPerSec: 128, burst: 32, pageWorkers: 16, fetch: 128, perSource: 128, global: 2048},
		},
		{
			name:  "读不到内存只按核数",
			cores: 3,
			want:  collectScale{inflight: 3, pagesPerSec: 24, burst: 6, pageWorkers: 4, fetch: 12, perSource: 32, global: 96},
		},
	}
	for _, tc := range cases {
		t.Run(tc.name, func(t *testing.T) {
			if got := scaleCollectProfile(tc.cores, tc.memBytes); got != tc.want {
				t.Fatalf("scale=%+v want %+v", got, tc.want)
			}
		})
	}
}

func TestResolveCollectProfile_RecomputesFromDevice(t *testing.T) {
	prevProcs := runtime.GOMAXPROCS(2)
	prev := collectProfileSnapshot()
	t.Cleanup(func() {
		runtime.GOMAXPROCS(prevProcs)
		restoreCollectProfile(prev)
	})
	silenceCPUQuota(t)
	useMemoryProbe(t, "MemTotal: 16777216 kB\n", "", "")

	runtime.GOMAXPROCS(2)
	resolveCollectProfile()
	if got := collectProfileSnapshot(); got.inflight != 2 || got.pagesPerSec != 16 {
		t.Fatalf("2 核 16GiB: %+v", got)
	}

	runtime.GOMAXPROCS(8)
	resolveCollectProfile()
	if got := collectProfileSnapshot(); got.inflight != 8 || got.pagesPerSec != 64 || got.fetch != 64 {
		t.Fatalf("再次按 8 核重算: %+v", got)
	}

	useMemoryProbe(t, "MemTotal: 8388608 kB\n", "2147483648\n", "")
	runtime.GOMAXPROCS(8)
	resolveCollectProfile()
	if got := collectProfileSnapshot(); got.inflight != 4 || got.global != 128 {
		t.Fatalf("cgroup 限额 2GiB 应压过核数: %+v", got)
	}

	useMemoryProbe(t, "", "", "")
	runtime.GOMAXPROCS(3)
	resolveCollectProfile()
	if got := collectProfileSnapshot(); got.inflight != 3 || got.pagesPerSec != 24 {
		t.Fatalf("读不到内存时只按核数: %+v", got)
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
	source      int
	pages       int
	fetch       int
	inflight    int
	pagesPerSec int
	burst       int
	perSrc      int
	global      int
}

func collectProfileSnapshot() collectProfileValues {
	return collectProfileValues{
		source:      CollectSourceConcurrency,
		pages:       CollectPageWorkers,
		fetch:       CollectFetchInFlight,
		inflight:    CollectWriteMaxInflight,
		pagesPerSec: CollectWritePagesPerSec,
		burst:       CollectWriteBurstPages,
		perSrc:      CollectWriteMaxPendingPagesPerSource,
		global:      CollectWriteMaxPendingPagesGlobal,
	}
}

func restoreCollectProfile(v collectProfileValues) {
	CollectSourceConcurrency = v.source
	CollectPageWorkers = v.pages
	CollectFetchInFlight = v.fetch
	CollectWriteMaxInflight = v.inflight
	CollectWritePagesPerSec = v.pagesPerSec
	CollectWriteBurstPages = v.burst
	CollectWriteMaxPendingPagesPerSource = v.perSrc
	CollectWriteMaxPendingPagesGlobal = v.global
}

func silenceCPUQuota(t *testing.T) {
	t.Helper()
	prevV2, prevQuota, prevPeriod := cgroupV2CPUPath, cgroupV1QuotaPath, cgroupV1PeriodPath
	missing := filepath.Join(t.TempDir(), "missing")
	cgroupV2CPUPath = missing
	cgroupV1QuotaPath = missing + "-quota"
	cgroupV1PeriodPath = missing + "-period"
	t.Cleanup(func() {
		cgroupV2CPUPath = prevV2
		cgroupV1QuotaPath = prevQuota
		cgroupV1PeriodPath = prevPeriod
	})
}

func useMemoryProbe(t *testing.T, meminfo, v2, v1 string) {
	t.Helper()
	dir := t.TempDir()
	write := func(name, body string) string {
		if body == "" {
			return filepath.Join(dir, name+"-missing")
		}
		path := filepath.Join(dir, name)
		if err := os.WriteFile(path, []byte(body), 0o644); err != nil {
			t.Fatal(err)
		}
		return path
	}
	prevInfo, prevV2, prevV1 := meminfoPath, cgroupV2MemoryPath, cgroupV1MemoryPath
	meminfoPath = write("meminfo", meminfo)
	cgroupV2MemoryPath = write("memory.max", v2)
	cgroupV1MemoryPath = write("memory.limit_in_bytes", v1)
	t.Cleanup(func() {
		meminfoPath = prevInfo
		cgroupV2MemoryPath = prevV2
		cgroupV1MemoryPath = prevV1
	})
}
