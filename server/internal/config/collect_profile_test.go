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
	useMemoryProbe(t, "MemTotal: 8388608 kB\n", "", "")

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

func TestResolveCollectProfile_SmallMemory(t *testing.T) {
	prevProcs := runtime.GOMAXPROCS(8)
	prev := collectProfileSnapshot()
	t.Cleanup(func() {
		runtime.GOMAXPROCS(prevProcs)
		restoreCollectProfile(prev)
	})

	useMemoryProbe(t, "MemTotal: 2097152 kB\n", "", "")
	restoreCollectProfile(highCollectProfile())
	resolveCollectProfile()
	if got := collectProfileSnapshot(); got != smallCollectProfile() {
		t.Fatalf("2GiB 应使用小档: %+v", got)
	}

	useMemoryProbe(t, "MemTotal: 2097153 kB\n", "", "")
	restoreCollectProfile(highCollectProfile())
	resolveCollectProfile()
	if got := collectProfileSnapshot(); got != highCollectProfile() {
		t.Fatalf("超过 2GiB 应保持全速档: %+v", got)
	}

	useMemoryProbe(t, "MemTotal: 8388608 kB\n", "2147483648\n", "")
	restoreCollectProfile(highCollectProfile())
	resolveCollectProfile()
	if got := collectProfileSnapshot(); got != smallCollectProfile() {
		t.Fatalf("cgroup 限额 2GiB 应使用小档: %+v", got)
	}

	useMemoryProbe(t, "MemTotal: 8388608 kB\n", "max\n", "")
	restoreCollectProfile(highCollectProfile())
	resolveCollectProfile()
	if got := collectProfileSnapshot(); got != highCollectProfile() {
		t.Fatalf("cgroup 无限制且内存更大应保持全速档: %+v", got)
	}

	useMemoryProbe(t, "MemTotal: 2097152 kB\n", "max\n", "9223372036854771712\n")
	restoreCollectProfile(highCollectProfile())
	resolveCollectProfile()
	if got := collectProfileSnapshot(); got != smallCollectProfile() {
		t.Fatalf("cgroup 无限制时应回退 MemTotal: %+v", got)
	}

	useMemoryProbe(t, "", "", "")
	restoreCollectProfile(highCollectProfile())
	resolveCollectProfile()
	if got := collectProfileSnapshot(); got != highCollectProfile() {
		t.Fatalf("读不到内存且核数大于 2 应保持全速档: %+v", got)
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
	fetch  int
	write  int
	perSrc int
	global int
}

func highCollectProfile() collectProfileValues {
	return collectProfileValues{source: 0, pages: 16, fetch: 128, write: 16, perSrc: 500, global: 5000}
}

func smallCollectProfile() collectProfileValues {
	return collectProfileValues{source: 2, pages: 4, fetch: 8, write: 2, perSrc: 32, global: 64}
}

func collectProfileSnapshot() collectProfileValues {
	return collectProfileValues{
		source: CollectSourceConcurrency,
		pages:  CollectPageWorkers,
		fetch:  CollectFetchInFlight,
		write:  CollectWriteMaxInflight,
		perSrc: CollectWriteMaxPendingPagesPerSource,
		global: CollectWriteMaxPendingPagesGlobal,
	}
}

func restoreCollectProfile(v collectProfileValues) {
	CollectSourceConcurrency = v.source
	CollectPageWorkers = v.pages
	CollectFetchInFlight = v.fetch
	CollectWriteMaxInflight = v.write
	CollectWriteMaxPendingPagesPerSource = v.perSrc
	CollectWriteMaxPendingPagesGlobal = v.global
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
