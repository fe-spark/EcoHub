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

	"gorm.io/gorm"
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
	// apiLogFlushBackoff 落库失败冷却，禁止热路径每条日志立即重试整批
	apiLogFlushBackoff = 2 * time.Second
	// apiLogQueryMaxSpan 管理端单次列表查询允许的最大时间跨度，阻断跨全表深度扫描
	apiLogQueryMaxSpan = 3 * 24 * time.Hour
	// apiLogSearchSpan 关键字模糊搜索需回表过滤，窗口额外收口
	apiLogSearchSpan = 24 * time.Hour
	// apiLogMaxOffset 深分页物理上限：超过后不再继续取数
	apiLogMaxOffset = 20000
	// maxPruneIterations 单次修剪最大批次上限（20 批 * 5000 条 = 10 万条），防单次长事务拖垮从库或超时
	maxPruneIterations = 20
)

var (
	apiLogQueue    = make(chan *model.ApiAccessLog, ApiLogQueueCapacity)
	workerOnce     sync.Once
	workerCtx      context.Context
	workerCancel   context.CancelFunc
	tableMigrated  bool
	tableMu        sync.Mutex
	workerStopping atomic.Bool
	workerDone     chan struct{}
	workerDoneOnce sync.Once
	pruneMu        sync.Mutex
)

func init() {
	workerCtx, workerCancel = context.WithCancel(context.Background())
	workerDone = make(chan struct{})
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

// StopApiLogWorker 停止工作协程（优雅退出）。停止后 Enqueue 不再接收新日志，
// worker 会排空通道内积压并尝试最终刷盘，完成后关闭 ApiLogWorkerDone 信号。
func StopApiLogWorker() {
	workerStopping.Store(true)
	if workerCancel != nil {
		workerCancel()
	}
}

// ApiLogWorkerDone 返回 worker 完成排空与最终刷盘的信号，供优雅停机等待。
func ApiLogWorkerDone() <-chan struct{} {
	return workerDone
}

// capApiLogBatch 运存物理硬顶：超过 ApiLogQueueCapacity 时丢弃最旧积压，仅保留最新部分。
func capApiLogBatch(batch []*model.ApiAccessLog) []*model.ApiAccessLog {
	if len(batch) > ApiLogQueueCapacity {
		return batch[len(batch)-ApiLogQueueCapacity:]
	}
	return batch
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
	defer func() {
		workerDoneOnce.Do(func() { close(workerDone) })
	}()

	batch := make([]*model.ApiAccessLog, 0, ApiLogBatchSize)
	var flushFailUntil time.Time
	var batchRetryCount int

	flush := func(ignoreBackoff bool) {
		if len(batch) == 0 {
			return
		}
		// 缓冲硬顶：无论 DB 是否可用/退避是否生效，先丢弃最旧积压，保证运存上界
		batch = capApiLogBatch(batch)
		if db.Mdb == nil {
			return
		}
		if !ignoreBackoff && apiLogFlushBlocked(flushFailUntil, time.Now()) {
			// 退避期不重试整批，仅等待下一周期；内存上界已由上方硬顶裁剪保证
			return
		}
		ensureApiAccessLogTable()
		err := db.Mdb.Transaction(func(tx *gorm.DB) error {
			return tx.CreateInBatches(batch, ApiLogBatchSize).Error
		})
		if err != nil {
			batchRetryCount++
			syslog.Errorf("[ApiLogWorker] 批量落库失败 (第 %d 次重试): %v", batchRetryCount, err)
			// 重置切片中可能已被 GORM 回填主键的元素，防止重试时携带脏主键引发 Duplicate Key 冲突
			for _, it := range batch {
				if it != nil {
					it.ID = 0
				}
			}
			if batchRetryCount >= 3 {
				syslog.Errorf("[ApiLogWorker] 批次连续重试 %d 次失败，强制丢弃毒丸批次 (%d 条) 以恢复全局写队列", batchRetryCount, len(batch))
				batch = batch[:0]
				batchRetryCount = 0
				flushFailUntil = time.Time{}
				return
			}
			flushFailUntil = time.Now().Add(apiLogFlushBackoff)
			return
		}
		batchRetryCount = 0
		flushFailUntil = time.Time{}
		batch = batch[:0]
	}

	for {
		select {
		case <-ctx.Done():
			// 停机时排空通道中的剩余日志并尝试最终刷盘（忽略冷却，尽量落盘）
			for {
				select {
				case item := <-apiLogQueue:
					batch = append(batch, item)
					if len(batch) >= ApiLogBatchSize {
						flush(true)
					}
				default:
					flush(true)
					return
				}
			}
		case item := <-apiLogQueue:
			batch = append(batch, item)
			if len(batch) >= ApiLogBatchSize {
				flush(false)
			}
		case <-ticker.C:
			flush(false)
		}
	}
}

// PruneExpiredApiLogs 自动修剪过期接口日志，强制执行滑动窗口防无限扩盘
func PruneExpiredApiLogs(retentionDays int) (int64, error) {
	if retentionDays <= 0 {
		retentionDays = DefaultRetentionDays
	}
	if !pruneMu.TryLock() {
		syslog.Warnf("[ApiLog] 上一次接口日志修剪仍处于执行中，跳过本次触发以防行锁争用")
		return 0, nil
	}
	defer pruneMu.Unlock()

	cutoff := time.Now().AddDate(0, 0, -retentionDays)
	if db.Mdb == nil {
		return 0, nil
	}

	var totalDeleted int64
	// 分批按主键递增删除（每次 5000 条，单次最多循环 20 批），防止大事务长时间锁表、主从复制延迟及 HTTP 504 超时
	for i := 0; i < maxPruneIterations; i++ {
		var ids []uint64
		if err := db.Mdb.Model(&model.ApiAccessLog{}).
			Where("created_at < ?", cutoff).
			Order("id ASC").
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

	// 日期 / 时间范围筛选：无论显式指定还是缺省，最终窗口都收口在保留窗口内，
	// 阻断跨全表无索引深度扫描；关键字搜索（回表过滤成本高）窗口再收口到 24 小时。
	loc := time.Local
	now := time.Now().In(loc)
	todayZero := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, loc)
	defaultStart := todayZero.AddDate(0, 0, -2)

	var start, end time.Time
	switch {
	case p.StartTime != "" && p.EndTime != "":
		s, errS := time.ParseInLocation("2006-01-02 15:04:05", p.StartTime, loc)
		e, errE := time.ParseInLocation("2006-01-02 15:04:05", p.EndTime, loc)
		if errS != nil || errE != nil || e.Before(s) {
			// 时间解析失败或区间反向时不漏过滤，回退默认近 3 天窗口保护
			start, end = defaultStart, now
		} else {
			start, end = s, e
		}
	case p.Day != "":
		startOfDay, err := time.ParseInLocation("2006-01-02", p.Day, loc)
		if err != nil {
			// Day 解析失败时不漏过滤，回退到默认近 3 天窗口保护，防止全表扫描
			start, end = defaultStart, now
		} else {
			start, end = startOfDay, startOfDay.Add(24*time.Hour)
		}
	default:
		start, end = defaultStart, now
	}
	start, end = clampWindow(start, end, now, apiLogQueryMaxSpan)
	if p.Q != "" {
		start, end = clampWindow(start, end, now, apiLogSearchSpan)
	}
	tx = tx.Where("created_at >= ? AND created_at < ?", start, end)

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

	// 关键字模糊搜索（path / query / ip / device_id）。
	// 用自定义 ESCAPE '|'：MySQL 里写 ESCAPE '\\' 会因反斜杠字符串转义生成非法 SQL，
	// SQLite 又无默认转义，自定义转义符在两种方言下语义一致；搜索窗口已收口到 24h。
	if p.Q != "" {
		esc := strings.NewReplacer("|", "||", "%", "|%", "_", "|_")
		like := "%" + esc.Replace(p.Q) + "%"
		tx = tx.Where("path LIKE ? ESCAPE '|' OR query LIKE ? ESCAPE '|' OR ip LIKE ? ESCAPE '|' OR device_id LIKE ? ESCAPE '|'",
			like, like, like, like)
	}

	var total int64
	if err := tx.Count(&total).Error; err != nil {
		return nil, err
	}

	// 统计今日宏观指标（带 30 秒短期缓存，防止分页频繁全量二次回表导致数据库 CPU 打满）
	totalToday, errorToday, slowToday, avgMsToday := getTodayMacroStats(now, loc)

	offset := (page - 1) * pageSize
	if offset > apiLogMaxOffset {
		// 深分页退化为全量排序扫描，物理阻断：超过上限不再取数（total 仍真实，供分页条展示）
		return &ApiLogQueryResult{
			List:       []model.ApiAccessLog{},
			Total:      total,
			Page:       page,
			PageSize:   pageSize,
			TotalToday: totalToday,
			ErrorToday: errorToday,
			SlowToday:  slowToday,
			AvgMsToday: avgMsToday,
		}, nil
	}

	var list []model.ApiAccessLog
	if err := tx.Order("created_at DESC, id DESC").Offset(offset).Limit(pageSize).Find(&list).Error; err != nil {
		return nil, err
	}

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
		// 失败时设置短期退避（5 秒），防止并发请求瞬间穿透打垮数据库
		todayStatsCacheTime = time.Now().Add(-25 * time.Second)
		return cachedTotalToday, cachedErrorToday, cachedSlowToday, cachedAvgMsToday
	}

	cachedTotalToday = row.TotalToday
	cachedErrorToday = row.ErrorToday
	cachedSlowToday = row.SlowToday
	cachedAvgMsToday = int64(row.AvgMsToday + 0.5)
	todayStatsCacheTime = time.Now()

	return cachedTotalToday, cachedErrorToday, cachedSlowToday, cachedAvgMsToday
}

func apiLogFlushBlocked(failUntil, now time.Time) bool {
	return !failUntil.IsZero() && now.Before(failUntil)
}

// clampWindow 把查询窗口收口到 max 跨度内；end 指向未来时收敛到当前时间附近，
// 保证窗口始终落在真实数据区间。
func clampWindow(start, end, now time.Time, max time.Duration) (time.Time, time.Time) {
	if end.After(now.Add(time.Minute)) {
		end = now.Add(time.Minute)
	}
	if start.IsZero() || end.IsZero() || end.Before(start) {
		return start, end
	}
	if end.Sub(start) > max {
		start = end.Add(-max)
	}
	return start, end
}
