package access

import (
	"context"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
)

const (
	// ApiLogQueueCapacity 运存物理硬顶保护：最大缓冲 2000 条，内存开销 < 2MB
	ApiLogQueueCapacity = 2000
	// ApiLogBatchSize 批量落库条数
	ApiLogBatchSize = 100
	// ApiLogFlushInterval 批量刷盘超时周期
	ApiLogFlushInterval = 1500 * time.Millisecond
	// DefaultRetentionDays 默认保留 7 天滑动窗口
	DefaultRetentionDays = 7
)

var (
	apiLogQueue    = make(chan *model.ApiAccessLog, ApiLogQueueCapacity)
	workerOnce     sync.Once
	workerCtx      context.Context
	workerCancel   context.CancelFunc
	tableMigrated  bool
	tableMu        sync.Mutex
	workerStopping atomic.Bool
)

func init() {
	workerCtx, workerCancel = context.WithCancel(context.Background())
	StartApiLogWorker()
}

// ensureApiAccessLogTable 确保数据表存在，失败不锁死 Once 允许后续重试
func ensureApiAccessLogTable() {
	if db.Mdb == nil || tableMigrated {
		return
	}
	tableMu.Lock()
	defer tableMu.Unlock()
	if tableMigrated {
		return
	}
	if !db.Mdb.Migrator().HasTable(&model.ApiAccessLog{}) {
		if err := db.Mdb.AutoMigrate(&model.ApiAccessLog{}); err != nil {
			syslog.Errorf("[ApiLogWorker] 自动迁移 api_access_logs 数据表失败: %v", err)
			return
		}
	}
	tableMigrated = true
}

// StartApiLogWorker 启动单例后台批量写协程
func StartApiLogWorker() {
	workerOnce.Do(func() {
		go runBatchFlushWorker(workerCtx)
	})
}

// StopApiLogWorker 停止工作协程（优雅退出）
func StopApiLogWorker() {
	workerStopping.Store(true)
	if workerCancel != nil {
		workerCancel()
	}
}

// EnqueueApiAccessLog 将接口请求日志丢入异步缓冲通道（主请求链路 0 阻塞、0 延迟）
func EnqueueApiAccessLog(item *model.ApiAccessLog) {
	if item == nil || workerStopping.Load() {
		return
	}
	select {
	case apiLogQueue <- item:
	default:
		// 极端突发流量下背压保护：丢弃非关键日志采样，物理阻断 OOM
	}
}

// runBatchFlushWorker 批量异步刷盘 Worker
func runBatchFlushWorker(ctx context.Context) {
	ticker := time.NewTicker(ApiLogFlushInterval)
	defer ticker.Stop()

	batch := make([]*model.ApiAccessLog, 0, ApiLogBatchSize)

	flush := func() {
		if len(batch) == 0 {
			return
		}
		if db.Mdb != nil {
			ensureApiAccessLogTable()
			if err := db.Mdb.CreateInBatches(batch, ApiLogBatchSize).Error; err != nil {
				syslog.Errorf("[ApiLogWorker] 批量落库失败: %v", err)
				// 失败时保留未落库数据，但若堆积超过硬顶，截断最老日志防止 OOM
				if len(batch) > ApiLogQueueCapacity {
					batch = batch[len(batch)-ApiLogQueueCapacity:]
				}
				return
			}
			batch = make([]*model.ApiAccessLog, 0, ApiLogBatchSize)
		} else {
			// 数据库尚未就绪时，保留当前 batch（超容截断最老条目）
			if len(batch) > ApiLogQueueCapacity {
				batch = batch[len(batch)-ApiLogQueueCapacity:]
			}
		}
	}

	for {
		select {
		case <-ctx.Done():
			// 停机时排空通道中的剩余日志并尝试最终刷盘
			for {
				select {
				case item := <-apiLogQueue:
					batch = append(batch, item)
					if len(batch) >= ApiLogBatchSize {
						flush()
					}
				default:
					flush()
					return
				}
			}
		case item := <-apiLogQueue:
			batch = append(batch, item)
			if len(batch) >= ApiLogBatchSize {
				flush()
			}
		case <-ticker.C:
			flush()
		}
	}
}

// PruneExpiredApiLogs 自动修剪过期接口日志，强制执行滑动窗口防无限扩盘
func PruneExpiredApiLogs(retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		retentionDays = DefaultRetentionDays
	}
	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	if db.Mdb == nil {
		return 0, nil
	}

	var totalDeleted int64
	// 分批按主键删除（每次 5000 条），防止大事务长时间锁表与主从延迟
	for {
		var ids []uint64
		if err := db.Mdb.Model(&model.ApiAccessLog{}).
			Where("created_at < ?", cutoff).
			Limit(5000).
			Pluck("id", &ids).Error; err != nil {
			return totalDeleted, err
		}
		if len(ids) == 0 {
			break
		}

		res := db.Mdb.Where("id IN ?", ids).Delete(&model.ApiAccessLog{})
		if res.Error != nil {
			return totalDeleted, res.Error
		}
		totalDeleted += res.RowsAffected
		if len(ids) < 5000 {
			break
		}
		time.Sleep(50 * time.Millisecond) // 间隙让出 CPU 与 IO
	}

	return totalDeleted, nil
}

// ApiLogQueryParams 查询参数
type ApiLogQueryParams struct {
	Page       int
	PageSize   int
	Day        string
	StartTime  string
	EndTime    string
	Method     string
	Status     string
	Duration   string
	ClientType string
	Q          string
}

// ApiLogQueryResult 查询结果
type ApiLogQueryResult struct {
	List       []model.ApiAccessLog `json:"list"`
	Total      int64                `json:"total"`
	Page       int                  `json:"page"`
	PageSize   int                  `json:"pageSize"`
	TotalToday int64                `json:"totalToday"`
	ErrorToday int64                `json:"errorToday"`
	SlowToday  int64                `json:"slowToday"`
	AvgMsToday int64                `json:"avgMsToday"`
}

// QueryApiAccessLogs 分页查询接口日志与今日概览统计
func QueryApiAccessLogs(p ApiLogQueryParams) (*ApiLogQueryResult, error) {
	if db.Mdb == nil {
		return &ApiLogQueryResult{List: []model.ApiAccessLog{}}, nil
	}
	ensureApiAccessLogTable()

	page := p.Page
	if page < 1 {
		page = 1
	}
	pageSize := p.PageSize
	if pageSize < 1 {
		pageSize = 20
	} else if pageSize > 100 {
		pageSize = 100
	}

	tx := db.Mdb.Model(&model.ApiAccessLog{})

	// 日期 / 时间范围筛选（若未指定任何时间范围，默认限制在近 3 天内，阻断跨全表无索引深度扫描）
	loc := time.Local
	now := time.Now().In(loc)
	todayZero := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)

	if p.StartTime != "" && p.EndTime != "" {
		tx = tx.Where("created_at BETWEEN ? AND ?", p.StartTime, p.EndTime)
	} else if p.Day != "" {
		startOfDay, err := time.ParseInLocation("2006-01-02", p.Day, loc)
		if err != nil {
			// Day 解析失败时不漏过滤，回退到默认近 3 天窗口保护，防止全表扫描
			recentStart := todayZero.AddDate(0, 0, -2)
			tx = tx.Where("created_at >= ?", recentStart)
		} else {
			nextDay := startOfDay.Add(24 * time.Hour)
			tx = tx.Where("created_at >= ? AND created_at < ?", startOfDay, nextDay)
		}
	} else {
		recentStart := todayZero.AddDate(0, 0, -2)
		tx = tx.Where("created_at >= ?", recentStart)
	}

	// 请求方法筛选
	if p.Method != "" && p.Method != "all" {
		tx = tx.Where("method = ?", strings.ToUpper(p.Method))
	}

	// 状态码筛选
	switch p.Status {
	case "2xx":
		tx = tx.Where("status >= 200 AND status < 300")
	case "3xx":
		tx = tx.Where("status >= 300 AND status < 400")
	case "4xx":
		tx = tx.Where("status >= 400 AND status < 500")
	case "5xx":
		tx = tx.Where("status >= 500")
	case "error":
		tx = tx.Where("status >= 400")
	case "":
	case "all":
	default:
		tx = tx.Where("status = ?", p.Status)
	}

	// 耗时筛选
	switch p.Duration {
	case "fast":
		tx = tx.Where("duration_ms < 100")
	case "medium":
		tx = tx.Where("duration_ms >= 100 AND duration_ms <= 500")
	case "slow":
		tx = tx.Where("duration_ms > 500")
	}

	// 终端类型
	if p.ClientType != "" && p.ClientType != "all" {
		tx = tx.Where("client_type = ?", p.ClientType)
	}

	// 路径或 IP 关键字模糊搜索（转义 LIKE 通配符防全表注入匹配）
	if p.Q != "" {
		escaped := strings.ReplaceAll(p.Q, "\\", "\\\\")
		escaped = strings.ReplaceAll(escaped, "%", "\\%")
		escaped = strings.ReplaceAll(escaped, "_", "\\_")
		likePattern := "%" + escaped + "%"
		tx = tx.Where("path LIKE ? ESCAPE '\\' OR ip LIKE ? ESCAPE '\\' OR query LIKE ? ESCAPE '\\' OR device_id LIKE ? ESCAPE '\\'", likePattern, likePattern, likePattern, likePattern)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, err
	}

	var list []model.ApiAccessLog
	offset := (page - 1) * pageSize
	if err := tx.Order("id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, err
	}

	// 统计今日宏观指标（带 30 秒短期缓存，防止分页频繁全量二次回表导致数据库 CPU 打满）
	totalToday, errorToday, slowToday, avgMsToday := getTodayMacroStats(now, loc)

	return &ApiLogQueryResult{
		List:       list,
		Total:      total,
		Page:       page,
		PageSize:   pageSize,
		TotalToday: totalToday,
		ErrorToday: errorToday,
		SlowToday:  slowToday,
		AvgMsToday: avgMsToday,
	}, nil
}

var (
	todayStatsCacheLock sync.RWMutex
	todayStatsCacheTime time.Time
	cachedTotalToday    int64
	cachedErrorToday    int64
	cachedSlowToday     int64
	cachedAvgMsToday    int64
)

type TodayStatsRow struct {
	TotalToday int64   `gorm:"column:total_today"`
	ErrorToday int64   `gorm:"column:error_today"`
	SlowToday  int64   `gorm:"column:slow_today"`
	AvgMsToday float64 `gorm:"column:avg_ms_today"`
}

func resetTodayStatsCache() {
	todayStatsCacheLock.Lock()
	todayStatsCacheTime = time.Time{}
	todayStatsCacheLock.Unlock()
}

func getTodayMacroStats(now time.Time, loc *time.Location) (int64, int64, int64, int64) {
	if db.Mdb == nil {
		return 0, 0, 0, 0
	}

	todayStatsCacheLock.RLock()
	if time.Since(todayStatsCacheTime) < 30*time.Second {
		tot, errCount, slow, avg := cachedTotalToday, cachedErrorToday, cachedSlowToday, cachedAvgMsToday
		todayStatsCacheLock.RUnlock()
		return tot, errCount, slow, avg
	}
	todayStatsCacheLock.RUnlock()

	todayStatsLock := &todayStatsCacheLock
	todayStatsLock.Lock()
	defer todayStatsLock.Unlock()
	if time.Since(todayStatsCacheTime) < 30*time.Second {
		return cachedTotalToday, cachedErrorToday, cachedSlowToday, cachedAvgMsToday
	}

	todayStart := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	tomorrowStart := todayStart.Add(24 * time.Hour)

	var row TodayStatsRow
	if err := db.Mdb.Model(&model.ApiAccessLog{}).
		Select(`
			COUNT(*) AS total_today,
			COALESCE(SUM(CASE WHEN status >= 400 THEN 1 ELSE 0 END), 0) AS error_today,
			COALESCE(SUM(CASE WHEN duration_ms > 500 THEN 1 ELSE 0 END), 0) AS slow_today,
			COALESCE(AVG(duration_ms), 0) AS avg_ms_today
		`).
		Where("created_at >= ? AND created_at < ?", todayStart, tomorrowStart).
		Scan(&row).Error; err != nil {
		syslog.Errorf("[ApiLog] 统计今日宏观指标失败: %v", err)
		return 0, 0, 0, 0
	}

	cachedTotalToday = row.TotalToday
	cachedErrorToday = row.ErrorToday
	cachedSlowToday = row.SlowToday
	cachedAvgMsToday = int64(row.AvgMsToday + 0.5)
	todayStatsCacheTime = time.Now()

	return cachedTotalToday, cachedErrorToday, cachedSlowToday, cachedAvgMsToday
}
