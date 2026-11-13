package config

const (
	// MysqlMaxOpenConns 本进程业务连接池大小。
	MysqlMaxOpenConns = 48
	// MysqlForegroundConns 留给前台搜索和管理接口的连接。
	// 写槽上限 = MysqlMaxOpenConns - MysqlForegroundConns。
	MysqlForegroundConns = 32
	// CollectWriteSlotMin 写并发下限。锁错误时减到这里。
	CollectWriteSlotMin = 2
)

// WriteSlotMax 同时写库事务数的上限，由连接池减去前台预留得出。
func WriteSlotMax() int {
	n := MysqlMaxOpenConns - MysqlForegroundConns
	if n < CollectWriteSlotMin {
		return CollectWriteSlotMin
	}
	return n
}

// ApplyWriteSlots 只设置当前写槽。页并发、取数在途和队列不跟着变。
func ApplyWriteSlots(n int) {
	if n < CollectWriteSlotMin {
		n = CollectWriteSlotMin
	}
	if max := WriteSlotMax(); n > max {
		n = max
	}
	CollectWriteMaxInflight = n
}

// PageWorkerLimit 当前页并发。与写槽无关。
func PageWorkerLimit() int {
	if CollectPageWorkers > 0 {
		return CollectPageWorkers
	}
	return 1
}

// FetchInFlightLimit 当前全局取数在途。与写槽无关。
func FetchInFlightLimit() int {
	if CollectFetchInFlight > 0 {
		return CollectFetchInFlight
	}
	return 1
}
