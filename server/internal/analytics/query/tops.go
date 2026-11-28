package query

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"time"

	"server/internal/analytics/config"
	"server/internal/analytics/enrich"
	"server/internal/analytics/rollup"
	"server/internal/analytics/store"
	"server/internal/analytics/types"
	"server/internal/infra/db"
	"server/internal/model"

	"github.com/redis/go-redis/v9"
)

// QueryTopsScope 查询指定维度的前 N 条榜单（用于仪表盘直接展示）
func QueryTopsScope(day, kind, module, platform string, limit int) ([]types.TopItem, error) {
	if limit <= 0 {
		limit = enrich.TopKeepDefault
	}
	if limit > store.ZSetKeepLimit {
		limit = store.ZSetKeepLimit
	}
	res, err := QueryTopsPage(day, kind, module, platform, "", 1, limit)
	if err != nil {
		return nil, err
	}
	return res.Items, nil
}

// QueryTopsPage 查询支持关键字过滤与分页的榜单列表
func QueryTopsPage(day, kind, module, platform, q string, page, pageSize int) (types.TopPageResult, error) {
	kind = strings.ToLower(strings.TrimSpace(kind))
	if kind == "" {
		kind = "play"
	}
	if page < 1 {
		page = 1
	}
	if pageSize <= 0 {
		pageSize = 20
	}
	if pageSize > 200 {
		pageSize = 200
	}

	now := time.Now().In(time.Local)
	target, err := store.ParseDay(day, now)
	if err != nil {
		return types.TopPageResult{}, err
	}
	module = normalizeModule(module)
	platform = normalizePlatform(platform)

	var (
		allItems []types.TopItem
		cappedAt int
	)

	if !store.IsLocalToday(target, now) {
		dayStr := target.Format("2006-01-02")
		if _, ok, err := rollup.LoadDailyStats(dayStr); err != nil {
			return types.TopPageResult{}, err
		} else if ok {
			cappedAt = enrich.TopPersistLimit
			allItems, err = loadPersistedTops(dayStr, kind, module, platform, enrich.TopPersistLimit)
			if err != nil {
				return types.TopPageResult{}, err
			}
			return paginateTopItems(kind, allItems, q, page, pageSize, cappedAt), nil
		}
		if db.Rdb == nil {
			return types.TopPageResult{Kind: kind, Items: []types.TopItem{}, CappedAt: enrich.TopPersistLimit}, nil
		}
	}

	if db.Rdb == nil {
		if !config.IsEnabled() {
			return types.TopPageResult{Kind: kind, Items: []types.TopItem{}, CappedAt: store.ZSetKeepLimit}, nil
		}
		return types.TopPageResult{}, fmt.Errorf("redis unavailable")
	}

	cappedAt = store.ZSetKeepLimit
	dayKey := target.Format("20060102")
	fetch := pageSize * page
	if strings.TrimSpace(q) != "" || fetch > 500 {
		fetch = store.ZSetKeepLimit
	}
	if kind == "play" || kind == "classify" {
		if f := enrich.PlayTopFetchCount(fetch); f > fetch {
			fetch = f
		}
	}

	key := topZSetKey(kind, module, platform, dayKey)
	pairs, err := db.Rdb.ZRevRangeWithScores(db.Cxt, key, 0, int64(fetch-1)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return types.TopPageResult{}, err
	}
	allItems = make([]types.TopItem, 0, len(pairs))
	for _, p := range pairs {
		member, _ := p.Member.(string)
		if member != "" {
			allItems = append(allItems, types.TopItem{Key: member, Count: int64(p.Score)})
		}
	}
	switch kind {
	case "play":
		allItems, err = enrich.TakePlayTops(allItems, fetch)
	case "classify":
		allItems, err = enrich.TakeClassifyTops(allItems, fetch)
	}
	if err != nil {
		return types.TopPageResult{}, err
	}

	return paginateTopItems(kind, allItems, q, page, pageSize, cappedAt), nil
}

func loadPersistedTops(dayStr, kind, module, platform string, limit int) ([]types.TopItem, error) {
	queryKind := scopedTopKind(kind, module, platform)
	items, err := loadDailyTopRows(dayStr, queryKind, limit)
	if err != nil {
		return nil, err
	}
	if len(items) == 0 && queryKind == "page" {
		webItems, err := loadDailyTopRows(dayStr, "web_page", limit)
		if err != nil {
			return nil, err
		}
		appItems, err := loadDailyTopRows(dayStr, "app_page", limit)
		if err != nil {
			return nil, err
		}
		items = mergeTopItemsByCount(webItems, appItems)
	}
	switch kind {
	case "play":
		return enrich.TakePlayTops(items, limit)
	case "classify":
		return enrich.TakeClassifyTops(items, limit)
	default:
		return enrich.LimitTopItems(items, limit), nil
	}
}

func loadDailyTopRows(day, kind string, limit int) ([]types.TopItem, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	var rows []model.AccessDailyTop
	if err := db.Mdb.Where("day = ? AND kind = ?", day, kind).
		Order("rank ASC").Limit(limit).Find(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]types.TopItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, types.TopItem{
			Key:      r.ItemKey,
			Count:    r.Count,
			Title:    r.Title,
			Category: r.Category,
			Poster:   r.Poster,
			Year:     r.Year,
		})
	}
	return items, nil
}

func scopedTopKind(kind, module, platform string) string {
	if kind == "path" {
		kind = "page"
	}
	switch kind {
	case "page", "play", "search", "classify":
		switch module {
		case types.ModuleWeb:
			return "web_" + kind
		case types.ModuleTvbox:
			return "tvbox_" + kind
		case types.ModuleApp:
			if platform == "android" || platform == "harmony" || platform == "ios" {
				return platform + "_" + kind
			}
			return "app_" + kind
		default:
			return kind
		}
	default:
		return kind
	}
}

func topZSetKey(kind, module, platform, dayKey string) string {
	switch kind {
	case "search":
		switch module {
		case types.ModuleWeb:
			return store.WebTopSearchKey(dayKey)
		case types.ModuleTvbox:
			return store.TvboxTopSearchKey(dayKey)
		case types.ModuleApp:
			if platform != "" && platform != "all" {
				return store.AppTopSearchKey(platform, dayKey)
			}
			return store.AppAllTopSearchKey(dayKey)
		default:
			return store.TopSearchKey(dayKey)
		}
	case "play":
		switch module {
		case types.ModuleWeb:
			return store.WebTopPlayKey(dayKey)
		case types.ModuleTvbox:
			return store.TvboxTopPlayKey(dayKey)
		case types.ModuleApp:
			if platform != "" && platform != "all" {
				return store.AppTopPlayKey(platform, dayKey)
			}
			return store.AppAllTopPlayKey(dayKey)
		default:
			return store.TopPlayKey(dayKey)
		}
	case "classify":
		switch module {
		case types.ModuleWeb:
			return store.WebTopClassifyKey(dayKey)
		case types.ModuleTvbox:
			return store.TvboxTopClassifyKey(dayKey)
		case types.ModuleApp:
			if platform != "" && platform != "all" {
				return store.AppTopClassifyKey(platform, dayKey)
			}
			return store.AppAllTopClassifyKey(dayKey)
		default:
			return store.TopClassifyKey(dayKey)
		}
	default:
		switch module {
		case types.ModuleWeb:
			return store.WebTopPageKey(dayKey)
		case types.ModuleApp:
			if platform != "" && platform != "all" {
				return store.AppTopPageKey(platform, dayKey)
			}
			return store.AppAllTopPageKey(dayKey)
		default:
			return store.TopPathKey(dayKey)
		}
	}
}

func paginateTopItems(kind string, items []types.TopItem, q string, page, pageSize, cappedAt int) types.TopPageResult {
	q = strings.ToLower(strings.TrimSpace(q))
	filtered := items
	if q != "" {
		filtered = make([]types.TopItem, 0, len(items))
		for _, it := range items {
			if strings.Contains(strings.ToLower(it.Key), q) ||
				strings.Contains(strings.ToLower(it.Title), q) ||
				strings.Contains(strings.ToLower(it.Category), q) {
				filtered = append(filtered, it)
			}
		}
	}
	total := len(filtered)
	start := (page - 1) * pageSize
	if start >= total {
		return types.TopPageResult{Kind: kind, Items: []types.TopItem{}, Total: total, CappedAt: cappedAt}
	}
	end := start + pageSize
	if end > total {
		end = total
	}
	return types.TopPageResult{
		Kind:     kind,
		Items:    filtered[start:end],
		Total:    total,
		CappedAt: cappedAt,
	}
}

func mergeTopItemsByCount(parts ...[]types.TopItem) []types.TopItem {
	counts := make(map[string]int64, 16)
	meta := make(map[string]types.TopItem, 16)
	for _, items := range parts {
		for _, it := range items {
			if it.Key == "" {
				continue
			}
			counts[it.Key] += it.Count
			if _, ok := meta[it.Key]; !ok {
				meta[it.Key] = it
			}
		}
	}
	out := make([]types.TopItem, 0, len(counts))
	for k, n := range counts {
		it := meta[k]
		it.Count = n
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Key < out[j].Key
		}
		return out[i].Count > out[j].Count
	})
	return out
}
