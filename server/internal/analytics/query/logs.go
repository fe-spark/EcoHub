package query

import (
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"server/internal/analytics/config"
	"server/internal/analytics/enrich"
	"server/internal/analytics/store"
	"server/internal/analytics/types"
	appcfg "server/internal/config"
	"server/internal/infra/db"

	"github.com/redis/go-redis/v9"
)

// LogsQuery 访问流水查询条件
type LogsQuery struct {
	Day      string
	Module   string
	Platform string
	Action   string
	Q        string
	Page     int
	PageSize int
}

// QueryLogsPage 查询分端访问流水（支持行为筛选、关键字过滤与分页）
func QueryLogsPage(params LogsQuery) (types.LogPageResult, error) {
	if db.Rdb == nil {
		if !config.IsEnabled() {
			return types.LogPageResult{List: []types.Event{}, Total: 0}, nil
		}
		return types.LogPageResult{}, fmt.Errorf("redis unavailable")
	}
	now := time.Now().In(time.Local)
	target, err := store.ParseDay(params.Day, now)
	if err != nil {
		return types.LogPageResult{}, err
	}
	dayKey := target.Format("20060102")
	module := normalizeModule(params.Module)
	platform := normalizePlatform(params.Platform)
	actionFilter := strings.ToLower(strings.TrimSpace(params.Action))
	q := strings.ToLower(strings.TrimSpace(params.Q))

	fetch := int64(appcfg.AnalyticsRecentLimit)
	if fetch < 1 {
		fetch = store.RecentKeepLimit
	}
	raw, err := loadRecentLogLines(recentKeysForModule(module, dayKey), fetch)
	if err != nil {
		return types.LogPageResult{}, err
	}
	events := parseRecentEvents(raw)
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Ts.After(events[j].Ts)
	})

	targetY, targetM, targetD := target.Date()
	filtered := make([]types.Event, 0, len(events))
	for _, evt := range events {
		ey, em, ed := evt.Ts.In(time.Local).Date()
		if ey != targetY || em != targetM || ed != targetD {
			continue
		}
		if module == types.ModuleApp && platform != "" && platform != "all" && evt.ClientType != platform {
			continue
		}
		if actionFilter != "" && actionFilter != "all" && strings.ToLower(evt.Action) != actionFilter {
			continue
		}
		if q != "" && !matchLogKeyword(evt, q) {
			continue
		}
		filtered = append(filtered, evt)
	}

	total := len(filtered)
	page := params.Page
	pageSize := params.PageSize
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > store.RecentKeepLimit {
		pageSize = store.RecentKeepLimit
	}
	start := (page - 1) * pageSize
	if start >= total {
		return types.LogPageResult{List: []types.Event{}, Total: total}, nil
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	paged, err := enrich.EnrichLogEvents(filtered[start:end])
	if err != nil {
		return types.LogPageResult{}, err
	}
	return types.LogPageResult{List: paged, Total: total}, nil
}

func recentKeysForModule(module, dayKey string) []string {
	switch module {
	case types.ModuleWeb:
		return []string{store.WebRecentDayKey(dayKey)}
	case types.ModuleApp:
		return []string{store.AppRecentDayKey(dayKey)}
	case types.ModuleTvbox:
		return []string{store.TvboxRecentDayKey(dayKey)}
	default:
		return []string{
			store.WebRecentDayKey(dayKey),
			store.AppRecentDayKey(dayKey),
			store.TvboxRecentDayKey(dayKey),
		}
	}
}

func loadRecentLogLines(keys []string, fetch int64) ([]string, error) {
	out := make([]string, 0, int(fetch)*len(keys))
	for _, key := range keys {
		raw, err := db.Rdb.LRange(db.Cxt, key, 0, fetch-1).Result()
		if err != nil && !errors.Is(err, redis.Nil) {
			return nil, err
		}
		out = append(out, raw...)
	}
	return out, nil
}

func parseRecentEvents(raw []string) []types.Event {
	events := make([]types.Event, 0, len(raw))
	for _, line := range raw {
		var evt types.Event
		if json.Unmarshal([]byte(line), &evt) != nil {
			continue
		}
		events = append(events, evt)
	}
	return events
}

func matchLogKeyword(evt types.Event, q string) bool {
	return strings.Contains(strings.ToLower(evt.Path), q) ||
		strings.Contains(strings.ToLower(evt.Page), q) ||
		strings.Contains(strings.ToLower(evt.PageTitle), q) ||
		strings.Contains(strings.ToLower(evt.Resource), q) ||
		strings.Contains(strings.ToLower(evt.ResourceTitle), q) ||
		strings.Contains(strings.ToLower(evt.Query), q) ||
		strings.Contains(strings.ToLower(evt.DeviceId), q) ||
		strings.Contains(strings.ToLower(evt.DeviceModel), q) ||
		strings.Contains(strings.ToLower(evt.Action), q) ||
		strings.Contains(strings.ToLower(evt.IPPreview), q)
}
