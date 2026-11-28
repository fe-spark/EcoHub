package rollup

import (
	"errors"
	"strings"
	"sync"
	"time"

	"server/internal/analytics/config"
	"server/internal/analytics/enrich"
	"server/internal/analytics/store"
	"server/internal/analytics/types"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const (
	maxPersistedVersionsPerOS = 50
	maxPersistedModels        = 80
	maxDailyTextJSONBytes     = 32768
	maxRollupLookbackDays     = 14
)

var (
	schedulerMu   sync.Mutex
	stopCh        chan struct{}
	schedulerDone chan struct{}
	rollupMu      sync.Mutex
)

// Mutex 返回归档互斥锁，供清理逻辑同步避让
func Mutex() *sync.Mutex {
	return &rollupMu
}

// StartScheduler 启动每小时日终归档后台协程（支持重复启停）
func StartScheduler() {
	schedulerMu.Lock()
	defer schedulerMu.Unlock()
	if stopCh != nil {
		return
	}
	ch := make(chan struct{})
	done := make(chan struct{})
	stopCh = ch
	schedulerDone = done
	go func() {
		defer close(done)
		safeRun()
		ticker := time.NewTicker(time.Hour)
		defer ticker.Stop()
		for {
			select {
			case <-ch:
				return
			case <-ticker.C:
				safeRun()
			}
		}
	}()
}

// StopScheduler 停止日终归档定时器协程并等待在途归档完成
func StopScheduler() {
	schedulerMu.Lock()
	ch := stopCh
	done := schedulerDone
	stopCh = nil
	schedulerDone = nil
	schedulerMu.Unlock()

	if ch != nil {
		close(ch)
	}
	if done != nil {
		<-done
	}
}

func safeRun() {
	if !config.IsEnabled() {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			syslog.Errorf("[Analytics] rollup panic: %v", rec)
		}
	}()
	RunDailyRollup()
}

// RunDailyRollup 将已闭合自然日的 Redis 统计快照 UPSERT 归档进 MySQL
func RunDailyRollup() {
	rollupMu.Lock()
	defer rollupMu.Unlock()
	if !config.IsEnabled() || db.Rdb == nil || db.Mdb == nil {
		return
	}

	now := time.Now().In(time.Local)
	yesterday := store.StartOfLocalDay(now).AddDate(0, 0, -1)
	last, err := loadRolledDay()
	if err != nil {
		syslog.Errorf("[Analytics] 读取滚动水位失败: %v", err)
		return
	}
	days := DaysToRoll(last, yesterday)
	// 凌晨 0~3 点刷新昨天快照，容纳跨天队列滞后写入的数据
	if len(days) == 0 && last.Equal(yesterday) && now.Hour() < 3 {
		days = []time.Time{yesterday}
	}
	for _, day := range days {
		stats, tops, has, snapErr := SnapshotDayFromRedis(day)
		if snapErr != nil {
			syslog.Errorf("[Analytics] 滚动快照失败 day=%s: %v", day.Format("2006-01-02"), snapErr)
			return
		}
		if has {
			if err := PersistDaily(stats, tops); err != nil {
				syslog.Errorf("[Analytics] 滚动落库失败 day=%s: %v", stats.Day, err)
				return
			}
		}
		if err := db.Rdb.Set(db.Cxt, store.RolledDayMetaKey, day.Format("2006-01-02"), 0).Err(); err != nil {
			syslog.Errorf("[Analytics] 写入滚动水位失败: %v", err)
			return
		}
	}
}

// DaysToRoll 返回 lastRolled 之后、yesterday 为止的闭合自然日（最多回溯 14 天）
func DaysToRoll(lastRolled, yesterday time.Time) []time.Time {
	start := lastRolled.AddDate(0, 0, 1)
	minStart := yesterday.AddDate(0, 0, -(maxRollupLookbackDays - 1))
	if start.Before(minStart) {
		start = minStart
	}
	if start.After(yesterday) {
		return nil
	}
	var days []time.Time
	for d := start; !d.After(yesterday); d = d.AddDate(0, 0, 1) {
		days = append(days, d)
	}
	return days
}

func loadRolledDay() (time.Time, error) {
	if db.Rdb != nil {
		raw, err := db.Rdb.Get(db.Cxt, store.RolledDayMetaKey).Result()
		if err == nil && strings.TrimSpace(raw) != "" {
			return ParseRolledDay(strings.TrimSpace(raw), nil)
		} else if err != nil && !errors.Is(err, redis.Nil) {
			return time.Time{}, err
		}
	}
	if db.Mdb != nil {
		var maxDay string
		if err := db.Mdb.Model(&model.AccessDailyStats{}).Select("MAX(day)").Scan(&maxDay).Error; err == nil && maxDay != "" {
			return ParseRolledDay(maxDay, nil)
		}
	}
	now := time.Now().In(time.Local)
	return store.StartOfLocalDay(now).AddDate(0, 0, -2), nil
}

// ParseRolledDay 解析水位日期字符串，异常/空值时默认回退至前天
func ParseRolledDay(raw string, err error) (time.Time, error) {
	if errors.Is(err, redis.Nil) || (err == nil && raw == "") {
		now := time.Now().In(time.Local)
		return store.StartOfLocalDay(now).AddDate(0, 0, -2), nil
	}
	if err != nil {
		return time.Time{}, err
	}
	t, parseErr := time.ParseInLocation("2006-01-02", raw, time.Local)
	if parseErr != nil {
		now := time.Now().In(time.Local)
		return store.StartOfLocalDay(now).AddDate(0, 0, -2), nil
	}
	return store.StartOfLocalDay(t), nil
}

// SnapshotDayFromRedis 从 Redis 提取单日全部统计与榜单快照
func SnapshotDayFromRedis(day time.Time) (model.AccessDailyStats, []model.AccessDailyTop, bool, error) {
	dayKey := day.Format("20060102")
	ctx := db.Cxt
	pipe := db.Rdb.Pipeline()

	uvCmd := pipe.PFCount(ctx, store.UVKey(dayKey))
	dayCmd := pipe.HGetAll(ctx, store.DayAggKey(dayKey))
	clientCmd := pipe.HGetAll(ctx, store.ClientKey(dayKey))
	actionCmd := pipe.HGetAll(ctx, store.ActionKey(dayKey))
	histCmd := pipe.HGetAll(ctx, store.HistKey(dayKey))
	droppedDayCmd := pipe.Get(ctx, store.DroppedDayKey(dayKey))
	bucketCmd := pipe.HGetAll(ctx, store.BucketKey(dayKey))
	sourceCallsCmd := pipe.HGetAll(ctx, store.CollectSourceKey(dayKey))

	topLimit := int64(enrich.TopPersistLimit - 1)
	playFetch := int64(enrich.PlayTopFetchCount(enrich.TopPersistLimit) - 1)
	searchCmd := pipe.ZRevRangeWithScores(ctx, store.TopSearchKey(dayKey), 0, topLimit)
	playCmd := pipe.ZRevRangeWithScores(ctx, store.TopPlayKey(dayKey), 0, playFetch)
	pageCmd := pipe.ZRevRangeWithScores(ctx, store.TopPathKey(dayKey), 0, topLimit)
	classifyCmd := pipe.ZRevRangeWithScores(ctx, store.TopClassifyKey(dayKey), 0, playFetch)

	// Web 快照
	webPVCmd := pipe.Get(ctx, store.WebPVKey(dayKey))
	webUVCmd := pipe.PFCount(ctx, store.WebUVKey(dayKey))
	webActionCmd := pipe.HGetAll(ctx, store.WebActionKey(dayKey))
	webTopCmd := pipe.ZRevRangeWithScores(ctx, store.WebTopPageKey(dayKey), 0, topLimit)
	webPlayCmd := pipe.ZRevRangeWithScores(ctx, store.WebTopPlayKey(dayKey), 0, playFetch)
	webSearchCmd := pipe.ZRevRangeWithScores(ctx, store.WebTopSearchKey(dayKey), 0, topLimit)
	webClassifyCmd := pipe.ZRevRangeWithScores(ctx, store.WebTopClassifyKey(dayKey), 0, playFetch)
	webBrowserCmd := pipe.HGetAll(ctx, store.WebBrowsersKey(dayKey))
	webOSCmd := pipe.HGetAll(ctx, store.WebOSKey(dayKey))

	// App 快照
	appPVCmd := pipe.Get(ctx, store.AppAllPVKey(dayKey))
	appUVCmd := pipe.PFCount(ctx, store.AppAllUVKey(dayKey))
	appActionCmd := pipe.HGetAll(ctx, store.AppActionKey(dayKey))
	appTopCmd := pipe.ZRevRangeWithScores(ctx, store.AppAllTopPageKey(dayKey), 0, topLimit)
	appPlayCmd := pipe.ZRevRangeWithScores(ctx, store.AppAllTopPlayKey(dayKey), 0, playFetch)
	appSearchCmd := pipe.ZRevRangeWithScores(ctx, store.AppAllTopSearchKey(dayKey), 0, topLimit)
	appClassifyCmd := pipe.ZRevRangeWithScores(ctx, store.AppAllTopClassifyKey(dayKey), 0, playFetch)
	platformsCmd := pipe.HGetAll(ctx, store.AppPlatformsKey(dayKey))
	appModelsCmd := pipe.HGetAll(ctx, store.AppModelsKey(dayKey))

	type platCmds struct {
		uv       *redis.IntCmd
		action   *redis.MapStringStringCmd
		version  *redis.MapStringStringCmd
		page     *redis.ZSliceCmd
		play     *redis.ZSliceCmd
		search   *redis.ZSliceCmd
		classify *redis.ZSliceCmd
	}
	platMap := make(map[string]platCmds, len(types.AppPlatforms))
	for _, p := range types.AppPlatforms {
		platMap[p] = platCmds{
			uv:       pipe.PFCount(ctx, store.AppUVKey(p, dayKey)),
			action:   pipe.HGetAll(ctx, store.AppPlatformActionKey(p, dayKey)),
			version:  pipe.HGetAll(ctx, store.AppVersionKey(p, dayKey)),
			page:     pipe.ZRevRangeWithScores(ctx, store.AppTopPageKey(p, dayKey), 0, topLimit),
			play:     pipe.ZRevRangeWithScores(ctx, store.AppTopPlayKey(p, dayKey), 0, playFetch),
			search:   pipe.ZRevRangeWithScores(ctx, store.AppTopSearchKey(p, dayKey), 0, topLimit),
			classify: pipe.ZRevRangeWithScores(ctx, store.AppTopClassifyKey(p, dayKey), 0, playFetch),
		}
	}

	// TVBox 快照
	tvboxUVCmd := pipe.PFCount(ctx, store.TvboxUVKey(dayKey))
	tvboxActionCmd := pipe.HGetAll(ctx, store.TvboxActionKey(dayKey))
	tvboxPlayCmd := pipe.ZRevRangeWithScores(ctx, store.TvboxTopPlayKey(dayKey), 0, playFetch)
	tvboxSearchCmd := pipe.ZRevRangeWithScores(ctx, store.TvboxTopSearchKey(dayKey), 0, topLimit)
	tvboxClassifyCmd := pipe.ZRevRangeWithScores(ctx, store.TvboxTopClassifyKey(dayKey), 0, playFetch)

	if _, err := pipe.Exec(ctx); err != nil && !errors.Is(err, redis.Nil) {
		return model.AccessDailyStats{}, nil, false, err
	}

	series, _ := store.FoldBucketMap(day, time.Now().In(time.Local), bucketCmd.Val())
	dayVals := store.ParseIntMap(dayCmd.Val())
	clientVals := store.ParseIntMap(clientCmd.Val())
	actionVals := store.ParseIntMap(actionCmd.Val())
	webActionVals := store.ParseIntMap(webActionCmd.Val())
	appActionVals := store.ParseIntMap(appActionCmd.Val())
	tvboxActionVals := store.ParseIntMap(tvboxActionCmd.Val())
	histVals := store.ParseIntMap(histCmd.Val())
	sourceCallsVals := store.ParseIntMap(sourceCallsCmd.Val())

	var droppedCount int64
	if n, err := droppedDayCmd.Int64(); err == nil && n > 0 {
		droppedCount = n
	}
	webPV, _ := webPVCmd.Int64()
	appPV, _ := appPVCmd.Int64()

	platformAction := make(map[string]map[string]int64, len(types.AppPlatforms))
	versionByPlatform := make(map[string]map[string]int64, len(types.AppPlatforms))
	platformUV := make(map[string]int64, len(types.AppPlatforms))
	for _, p := range types.AppPlatforms {
		pc := platMap[p]
		platformAction[p] = store.ParseIntMap(pc.action.Val())
		versionByPlatform[p] = store.CompactTopIntMap(store.ParseIntMap(pc.version.Val()), maxPersistedVersionsPerOS, maxDailyTextJSONBytes/4)
		platformUV[p] = pc.uv.Val()
	}
	allVersions := store.MergeNestedIntMap(versionByPlatform)
	compactModels := store.CompactTopIntMap(store.ParseIntMap(appModelsCmd.Val()), maxPersistedModels, maxDailyTextJSONBytes)

	stats := model.AccessDailyStats{
		Day:                day.Format("2006-01-02"),
		PV:                 dayVals["pv"] + dayVals["provide_pv"],
		UV:                 uvCmd.Val(),
		WebPV:              webPV,
		WebUV:              webUVCmd.Val(),
		AppPV:              appPV,
		AppUV:              appUVCmd.Val(),
		Err4:               dayVals["err4"] + dayVals["provide_err4"],
		Err5:               dayVals["err5"] + dayVals["provide_err5"],
		P95Ms:              store.EstimateP95(histVals),
		Dropped:            droppedCount,
		ProvidePV:          dayVals["provide_pv"],
		ProvideUV:          tvboxUVCmd.Val(),
		ProvideErr4:        dayVals["provide_err4"],
		ProvideErr5:        dayVals["provide_err5"],
		ClientJSON:         store.MarshalIntMap(clientVals),
		ActionJSON:         store.MarshalIntMap(actionVals),
		WebActionJSON:      store.MarshalIntMap(webActionVals),
		AppActionJSON:      store.MarshalIntMap(appActionVals),
		PlatformActionJSON: store.MarshalNestedIntMap(platformAction),
		TvboxActionJSON:    store.MarshalIntMap(tvboxActionVals),
		HistJSON:           store.MarshalIntMap(histVals),
		SeriesJSON:         store.MarshalSeries(series),
		PlatformJSON:       store.MarshalIntMap(store.ParseIntMap(platformsCmd.Val())),
		PlatformUVJSON:     store.MarshalIntMap(platformUV),
		VersionJSON:        store.MarshalNestedIntMap(versionByPlatform),
		BrowserJSON:        store.MarshalIntMap(store.ParseIntMap(webBrowserCmd.Val())),
		OSJSON:             store.MarshalIntMap(store.ParseIntMap(webOSCmd.Val())),
		ModelsJSON:         store.MarshalIntMap(compactModels),
		SourceCallsJSON:    store.MarshalIntMap(sourceCallsVals),
		RolledAt:           time.Now(),
	}

	tops := make([]model.AccessDailyTop, 0, enrich.TopPersistLimit*8)
	appendPlain := func(kind string, cmd *redis.ZSliceCmd) {
		tops = append(tops, topItemsToRows(stats.Day, kind, zsetToTopItems(cmd.Val()))...)
	}
	appendPlay := func(kind string, cmd *redis.ZSliceCmd) error {
		items, err := enrich.TakePlayTops(zsetToTopItems(cmd.Val()), enrich.TopPersistLimit)
		if err != nil {
			return err
		}
		tops = append(tops, topItemsToRows(stats.Day, kind, items)...)
		return nil
	}
	appendClassify := func(kind string, cmd *redis.ZSliceCmd) error {
		items, err := enrich.TakeClassifyTops(zsetToTopItems(cmd.Val()), enrich.TopPersistLimit)
		if err != nil {
			return err
		}
		tops = append(tops, topItemsToRows(stats.Day, kind, items)...)
		return nil
	}

	appendPlain("search", searchCmd)
	appendPlain("page", pageCmd)
	appendPlain("web_page", webTopCmd)
	appendPlain("web_search", webSearchCmd)
	appendPlain("app_page", appTopCmd)
	appendPlain("app_search", appSearchCmd)
	appendPlain("tvbox_search", tvboxSearchCmd)

	for _, pair := range []struct {
		kind string
		cmd  *redis.ZSliceCmd
	}{
		{"play", playCmd},
		{"web_play", webPlayCmd},
		{"app_play", appPlayCmd},
		{"tvbox_play", tvboxPlayCmd},
	} {
		if err := appendPlay(pair.kind, pair.cmd); err != nil {
			return model.AccessDailyStats{}, nil, false, err
		}
	}
	for _, pair := range []struct {
		kind string
		cmd  *redis.ZSliceCmd
	}{
		{"classify", classifyCmd},
		{"web_classify", webClassifyCmd},
		{"app_classify", appClassifyCmd},
		{"tvbox_classify", tvboxClassifyCmd},
	} {
		if err := appendClassify(pair.kind, pair.cmd); err != nil {
			return model.AccessDailyStats{}, nil, false, err
		}
	}
	for _, p := range types.AppPlatforms {
		pc := platMap[p]
		appendPlain(p+"_page", pc.page)
		appendPlain(p+"_search", pc.search)
		if err := appendPlay(p+"_play", pc.play); err != nil {
			return model.AccessDailyStats{}, nil, false, err
		}
		if err := appendClassify(p+"_classify", pc.classify); err != nil {
			return model.AccessDailyStats{}, nil, false, err
		}
	}

	has := stats.PV > 0 || stats.UV > 0 || stats.WebPV > 0 || stats.AppPV > 0 ||
		stats.ProvidePV > 0 || stats.Err4 > 0 || stats.Err5 > 0 || stats.Dropped > 0 ||
		len(clientVals) > 0 || len(actionVals) > 0 || len(tops) > 0 ||
		len(allVersions) > 0 || len(compactModels) > 0 || len(sourceCallsVals) > 0
	return stats, tops, has, nil
}

// PersistDaily 在事务内更新日汇总与排行榜单快照
func PersistDaily(stats model.AccessDailyStats, tops []model.AccessDailyTop) error {
	if db.Mdb == nil {
		return nil
	}
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Clauses(clause.OnConflict{UpdateAll: true}).Create(&stats).Error; err != nil {
			return err
		}
		if err := tx.Where("day = ?", stats.Day).Delete(&model.AccessDailyTop{}).Error; err != nil {
			return err
		}
		if len(tops) == 0 {
			return nil
		}
		return tx.CreateInBatches(&tops, 100).Error
	})
}

// LoadDailyStats 读取单日归档汇总
func LoadDailyStats(day string) (model.AccessDailyStats, bool, error) {
	var row model.AccessDailyStats
	if db.Mdb == nil {
		return row, false, errors.New("数据库未连接")
	}
	err := db.Mdb.Where("day = ?", day).First(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return row, false, nil
	}
	if err != nil {
		return row, false, err
	}
	return row, true, nil
}

// HasPersistedData 检查是否存在持久化分析记录
func HasPersistedData() (bool, int64, error) {
	if db.Mdb == nil {
		return false, 0, errors.New("数据库未连接")
	}
	var count int64
	if err := db.Mdb.Model(&model.AccessDailyStats{}).Count(&count).Error; err != nil {
		return false, 0, err
	}
	if count > 0 {
		return true, count, nil
	}
	var topCount int64
	if err := db.Mdb.Model(&model.AccessDailyTop{}).Limit(1).Count(&topCount).Error; err != nil {
		return false, 0, err
	}
	return topCount > 0, topCount, nil
}

func zsetToTopItems(pairs []redis.Z) []types.TopItem {
	items := make([]types.TopItem, 0, len(pairs))
	for _, p := range pairs {
		member, _ := p.Member.(string)
		if member == "" {
			continue
		}
		items = append(items, types.TopItem{Key: member, Count: int64(p.Score)})
	}
	return items
}

func topItemsToRows(day, kind string, items []types.TopItem) []model.AccessDailyTop {
	rows := make([]model.AccessDailyTop, 0, len(items))
	for i, it := range items {
		if i >= enrich.TopPersistLimit {
			break
		}
		rows = append(rows, model.AccessDailyTop{
			Day:      day,
			Kind:     kind,
			Rank:     i + 1,
			ItemKey:  it.Key,
			Count:    it.Count,
			Title:    it.Title,
			Category: it.Category,
			Poster:   it.Poster,
			Year:     it.Year,
		})
	}
	return rows
}
