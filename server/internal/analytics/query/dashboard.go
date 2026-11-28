package query

import (
	"time"

	"server/internal/analytics/enrich"
	"server/internal/analytics/store"
	"server/internal/analytics/types"
	"server/internal/infra/db"
	"server/internal/model"
)

// QueryDashboard 一次聚合返回前端看板所需的全量与分端指标、片源分布、榜单、首屏流水与可用日期
func QueryDashboard(day, module, platform, logAction, logQ string) (types.DashboardPayload, error) {
	now := time.Now().In(time.Local)
	target, err := store.ParseDay(day, now)
	if err != nil {
		return types.DashboardPayload{}, err
	}
	dayStr := target.Format("2006-01-02")
	normModule := normalizeModule(module)
	normPlatform := normalizePlatform(platform)

	globalOv, err := QueryOverviewScope(dayStr, types.ModuleAll, "all")
	if err != nil {
		return types.DashboardPayload{}, err
	}
	scopedOv := globalOv
	if normModule != types.ModuleAll {
		scopedOv, err = QueryOverviewScope(dayStr, normModule, normPlatform)
		if err != nil {
			return types.DashboardPayload{}, err
		}
	}

	sources, err := QuerySourceCalls(dayStr)
	if err != nil {
		return types.DashboardPayload{}, err
	}

	playTops, err := QueryTopsScope(dayStr, "play", normModule, normPlatform, enrich.TopKeepDefault)
	if err != nil {
		return types.DashboardPayload{}, err
	}
	searchTops, err := QueryTopsScope(dayStr, "search", normModule, normPlatform, enrich.TopKeepDefault)
	if err != nil {
		return types.DashboardPayload{}, err
	}
	classifyTops, err := QueryTopsScope(dayStr, "classify", normModule, normPlatform, enrich.TopKeepDefault)
	if err != nil {
		return types.DashboardPayload{}, err
	}
	var pageTops []types.TopItem
	if normModule != types.ModuleTvbox {
		pageTops, err = QueryTopsScope(dayStr, "page", normModule, normPlatform, enrich.TopKeepDefault)
		if err != nil {
			return types.DashboardPayload{}, err
		}
	} else {
		pageTops = []types.TopItem{}
	}

	logsPage, err := QueryLogsPage(LogsQuery{
		Day:      dayStr,
		Module:   normModule,
		Platform: normPlatform,
		Action:   logAction,
		Q:        logQ,
		Page:     1,
		PageSize: 10,
	})
	if err != nil {
		return types.DashboardPayload{}, err
	}

	days := ListAvailableDays(now)

	return types.DashboardPayload{
		Day:           dayStr,
		Module:        normModule,
		Platform:      normPlatform,
		Global:        globalOv,
		Scoped:        scopedOv,
		Sources:       sources,
		PlayTops:      playTops,
		SearchTops:    searchTops,
		ClassifyTops:  classifyTops,
		PageTops:      pageTops,
		Logs:          logsPage.List,
		LogsTotal:     logsPage.Total,
		AvailableDays: days,
	}, nil
}

// ListAvailableDays 返回有历史记录的日期倒序列表（包含今日）
func ListAvailableDays(now time.Time) []string {
	todayStr := store.StartOfLocalDay(now).Format("2006-01-02")
	seen := map[string]struct{}{todayStr: {}}
	out := []string{todayStr}
	if db.Mdb == nil {
		return out
	}
	var days []string
	if err := db.Mdb.Model(&model.AccessDailyStats{}).
		Order("day DESC").
		Limit(60).
		Pluck("day", &days).Error; err != nil {
		return out
	}
	for _, d := range days {
		if d == "" {
			continue
		}
		if _, ok := seen[d]; !ok {
			seen[d] = struct{}{}
			out = append(out, d)
		}
	}
	return out
}
