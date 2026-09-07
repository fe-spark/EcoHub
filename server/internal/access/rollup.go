package access

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const accessTopKeep = 10

func rolledDayKey() string {
	return config.AccessKeyPrefix + "meta:rolled_day"
}

func startOfLocalDay(t time.Time) time.Time {
	loc := t.Location()
	return time.Date(t.Year(), t.Month(), t.Day(), 0, 0, 0, 0, loc)
}

func isLocalToday(target, now time.Time) bool {
	return startOfLocalDay(target).Equal(startOfLocalDay(now))
}

// daysToRoll 返回 lastRolled 之后、yesterday 为止的所有闭合自然日（不设任何天数截断）。
func daysToRoll(lastRolled, yesterday time.Time) []time.Time {
	start := lastRolled.AddDate(0, 0, 1)
	if start.After(yesterday) {
		return nil
	}
	var days []time.Time
	for d := start; !d.After(yesterday); d = d.AddDate(0, 0, 1) {
		days = append(days, d)
	}
	return days
}

func marshalIntMap(m map[string]int64) string {
	if len(m) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func marshalNestedIntMap(m map[string]map[string]int64) string {
	if len(m) == 0 {
		return "{}"
	}
	raw, err := json.Marshal(m)
	if err != nil {
		return "{}"
	}
	return string(raw)
}

func mergeNestedIntMap(m map[string]map[string]int64) map[string]int64 {
	merged := map[string]int64{}
	for _, inner := range m {
		for k, v := range inner {
			merged[k] += v
		}
	}
	return merged
}

// versionsFromDaily 解析日归档版本分布。新格式为按平台嵌套；旧格式为扁平 version->count，两者都可读。
func versionsFromDaily(raw, platform string) map[string]int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" || raw == "{}" {
		return map[string]int64{}
	}
	var nested map[string]map[string]int64
	if err := json.Unmarshal([]byte(raw), &nested); err == nil && len(nested) > 0 {
		platform = strings.ToLower(strings.TrimSpace(platform))
		if platform != "" && platform != "all" {
			if m := nested[platform]; len(m) > 0 {
				return m
			}
			return map[string]int64{}
		}
		return mergeNestedIntMap(nested)
	}
	return unmarshalIntMap(raw)
}

func unmarshalIntMap(raw string) map[string]int64 {
	out := map[string]int64{}
	if raw == "" {
		return out
	}
	_ = json.Unmarshal([]byte(raw), &out)
	if out == nil {
		return map[string]int64{}
	}
	return out
}

var (
	rollupOnce sync.Once
	rollupMu   sync.Mutex
)

func startDailyRollup() {
	rollupOnce.Do(func() {
		go func() {
			safeDailyRollup()
			ticker := time.NewTicker(time.Hour)
			defer ticker.Stop()
			for range ticker.C {
				safeDailyRollup()
			}
		}()
	})
}

func safeDailyRollup() {
	if !config.AccessLogEnabled {
		return
	}
	defer func() {
		if rec := recover(); rec != nil {
			syslog.Errorf("[Access] rollup panic: %v", rec)
		}
	}()
	RunDailyRollup()
}

// RunDailyRollup 把已闭合的 Redis 日桶 UPSERT 进 MySQL，并裁剪 14 天外的行。
func RunDailyRollup() {
	if !config.AccessLogEnabled || db.Rdb == nil || db.Mdb == nil {
		return
	}
	rollupMu.Lock()
	defer rollupMu.Unlock()

	now := time.Now().In(time.Local)
	yesterday := startOfLocalDay(now).AddDate(0, 0, -1)
	last, err := loadRolledDay()
	if err != nil {
		syslog.Errorf("[Access] 读取滚动水位失败: %v", err)
		return
	}
	days := daysToRoll(last, yesterday)
	// 若已对齐至昨天但在凌晨窗口（0点-3点），支持对昨天再次刷新快照，容纳跨天缓冲队列中滞后写入的数据
	if len(days) == 0 && last.Equal(yesterday) && now.Hour() < 3 {
		days = []time.Time{yesterday}
	}
	for _, day := range days {
		stats, tops, has, snapErr := snapshotDayFromRedis(day)
		if snapErr != nil {
			syslog.Errorf("[Access] 滚动快照失败 day=%s: %v", day.Format("2006-01-02"), snapErr)
			return
		}
		if has {
			if err := persistDaily(stats, tops); err != nil {
				syslog.Errorf("[Access] 滚动落库失败 day=%s: %v", stats.Day, err)
				return
			}
		}
		if err := saveRolledDay(day); err != nil {
			syslog.Errorf("[Access] 写入滚动水位失败: %v", err)
			return
		}
	}
}

func loadRolledDay() (time.Time, error) {
	if db.Rdb != nil {
		raw, err := db.Rdb.Get(db.Cxt, rolledDayKey()).Result()
		if err == nil && strings.TrimSpace(raw) != "" {
			return parseRolledDay(strings.TrimSpace(raw), nil)
		} else if err != nil && err != redis.Nil {
			return time.Time{}, err
		}
	}

	// Redis 未命中或解析失败，从 MySQL 查询已落库的最大日期
	if db.Mdb != nil {
		var maxDay string
		if err := db.Mdb.Model(&model.AccessDailyStats{}).Select("MAX(day)").Scan(&maxDay).Error; err == nil && maxDay != "" {
			return parseRolledDay(maxDay, nil)
		}
	}

	// 首次运行或无任何历史记录：默认从昨日前一天开始检测
	now := time.Now().In(time.Local)
	return startOfLocalDay(now).AddDate(0, 0, -2), nil
}

func parseRolledDay(raw string, err error) (time.Time, error) {
	if err == redis.Nil || (err == nil && raw == "") {
		now := time.Now().In(time.Local)
		return startOfLocalDay(now).AddDate(0, 0, -2), nil
	}
	if err != nil {
		return time.Time{}, err
	}
	t, parseErr := time.ParseInLocation("2006-01-02", raw, time.Local)
	if parseErr != nil {
		now := time.Now().In(time.Local)
		return startOfLocalDay(now).AddDate(0, 0, -2), nil
	}
	return startOfLocalDay(t), nil
}

func saveRolledDay(day time.Time) error {
	return db.Rdb.Set(db.Cxt, rolledDayKey(), day.Format("2006-01-02"), 0).Err()
}

func snapshotDayFromRedis(day time.Time) (model.AccessDailyStats, []model.AccessDailyTop, bool, error) {
	dayKey := day.Format("20060102")
	ctx := db.Cxt
	pipe := db.Rdb.Pipeline()
	uvCmd := pipe.PFCount(ctx, uvKey(dayKey))
	dayCmd := pipe.HGetAll(ctx, dayAggKey(dayKey))
	clientCmd := pipe.HGetAll(ctx, clientKey(dayKey))
	actionCmd := pipe.HGetAll(ctx, actionKey(dayKey))
	histCmd := pipe.HGetAll(ctx, histKey(dayKey))
	droppedDayCmd := pipe.Get(ctx, droppedDayKey(dayKey))
	searchCmd := pipe.ZRevRangeWithScores(ctx, topSearchKey(dayKey), 0, accessTopKeep-1)
	playCmd := pipe.ZRevRangeWithScores(ctx, topPlayKey(dayKey), 0, int64(playTopFetchCount(accessTopKeep)-1))
	pageCmd := pipe.ZRevRangeWithScores(ctx, topPathKey(dayKey), 0, accessTopKeep-1)

	// Web 专属快照
	webPVCmd := pipe.Get(ctx, webPVKey(dayKey))
	webUVCmd := pipe.PFCount(ctx, webUVKey(dayKey))
	webTopCmd := pipe.ZRevRangeWithScores(ctx, webTopPageKey(dayKey), 0, accessTopKeep-1)
	webPlayCmd := pipe.ZRevRangeWithScores(ctx, webTopPlayKey(dayKey), 0, int64(playTopFetchCount(accessTopKeep)-1))
	webSearchCmd := pipe.ZRevRangeWithScores(ctx, webTopSearchKey(dayKey), 0, accessTopKeep-1)
	webClassifyCmd := pipe.ZRevRangeWithScores(ctx, webTopClassifyKey(dayKey), 0, accessTopKeep-1)
	webBrowserCmd := pipe.HGetAll(ctx, webBrowsersKey(dayKey))
	webOSCmd := pipe.HGetAll(ctx, webOSKey(dayKey))

	// App 专属快照
	appPVCmd := pipe.Get(ctx, appAllPVKey(dayKey))
	appUVCmd := pipe.PFCount(ctx, appAllUVKey(dayKey))
	appTopCmd := pipe.ZRevRangeWithScores(ctx, appAllTopPageKey(dayKey), 0, accessTopKeep-1)
	appPlayCmd := pipe.ZRevRangeWithScores(ctx, appAllTopPlayKey(dayKey), 0, int64(playTopFetchCount(accessTopKeep)-1))
	appSearchCmd := pipe.ZRevRangeWithScores(ctx, appAllTopSearchKey(dayKey), 0, accessTopKeep-1)
	appClassifyCmd := pipe.ZRevRangeWithScores(ctx, appAllTopClassifyKey(dayKey), 0, accessTopKeep-1)
	platformsCmd := pipe.HGetAll(ctx, appPlatformsKey(dayKey))
	androidUVCmd := pipe.PFCount(ctx, appUVKey("android", dayKey))
	harmonyUVCmd := pipe.PFCount(ctx, appUVKey("harmony", dayKey))
	iosUVCmd := pipe.PFCount(ctx, appUVKey("ios", dayKey))
	androidVerCmd := pipe.HGetAll(ctx, appVersionKey("android", dayKey))
	harmonyVerCmd := pipe.HGetAll(ctx, appVersionKey("harmony", dayKey))
	iosVerCmd := pipe.HGetAll(ctx, appVersionKey("ios", dayKey))
	appModelsCmd := pipe.HGetAll(ctx, appModelsKey(dayKey))
	classifyCmd := pipe.ZRevRangeWithScores(ctx, topClassifyKey(dayKey), 0, accessTopKeep-1)

	type platformTopCmds struct {
		page     *redis.ZSliceCmd
		play     *redis.ZSliceCmd
		search   *redis.ZSliceCmd
		classify *redis.ZSliceCmd
	}
	appPlatforms := []string{"android", "harmony", "ios"}
	platTops := make(map[string]platformTopCmds, len(appPlatforms))
	for _, p := range appPlatforms {
		platTops[p] = platformTopCmds{
			page:     pipe.ZRevRangeWithScores(ctx, appTopPageKey(p, dayKey), 0, accessTopKeep-1),
			play:     pipe.ZRevRangeWithScores(ctx, appTopPlayKey(p, dayKey), 0, int64(playTopFetchCount(accessTopKeep)-1)),
			search:   pipe.ZRevRangeWithScores(ctx, appTopSearchKey(p, dayKey), 0, accessTopKeep-1),
			classify: pipe.ZRevRangeWithScores(ctx, appTopClassifyKey(p, dayKey), 0, accessTopKeep-1),
		}
	}

	// TVBox 专属快照
	tvboxUVCmd := pipe.PFCount(ctx, tvboxUVKey(dayKey))
	tvboxPlayCmd := pipe.ZRevRangeWithScores(ctx, tvboxTopPlayKey(dayKey), 0, int64(playTopFetchCount(accessTopKeep)-1))
	tvboxSearchCmd := pipe.ZRevRangeWithScores(ctx, tvboxTopSearchKey(dayKey), 0, accessTopKeep-1)
	tvboxClassifyCmd := pipe.ZRevRangeWithScores(ctx, tvboxTopClassifyKey(dayKey), 0, accessTopKeep-1)

	nMin := minuteSlotCount(day, time.Now().In(time.Local))
	slots := queueMinuteSlots(pipe, day, nMin)
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return model.AccessDailyStats{}, nil, false, err
	}
	series, _ := foldMinuteSlots(slots)
	dayVals := parseIntMap(dayCmd.Val())
	clientVals := parseIntMap(clientCmd.Val())
	actionVals := parseIntMap(actionCmd.Val())
	histVals := parseIntMap(histCmd.Val())

	var droppedCount int64
	if n, err := droppedDayCmd.Int64(); err == nil && n > 0 {
		droppedCount = n
	}

	var webPV int64
	if v, err := webPVCmd.Int64(); err == nil {
		webPV = v
	}
	var appPV int64
	if v, err := appPVCmd.Int64(); err == nil {
		appPV = v
	}

	versionByPlatform := map[string]map[string]int64{
		"android": parseIntMap(androidVerCmd.Val()),
		"harmony": parseIntMap(harmonyVerCmd.Val()),
		"ios":     parseIntMap(iosVerCmd.Val()),
	}
	allVersions := mergeNestedIntMap(versionByPlatform)

	platformUV := map[string]int64{
		"android": androidUVCmd.Val(),
		"harmony": harmonyUVCmd.Val(),
		"ios":     iosUVCmd.Val(),
	}

	stats := model.AccessDailyStats{
		Day:            day.Format("2006-01-02"),
		PV:             dayVals["pv"] + dayVals["provide_pv"],
		UV:             uvCmd.Val(),
		WebPV:          webPV,
		WebUV:          webUVCmd.Val(),
		AppPV:          appPV,
		AppUV:          appUVCmd.Val(),
		Err4:           dayVals["err4"] + dayVals["provide_err4"],
		Err5:           dayVals["err5"] + dayVals["provide_err5"],
		P95Ms:          EstimateP95(histVals),
		Dropped:        droppedCount,
		ProvidePV:      dayVals["provide_pv"],
		ProvideUV:      tvboxUVCmd.Val(),
		ProvideErr4:    dayVals["provide_err4"],
		ProvideErr5:    dayVals["provide_err5"],
		ClientJSON:     marshalIntMap(clientVals),
		ActionJSON:     marshalIntMap(actionVals),
		HistJSON:       marshalIntMap(histVals),
		SeriesJSON:     marshalSeries(series),
		PlatformJSON:   marshalIntMap(parseIntMap(platformsCmd.Val())),
		PlatformUVJSON: marshalIntMap(platformUV),
		VersionJSON:    marshalNestedIntMap(versionByPlatform),
		BrowserJSON:    marshalIntMap(parseIntMap(webBrowserCmd.Val())),
		OSJSON:         marshalIntMap(parseIntMap(webOSCmd.Val())),
		ModelsJSON:     marshalIntMap(parseIntMap(appModelsCmd.Val())),
		RolledAt:       time.Now(),
	}

	tops := make([]model.AccessDailyTop, 0, accessTopKeep*4)
	searchItems := zsetToTopItems(searchCmd.Val())
	playItems := takePlayTops(zsetToTopItems(playCmd.Val()), accessTopKeep)
	webItems := zsetToTopItems(webTopCmd.Val())
	appItems := zsetToTopItems(appTopCmd.Val())

	tops = append(tops, topItemsToRows(stats.Day, "search", searchItems)...)
	tops = append(tops, topItemsToRows(stats.Day, "play", playItems)...)
	tops = append(tops, topItemsToRows(stats.Day, "page", zsetToTopItems(pageCmd.Val()))...)
	tops = append(tops, topItemsToRows(stats.Day, "web_page", webItems)...)
	tops = append(tops, topItemsToRows(stats.Day, "app_page", appItems)...)
	tops = append(tops, topItemsToRows(stats.Day, "web_play", takePlayTops(zsetToTopItems(webPlayCmd.Val()), accessTopKeep))...)
	tops = append(tops, topItemsToRows(stats.Day, "web_search", zsetToTopItems(webSearchCmd.Val()))...)
	tops = append(tops, topItemsToRows(stats.Day, "web_classify", takeClassifyTops(zsetToTopItems(webClassifyCmd.Val()), accessTopKeep))...)
	tops = append(tops, topItemsToRows(stats.Day, "app_play", takePlayTops(zsetToTopItems(appPlayCmd.Val()), accessTopKeep))...)
	tops = append(tops, topItemsToRows(stats.Day, "app_search", zsetToTopItems(appSearchCmd.Val()))...)
	tops = append(tops, topItemsToRows(stats.Day, "app_classify", takeClassifyTops(zsetToTopItems(appClassifyCmd.Val()), accessTopKeep))...)
	for _, p := range appPlatforms {
		cmds := platTops[p]
		tops = append(tops, topItemsToRows(stats.Day, p+"_page", zsetToTopItems(cmds.page.Val()))...)
		tops = append(tops, topItemsToRows(stats.Day, p+"_play", takePlayTops(zsetToTopItems(cmds.play.Val()), accessTopKeep))...)
		tops = append(tops, topItemsToRows(stats.Day, p+"_search", zsetToTopItems(cmds.search.Val()))...)
		tops = append(tops, topItemsToRows(stats.Day, p+"_classify", takeClassifyTops(zsetToTopItems(cmds.classify.Val()), accessTopKeep))...)
	}
	tops = append(tops, topItemsToRows(stats.Day, "tvbox_play", takePlayTops(zsetToTopItems(tvboxPlayCmd.Val()), accessTopKeep))...)
	tops = append(tops, topItemsToRows(stats.Day, "tvbox_search", zsetToTopItems(tvboxSearchCmd.Val()))...)
	tops = append(tops, topItemsToRows(stats.Day, "tvbox_classify", takeClassifyTops(zsetToTopItems(tvboxClassifyCmd.Val()), accessTopKeep))...)
	tops = append(tops, topItemsToRows(stats.Day, "classify", takeClassifyTops(zsetToTopItems(classifyCmd.Val()), accessTopKeep))...)

	has := stats.PV > 0 || stats.UV > 0 || stats.WebPV > 0 || stats.AppPV > 0 ||
		stats.ProvidePV > 0 || stats.Err4 > 0 || stats.Err5 > 0 || stats.Dropped > 0 ||
		len(clientVals) > 0 || len(actionVals) > 0 || len(tops) > 0 || len(allVersions) > 0
	return stats, tops, has, nil
}

func filterNumericClassifyTops(items []TopItem) []TopItem {
	valid := make([]TopItem, 0, len(items))
	for _, it := range items {
		if id, ok := parseFilmID(it.Key); ok {
			it.Key = strconv.FormatInt(id, 10)
			valid = append(valid, it)
		}
	}
	return valid
}

func zsetToTopItems(pairs []redis.Z) []TopItem {
	items := make([]TopItem, 0, len(pairs))
	for _, p := range pairs {
		member, _ := p.Member.(string)
		if member == "" {
			continue
		}
		items = append(items, TopItem{Key: member, Count: int64(p.Score)})
	}
	return items
}

func topItemsToRows(day, kind string, items []TopItem) []model.AccessDailyTop {
	rows := make([]model.AccessDailyTop, 0, len(items))
	for i, it := range items {
		if i >= accessTopKeep {
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

func persistDaily(stats model.AccessDailyStats, tops []model.AccessDailyTop) error {
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
		return tx.Create(&tops).Error
	})
}

func loadDailyStats(day string) (model.AccessDailyStats, bool) {
	var row model.AccessDailyStats
	if db.Mdb == nil {
		return row, false
	}
	err := db.Mdb.Where("day = ?", day).First(&row).Error
	if err != nil {
		return row, false
	}
	return row, true
}

// HasPersistedData 检查数据库中是否存在历史分析落库数据及总行数
func HasPersistedData() (bool, int64) {
	if db.Mdb == nil {
		return false, 0
	}
	var count int64
	if err := db.Mdb.Model(&model.AccessDailyStats{}).Count(&count).Error; err != nil {
		return false, 0
	}
	if count > 0 {
		return true, count
	}
	var topCount int64
	if err := db.Mdb.Model(&model.AccessDailyTop{}).Limit(1).Count(&topCount).Error; err == nil && topCount > 0 {
		return true, topCount
	}
	return false, 0
}

func loadDailyTops(day, kind string, limit int) []TopItem {
	if db.Mdb == nil {
		return nil
	}
	if limit <= 0 {
		limit = accessTopKeep
	}
	var rows []model.AccessDailyTop
	if err := db.Mdb.Where("day = ? AND kind = ?", day, kind).
		Order("rank ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil
	}
	items := make([]TopItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, TopItem{
			Key:      r.ItemKey,
			Count:    r.Count,
			Title:    r.Title,
			Category: r.Category,
			Poster:   r.Poster,
			Year:     r.Year,
		})
	}
	return items
}

func overviewFromDaily(row model.AccessDailyStats) *Overview {
	return overviewFromDailyScope(row, "", "")
}

func overviewFromDailyScope(row model.AccessDailyStats, module, platform string) *Overview {
	module = strings.ToLower(strings.TrimSpace(module))
	platform = strings.ToLower(strings.TrimSpace(platform))
	out := &Overview{
		Day:     row.Day,
		PV:      row.PV,
		UV:      row.UV,
		Err4:    row.Err4,
		Err5:    row.Err5,
		P95Ms:   row.P95Ms,
		Dropped: row.Dropped,
		Provide: ProvideStats{
			PV:   row.ProvidePV,
			Err4: row.ProvideErr4,
			Err5: row.ProvideErr5,
		},
		Client:    unmarshalIntMap(row.ClientJSON),
		Action:    unmarshalIntMap(row.ActionJSON),
		Hist:      unmarshalIntMap(row.HistJSON),
		Series:    unmarshalSeries(row.SeriesJSON),
		Platforms: unmarshalIntMap(row.PlatformJSON),
		Versions:  versionsFromDaily(row.VersionJSON, platform),
		Browsers:  unmarshalIntMap(row.BrowserJSON),
		OS:        unmarshalIntMap(row.OSJSON),
		Models:    unmarshalIntMap(row.ModelsJSON),
	}
	if module == "web" {
		out.PV = row.WebPV
		out.UV = row.WebUV
		for i := range out.Series {
			out.Series[i].PV = out.Series[i].WebPV
		}
	} else if module == "app" {
		if platform != "" && platform != "all" {
			platMap := unmarshalIntMap(row.PlatformJSON)
			out.PV = platMap[platform]
			out.UV = unmarshalIntMap(row.PlatformUVJSON)[platform]
			for i := range out.Series {
				switch platform {
				case "android":
					out.Series[i].PV = out.Series[i].AndroidPV
				case "harmony":
					out.Series[i].PV = out.Series[i].HarmonyPV
				case "ios":
					out.Series[i].PV = out.Series[i].IosPV
				default:
					out.Series[i].PV = out.Series[i].AppPV
				}
			}
		} else {
			out.PV = row.AppPV
			out.UV = row.AppUV
			for i := range out.Series {
				out.Series[i].PV = out.Series[i].AppPV
			}
		}
	} else if module == "tvbox" {
		out.PV = row.ProvidePV
		out.UV = row.ProvideUV
		for i := range out.Series {
			out.Series[i].PV = out.Series[i].ProvidePV
		}
	}
	return out
}
