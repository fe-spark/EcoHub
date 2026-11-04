package config

import (
	"os"
	"runtime"
	"strconv"
	"strings"
)

var (
	cgroupV2CPUPath    = "/sys/fs/cgroup/cpu.max"
	cgroupV1QuotaPath  = "/sys/fs/cgroup/cpu/cpu.cfs_quota_us"
	cgroupV1PeriodPath = "/sys/fs/cgroup/cpu/cpu.cfs_period_us"
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

// resolveCollectProfile 按当前进程可用 CPU 核数确定采集并发。
// 启动时将容器 CPU 限制算入 GOMAXPROCS；2 核及以下收紧，3 核及以上保持全速档。
func resolveCollectProfile() {
	applyContainerCPUQuota()
	if runtime.GOMAXPROCS(0) > 2 {
		return
	}
	CollectSourceConcurrency = 2
	CollectPageWorkers = 4
	CollectPageWorkersSolo = 8
	CollectFetchInFlight = 8
	CollectWriteMaxInflight = 2
	CollectWritePagesPerSec = 40
	CollectWriteBurstPages = 12
	CollectWriteMaxPendingPagesPerSource = 32
	CollectWriteMaxPendingPagesGlobal = 64
}
