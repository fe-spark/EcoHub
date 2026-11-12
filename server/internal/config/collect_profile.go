package config

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

// collectWriteSlotBytes 每 512MiB 允许一个同时写库事务，再和核数取更小的那个。
const collectWriteSlotBytes int64 = 512 << 20

var (
	cgroupV2CPUPath    = "/sys/fs/cgroup/cpu.max"
	cgroupV1QuotaPath  = "/sys/fs/cgroup/cpu/cpu.cfs_quota_us"
	cgroupV1PeriodPath = "/sys/fs/cgroup/cpu/cpu.cfs_period_us"
	cgroupV2MemoryPath = "/sys/fs/cgroup/memory.max"
	cgroupV1MemoryPath = "/sys/fs/cgroup/memory/memory.limit_in_bytes"
	meminfoPath        = "/proc/meminfo"
)

// detectCgroupCPUQuota 读取容器 cgroup 配额换算为可用核数。无限制或非 Linux 返回 0。
func detectCgroupCPUQuota() int {
	if runtime.GOOS != "linux" && cgroupV2CPUPath == "/sys/fs/cgroup/cpu.max" {
		return 0
	}
	// 优先尝试 cgroup v2
	if data, err := os.ReadFile(cgroupV2CPUPath); err == nil {
		fields := strings.Fields(strings.TrimSpace(string(data)))
		if len(fields) >= 2 && fields[0] != "max" {
			quota, err1 := strconv.ParseInt(fields[0], 10, 64)
			period, err2 := strconv.ParseInt(fields[1], 10, 64)
			if err1 == nil && err2 == nil && period > 0 && quota > 0 {
				cpus := int(quota / period)
				if cpus < 1 {
					cpus = 1
				}
				return cpus
			}
		}
	}
	// 再尝试 cgroup v1
	quotaData, errQ := os.ReadFile(cgroupV1QuotaPath)
	periodData, errP := os.ReadFile(cgroupV1PeriodPath)
	if errQ == nil && errP == nil {
		quota, err1 := strconv.ParseInt(strings.TrimSpace(string(quotaData)), 10, 64)
		period, err2 := strconv.ParseInt(strings.TrimSpace(string(periodData)), 10, 64)
		if err1 == nil && err2 == nil && period > 0 && quota > 0 {
			cpus := int(quota / period)
			if cpus < 1 {
				cpus = 1
			}
			return cpus
		}
	}
	return 0
}

func applyContainerCPUQuota() {
	if os.Getenv("GOMAXPROCS") != "" {
		return
	}
	if quotaCPUs := detectCgroupCPUQuota(); quotaCPUs > 0 && quotaCPUs < runtime.GOMAXPROCS(0) {
		runtime.GOMAXPROCS(quotaCPUs)
	}
}

// collectScale 一次按核数和内存算出的采集并发。
type collectScale struct {
	inflight    int
	pagesPerSec int
	burst       int
	pageWorkers int
	fetch       int
	perSource   int
	global      int
}

// scaleCollectProfile 按核数和内存连续计算采集并发。
// 写事务数取核数和「每 512MiB 一个」里更小的那个，夹在 1 和 16。
// 页速是写事务数的 8 倍。内存读不到时只按核数算。
func scaleCollectProfile(cores int, memBytes int64) collectScale {
	if cores < 1 {
		cores = 1
	}
	inflight := cores
	if memBytes > 0 {
		slots := int(memBytes / collectWriteSlotBytes)
		if slots < 1 {
			slots = 1
		}
		if slots < inflight {
			inflight = slots
		}
	}
	if inflight > 16 {
		inflight = 16
	}
	pagesPerSec := inflight * 8
	pageWorkers := inflight
	if pageWorkers < 4 {
		pageWorkers = 4
	}
	if pageWorkers > 16 {
		pageWorkers = 16
	}
	fetch := pageWorkers * inflight
	if fetch > 128 {
		fetch = 128
	}
	perSource := pagesPerSec
	if perSource < 32 {
		perSource = 32
	}
	if perSource > 500 {
		perSource = 500
	}
	global := perSource * inflight
	if global < 64 {
		global = 64
	}
	if global > 5000 {
		global = 5000
	}
	return collectScale{
		inflight:    inflight,
		pagesPerSec: pagesPerSec,
		burst:       inflight * 2,
		pageWorkers: pageWorkers,
		fetch:       fetch,
		perSource:   perSource,
		global:      global,
	}
}

// resolveCollectProfile 按当前核数和内存重算采集并发。每次调用都覆盖上一档。
func resolveCollectProfile() {
	applyContainerCPUQuota()
	scaled := scaleCollectProfile(runtime.GOMAXPROCS(0), detectMemoryLimitBytes())
	CollectWriteMaxInflight = scaled.inflight
	CollectWritePagesPerSec = scaled.pagesPerSec
	CollectWriteBurstPages = scaled.burst
	CollectPageWorkers = scaled.pageWorkers
	CollectFetchInFlight = scaled.fetch
	CollectWriteMaxPendingPagesPerSource = scaled.perSource
	CollectWriteMaxPendingPagesGlobal = scaled.global
}

// detectMemoryLimitBytes 读取容器内存限额。cgroup 有限额时用限额，无限制时用 MemTotal。读不到返回 0。
func detectMemoryLimitBytes() int64 {
	if n, ok := readCgroupMemoryMax(); ok {
		return n
	}
	return readMemTotalBytes()
}

func collectMemoryLimitLabel() string {
	n := detectMemoryLimitBytes()
	if n <= 0 {
		return "未知"
	}
	return strconv.FormatInt(n>>20, 10) + "MiB"
}

func readCgroupMemoryMax() (int64, bool) {
	if data, err := os.ReadFile(cgroupV2MemoryPath); err == nil {
		text := strings.TrimSpace(string(data))
		if text != "" && text != "max" {
			n, err := strconv.ParseInt(text, 10, 64)
			if err == nil && n > 0 {
				return n, true
			}
		}
	}
	if data, err := os.ReadFile(cgroupV1MemoryPath); err == nil {
		n, err := strconv.ParseInt(strings.TrimSpace(string(data)), 10, 64)
		// 无限制时该文件是接近 int64 上限的页对齐大数。
		if err == nil && n > 0 && n < 1<<50 {
			return n, true
		}
	}
	return 0, false
}

func readMemTotalBytes() int64 {
	data, err := os.ReadFile(meminfoPath)
	if err != nil {
		return 0
	}
	for _, line := range strings.Split(string(data), "\n") {
		if !strings.HasPrefix(line, "MemTotal:") {
			continue
		}
		fields := strings.Fields(line)
		if len(fields) < 2 {
			return 0
		}
		kb, err := strconv.ParseInt(fields[1], 10, 64)
		if err != nil || kb <= 0 {
			return 0
		}
		return kb * 1024
	}
	return 0
}
