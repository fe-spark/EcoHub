package config

import "sync/atomic"

const (
	// CollectWriteSlotMin 写并发起点。真实耗时变快再往上加。
	CollectWriteSlotMin = 2
	// CollectWriteSlotMax 写并发硬顶，避免把数据库打满。
	CollectWriteSlotMax = 16
)

// ApplyWriteSlots 把当前写事务数套到页并发、取数和队列上。
func ApplyWriteSlots(n int) {
	if n < CollectWriteSlotMin {
		n = CollectWriteSlotMin
	}
	if n > CollectWriteSlotMax {
		n = CollectWriteSlotMax
	}
	CollectWriteMaxInflight = n
	CollectPageWorkers = n
	livePageWorkers.Store(int32(n))
	fetch := n * n
	if fetch > 128 {
		fetch = 128
	}
	CollectFetchInFlight = fetch
	liveFetchSlots.Store(int32(fetch))
	perSource := n * 16
	if perSource > 500 {
		perSource = 500
	}
	CollectWriteMaxPendingPagesPerSource = perSource
	global := perSource * n
	if global > 5000 {
		global = 5000
	}
	CollectWriteMaxPendingPagesGlobal = global
}

var (
	livePageWorkers atomic.Int32
	liveFetchSlots  atomic.Int32
)

// WriteSlotsApplied 进程启动已套用写槽。测试不跑 InitConfig 时为 false。
func WriteSlotsApplied() bool {
	return livePageWorkers.Load() > 0
}

// PageWorkerLimit 当前页并发。运行中随写槽更新，测试未套用写槽时读配置初值。
func PageWorkerLimit() int {
	if n := int(livePageWorkers.Load()); n > 0 {
		return n
	}
	if CollectPageWorkers > 0 {
		return CollectPageWorkers
	}
	return 1
}

// FetchInFlightLimit 当前全局取数在途。运行中随写槽更新。
func FetchInFlightLimit() int {
	if n := int(liveFetchSlots.Load()); n > 0 {
		return n
	}
	if CollectFetchInFlight > 0 {
		return CollectFetchInFlight
	}
	return 1
}
