package config

const (
	// MysqlMaxOpenConns 本进程业务连接池大小。
	MysqlMaxOpenConns = 48
	// MysqlForegroundConns 留给前台搜索和管理接口的连接。
	// 写槽上限 = MysqlMaxOpenConns - MysqlForegroundConns（默认 16 路）。
	MysqlForegroundConns = 32
	// CollectWriteSlotMin 写并发下限。
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

// PageWorkerLimit 当前单站页并发上限。
func PageWorkerLimit() int {
	if CollectPageWorkers > 0 {
		return CollectPageWorkers
	}
	return 1
}

// FetchInFlightLimit 当前全局取数在途上限。
func FetchInFlightLimit() int {
	if CollectFetchInFlight > 0 {
		return CollectFetchInFlight
	}
	return 1
}
