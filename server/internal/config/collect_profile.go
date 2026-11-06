package config

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

const smallProfileMemoryBytes int64 = 2 << 30

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

// resolveCollectProfile 按 CPU 与内存确定采集并发。
// 启动时把容器 CPU 限额算进 GOMAXPROCS。2 核及以下，或可用内存不超过 2GiB，使用小档。
// 小档只收紧取数和写队列，写并发与站并发保持初值。3 核及以上且内存更大时保持全速，不会把已经收紧的小档改回。
func resolveCollectProfile() {
	applyContainerCPUQuota()
	if runtime.GOMAXPROCS(0) > 2 && !memoryWithinSmallProfile() {
		return
	}
	applySmallCollectProfile()
}

func applySmallCollectProfile() {
	CollectPageWorkers = 4
	CollectFetchInFlight = 8
	CollectWriteMaxPendingPagesPerSource = 32
	CollectWriteMaxPendingPagesGlobal = 64
}

// detectMemoryLimitBytes 读取容器内存限额。cgroup 有限额时用限额，无限制时用 MemTotal。读不到返回 0。
func detectMemoryLimitBytes() int64 {
	if n, ok := readCgroupMemoryMax(); ok {
		return n
	}
	return readMemTotalBytes()
}

func memoryWithinSmallProfile() bool {
	n := detectMemoryLimitBytes()
	return n > 0 && n <= smallProfileMemoryBytes
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
