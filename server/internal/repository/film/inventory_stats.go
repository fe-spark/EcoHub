package film

import (
	"context"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"

	"gorm.io/gorm"
)

const (
	defaultInventoryFastWait    = 1500 * time.Millisecond
	defaultInventoryErrCooldown = 10 * time.Second

	// InventoryStatsStatusReady 统计已完成，数据为真实准确值。
	InventoryStatsStatusReady = "ready"
	// InventoryStatsStatusCollecting 正在采集中，暂不查库统计，前台展示数据入库中。
	InventoryStatsStatusCollecting = "collecting"
	// InventoryStatsStatusComputing 采集已结束、后台仍在统计中，前台展示计算中并继续轮询。
	InventoryStatsStatusComputing = "computing"
)

type inventoryStatsSnapshot struct {
	dbConn       *gorm.DB
	ready        bool
	computedDate string
	stats        InventoryStats
	lastErr      error
	failedAt     time.Time
}

type inventoryStatsRun struct {
	dbConn *gorm.DB
	done   chan struct{}
	stats  InventoryStats
	err    error
}

var (
	inventoryStatsMu           sync.Mutex
	inventoryStatsData         inventoryStatsSnapshot
	inventoryStatsInflight     *inventoryStatsRun
	inventoryStatsPending      *gorm.DB
	inventoryStatsDirty        bool
	inventoryStatsCollectingFn func() bool

	// 测试钩子：控制接口快等窗口、错误冷却期与实际查库实现。
	inventoryStatsFastWait    = defaultInventoryFastWait
	inventoryStatsErrCooldown = defaultInventoryErrCooldown
	fetchInventoriesFn        = getSourceInventoriesWithDB
)

// SetInventoryStatsCollectingChecker 注入采集状态判定函数，采集中不查库直接返回入库中状态。
func SetInventoryStatsCollectingChecker(fn func() bool) {
	inventoryStatsMu.Lock()
	inventoryStatsCollectingFn = fn
	inventoryStatsMu.Unlock()
}

func isInventoryCollecting() bool {
	inventoryStatsMu.Lock()
	fn := inventoryStatsCollectingFn
	inventoryStatsMu.Unlock()
	return fn != nil && fn()
}

// LibraryInventory 工作台片库规模，汇总全部已启用采集站的真实已采集数据。
type LibraryInventory struct {
	Films      int64 `json:"films"`
	Categories int64 `json:"categories"`
	Failures   int64 `json:"failures"`
}

// SourceInventory 单个采集站的真实已采集规模（影片、分类与当天失败页）。
type SourceInventory struct {
	Id         string `json:"id"`
	Name       string `json:"name"`
	Enabled    bool   `json:"enabled"`
	IsPrimary  bool   `json:"isPrimary"`
	Films      int64  `json:"films"`
	Categories int64  `json:"categories"`
	Failures   int64  `json:"failures"`
}

// InventoryStats 工作台片库规模：顶部汇总已启用站点，明细包含全部站点真实数据。
type InventoryStats struct {
	Status  string            `json:"status"`
	Library LibraryInventory  `json:"library"`
	Sources []SourceInventory `json:"sources"`
}

// RefreshInventoryStatsAsync 在采集完成或数据变更后于后台异步重算一次片库规模。
// 采集中只记脏不查库；脏标记还在时，下次读取不会交当日旧缓存。已有计算在进行中则登记补算。
func RefreshInventoryStatsAsync() {
	if isInventoryCollecting() {
		inventoryStatsMu.Lock()
		inventoryStatsDirty = true
		inventoryStatsMu.Unlock()
		return
	}
	currentDB := db.Mdb
	if currentDB == nil {
		return
	}
	inventoryStatsMu.Lock()
	inventoryStatsDirty = false
	_ = triggerInventoryStatsLocked(currentDB, true)
	inventoryStatsMu.Unlock()
}

// GetInventoryStats 获取工作台片库规模。
// 采集中直接返回 collecting（数据入库中）且不查库；采集结束后优先返回内存准确结果，尚未算完时返回 computing（计算中）。
func GetInventoryStats(ctx context.Context, refresh bool) (InventoryStats, error) {
	empty := InventoryStats{Status: InventoryStatsStatusReady, Sources: []SourceInventory{}}
	if err := ctx.Err(); err != nil {
		return empty, err
	}
	currentDB := db.Mdb
	if currentDB == nil {
		return empty, filmsnapshot.ErrSearchIndexMissing
	}
	if isInventoryCollecting() {
		return InventoryStats{
			Status:  InventoryStatsStatusCollecting,
			Sources: []SourceInventory{},
		}, nil
	}

	run, cached, cachedErr, ok := acquireInventoryStatsRun(currentDB, refresh)
	if ok {
		return cached, nil
	}
	if cachedErr != nil {
		return empty, cachedErr
	}

	timer := time.NewTimer(inventoryStatsFastWait)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return empty, ctx.Err()
	case <-run.done:
		if run.err != nil {
			return empty, run.err
		}
		return cloneInventoryStats(run.stats), nil
	case <-timer.C:
		return InventoryStats{
			Status:  InventoryStatsStatusComputing,
			Sources: []SourceInventory{},
		}, nil
	}
}

func acquireInventoryStatsRun(currentDB *gorm.DB, refresh bool) (*inventoryStatsRun, InventoryStats, error, bool) {
	inventoryStatsMu.Lock()
	defer inventoryStatsMu.Unlock()

	if inventoryStatsDirty {
		refresh = true
		inventoryStatsDirty = false
	}
	if !refresh {
		if inventoryStatsInflight != nil && inventoryStatsInflight.dbConn == currentDB {
			return inventoryStatsInflight, InventoryStats{}, nil, false
		}
		if inventoryStatsData.dbConn == currentDB {
			if inventoryStatsData.lastErr != nil && !inventoryStatsData.failedAt.IsZero() && time.Since(inventoryStatsData.failedAt) < inventoryStatsErrCooldown {
				return nil, InventoryStats{}, inventoryStatsData.lastErr, false
			}
			if inventoryStatsData.ready && inventoryStatsData.computedDate == shanghaiDateKey(time.Now()) {
				return nil, cloneInventoryStats(inventoryStatsData.stats), nil, true
			}
		}
	}

	run := triggerInventoryStatsLocked(currentDB, refresh)
	return run, InventoryStats{}, nil, false
}

func triggerInventoryStatsLocked(currentDB *gorm.DB, queueIfBusy bool) *inventoryStatsRun {
	if inventoryStatsInflight != nil && inventoryStatsInflight.dbConn == currentDB {
		if queueIfBusy {
			inventoryStatsPending = currentDB
		}
		return inventoryStatsInflight
	}
	if inventoryStatsData.dbConn != currentDB {
		inventoryStatsData = inventoryStatsSnapshot{dbConn: currentDB}
	} else {
		inventoryStatsData.lastErr = nil
		inventoryStatsData.failedAt = time.Time{}
	}
	inventoryStatsPending = nil
	run := &inventoryStatsRun{
		dbConn: currentDB,
		done:   make(chan struct{}),
	}
	inventoryStatsInflight = run
	go executeInventoryStatsRun(run)
	return run
}

func executeInventoryStatsRun(run *inventoryStatsRun) {
	for {
		computedDate := shanghaiDateKey(time.Now())
		sources, err := fetchInventoriesFn(context.Background(), run.dbConn)
		var fresh InventoryStats
		if err == nil {
			fresh = InventoryStats{
				Status:  InventoryStatsStatusReady,
				Library: summarizeEnabledInventory(sources),
				Sources: sources,
			}
		}

		inventoryStatsMu.Lock()
		// 本轮计算期间若又收到新的采集完成/变更触发，且当前数据库句柄未切换，继续补算一轮最新结果。
		if inventoryStatsInflight == run && inventoryStatsPending == run.dbConn {
			inventoryStatsPending = nil
			inventoryStatsMu.Unlock()
			continue
		}
		if err != nil {
			run.err = err
			if inventoryStatsData.dbConn == run.dbConn {
				inventoryStatsData.lastErr = err
				inventoryStatsData.failedAt = time.Now()
			}
		} else {
			run.stats = fresh
			if inventoryStatsData.dbConn == run.dbConn {
				inventoryStatsData = inventoryStatsSnapshot{
					dbConn:       run.dbConn,
					ready:        true,
					computedDate: computedDate,
					stats:        fresh,
				}
			}
		}
		if inventoryStatsInflight == run {
			inventoryStatsInflight = nil
			if inventoryStatsPending == run.dbConn {
				inventoryStatsPending = nil
			}
		}
		close(run.done)
		inventoryStatsMu.Unlock()
		return
	}
}

func shanghaiDateKey(now time.Time) string {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		loc = time.Local
	}
	return now.In(loc).Format("2006-01-02")
}

func cloneInventoryStats(stats InventoryStats) InventoryStats {
	primaryID := activePrimaryID()
	status := stats.Status
	if status == "" {
		status = InventoryStatsStatusReady
	}
	out := InventoryStats{
		Status:  status,
		Library: stats.Library,
		Sources: make([]SourceInventory, len(stats.Sources)),
	}
	copy(out.Sources, stats.Sources)
	for i := range out.Sources {
		out.Sources[i].IsPrimary = out.Sources[i].Id == primaryID
	}
	return out
}

func summarizeEnabledInventory(sources []SourceInventory) LibraryInventory {
	var library LibraryInventory
	for _, source := range sources {
		if !source.Enabled {
			continue
		}
		library.Films += source.Films
		library.Categories += source.Categories
		library.Failures += source.Failures
	}
	return library
}

func getSourceInventoriesWithDB(ctx context.Context, connDB *gorm.DB) ([]SourceInventory, error) {
	if connDB == nil {
		return nil, filmsnapshot.ErrSearchIndexMissing
	}
	sourceList, err := repository.GetCollectSourceListDB(connDB)
	if err != nil {
		return nil, err
	}
	sourceIDs := make([]string, 0, len(sourceList))
	for _, source := range sourceList {
		if id := strings.TrimSpace(source.Id); id != "" {
			sourceIDs = append(sourceIDs, id)
		}
	}
	films, err := countFilmsBySource(ctx, connDB, sourceIDs)
	if err != nil {
		return nil, err
	}
	categories, err := countCategoriesBySource(ctx, connDB)
	if err != nil {
		return nil, err
	}
	_, failureCounts, err := repository.CountTodayCollectFailuresDB(connDB, time.Now())
	if err != nil {
		return nil, err
	}
	sources := make([]SourceInventory, 0, len(sourceIDs))
	for _, source := range sourceList {
		id := strings.TrimSpace(source.Id)
		if id == "" {
			continue
		}
		sources = append(sources, SourceInventory{
			Id:         id,
			Name:       source.Name,
			Enabled:    source.State,
			Films:      films[id],
			Categories: categories[id],
			Failures:   failureCounts[id],
		})
	}
	return sources, nil
}

func activePrimaryID() string {
	if primary := repository.CurrentPrimarySource(); primary != nil {
		return strings.TrimSpace(primary.Id)
	}
	return ""
}
