package access

import (
	"errors"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

type filmMetaCacheItem struct {
	Title    string
	Category string
	Poster   string
	Year     int64
	CachedAt time.Time
}

var (
	filmMetaCacheMu sync.RWMutex
	filmMetaCache   = map[int64]filmMetaCacheItem{}
)

const filmMetaCacheTTL = 1 * time.Minute

// resolveFilmMetas 按 mid 查 film_index。查询失败返回错误，不写占位标题。
func resolveFilmMetas(filmIDs []int64) (map[int64]filmMetaCacheItem, error) {
	if len(filmIDs) == 0 {
		return map[int64]filmMetaCacheItem{}, nil
	}

	result := make(map[int64]filmMetaCacheItem, len(filmIDs))
	missing := make([]int64, 0, len(filmIDs))
	now := time.Now()

	filmMetaCacheMu.RLock()
	for _, id := range filmIDs {
		if item, ok := filmMetaCache[id]; ok && now.Sub(item.CachedAt) < filmMetaCacheTTL {
			result[id] = item
		} else {
			missing = append(missing, id)
		}
	}
	filmMetaCacheMu.RUnlock()

	if len(missing) == 0 {
		return result, nil
	}
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}

	var rows []model.FilmIndex
	if err := db.Mdb.Model(&model.FilmIndex{}).
		Select("mid, name, c_name, picture, year").
		Where("mid IN ?", missing).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	foundMap := make(map[int64]filmMetaCacheItem, len(rows))
	for _, row := range rows {
		foundMap[row.Mid] = filmMetaCacheItem{
			Title:    row.Name,
			Category: row.CName,
			Poster:   row.Picture,
			Year:     row.Year,
			CachedAt: now,
		}
	}

	filmMetaCacheMu.Lock()
	defer filmMetaCacheMu.Unlock()

	for _, id := range missing {
		item, ok := foundMap[id]
		if !ok {
			continue
		}
		filmMetaCache[id] = item
		result[id] = item
	}

	// 限制缓存总容量防泄漏
	if len(filmMetaCache) > 5000 {
		cutoff := now.Add(-filmMetaCacheTTL)
		for k, v := range filmMetaCache {
			if v.CachedAt.Before(cutoff) {
				delete(filmMetaCache, k)
			}
		}
		// 若过期清理后依然超出上限（突发高频并发请求场景），硬顶重置防无界内存泄漏
		if len(filmMetaCache) > 5000 {
			filmMetaCache = make(map[int64]filmMetaCacheItem, 2000)
		}
	}

	return result, nil
}

// enrichPlayTopItems 为热播榜填补真实片名、海报与分类，并过滤非合法数字 ID 的脏数据
func enrichPlayTopItems(items []TopItem) ([]TopItem, error) {
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		// 若已有非占位片名（如历史归档数据），保留快照无需反查
		if it.Title != "" && !strings.HasPrefix(it.Title, "影片 #") {
			continue
		}
		if id, ok := parseFilmID(it.Key); ok {
			ids = append(ids, id)
		}
	}

	metaMap, err := resolveFilmMetas(ids)
	if err != nil {
		return nil, err
	}
	validItems := make([]TopItem, 0, len(items))
	for _, it := range items {
		id, ok := parseFilmID(it.Key)
		if !ok {
			continue
		}
		it.Key = strconv.FormatInt(id, 10)
		if it.Title == "" || strings.HasPrefix(it.Title, "影片 #") {
			if meta, ok := metaMap[id]; ok {
				it.Title = meta.Title
				it.Category = meta.Category
				it.Poster = meta.Poster
				it.Year = meta.Year
			}
		}
		if it.Title == "" {
			continue
		}
		validItems = append(validItems, it)
	}
	return validItems, nil
}

func playTopFetchCount(limit int) int {
	if limit <= 0 {
		limit = accessTopKeep
	}
	fetch := limit * 3
	if fetch > zsetKeep {
		fetch = zsetKeep
	}
	return fetch
}

func limitTopItems(items []TopItem, limit int) []TopItem {
	if limit <= 0 || len(items) <= limit {
		return items
	}
	return items[:limit]
}

func takePlayTops(items []TopItem, limit int) ([]TopItem, error) {
	enriched, err := enrichPlayTopItems(items)
	if err != nil {
		return nil, err
	}
	return limitTopItems(enriched, limit), nil
}

var (
	catNameCacheMu   sync.RWMutex
	catNameCache     = map[int64]string{}
	catIdByNameCache = map[string]int64{}
	catNameCacheAt   time.Time
)

func resolveCategoryIDByName(name string) (int64, bool) {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0, false
	}
	catNameCacheMu.RLock()
	if time.Since(catNameCacheAt) < 5*time.Minute {
		if id, ok := catIdByNameCache[name]; ok && id > 0 {
			catNameCacheMu.RUnlock()
			return id, true
		}
	}
	catNameCacheMu.RUnlock()

	if db.Mdb == nil {
		return 0, false
	}

	var cat model.Category
	if err := db.Mdb.Model(&model.Category{}).
		Select("id, name").
		Where("name = ?", name).
		First(&cat).Error; err == nil && cat.Id > 0 {
		catNameCacheMu.Lock()
		catNameCache[cat.Id] = cat.Name
		catIdByNameCache[cat.Name] = cat.Id
		catNameCacheMu.Unlock()
		return cat.Id, true
	}
	return 0, false
}

func resolveCategoryNames(ids []int64) map[int64]string {
	if len(ids) == 0 {
		return map[int64]string{}
	}
	result := make(map[int64]string, len(ids))
	missing := make([]int64, 0, len(ids))

	catNameCacheMu.RLock()
	if time.Since(catNameCacheAt) < 5*time.Minute {
		for _, id := range ids {
			if name, ok := catNameCache[id]; ok {
				result[id] = name
			} else {
				missing = append(missing, id)
			}
		}
	} else {
		missing = ids
	}
	catNameCacheMu.RUnlock()

	if len(missing) == 0 || db.Mdb == nil {
		return result
	}

	var cats []model.Category
	if err := db.Mdb.Model(&model.Category{}).
		Select("id, name").
		Where("id IN ?", missing).
		Find(&cats).Error; err == nil {
		catNameCacheMu.Lock()
		if time.Since(catNameCacheAt) >= 5*time.Minute {
			catNameCache = map[int64]string{}
			catIdByNameCache = map[string]int64{}
			catNameCacheAt = time.Now()
		}
		for _, c := range cats {
			catNameCache[c.Id] = c.Name
			catIdByNameCache[c.Name] = c.Id
			result[c.Id] = c.Name
		}
		// 缓存占位防穿透：库中未查到的 ID 记录占位符，防止无效/已删除 ID 高频打库
		for _, id := range missing {
			if _, ok := catNameCache[id]; !ok {
				placeholder := fmt.Sprintf("分类 #%d", id)
				catNameCache[id] = placeholder
				result[id] = placeholder
			}
		}
		catNameCacheMu.Unlock()
	}

	return result
}

// enrichClassifyTopItems 为分类榜填补真实分类名称，并过滤噪声数据，同时兼容纯数字 ID 与真实分类名称
func enrichClassifyTopItems(items []TopItem) []TopItem {
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		if it.Category != "" && !strings.HasPrefix(it.Category, "分类 #") {
			continue
		}
		if id, ok := parseFilmID(it.Key); ok {
			ids = append(ids, id)
		}
	}

	catMap := resolveCategoryNames(ids)
	validItems := make([]TopItem, 0, len(items))
	for _, it := range items {
		trimmedKey := strings.TrimSpace(it.Key)
		if trimmedKey == "" || trimmedKey == "list" || trimmedKey == "config" {
			// 直接丢弃历史遗留的 "list", "config" 等无意义噪声
			continue
		}

		id, ok := parseFilmID(trimmedKey)
		if ok {
			it.Key = strconv.FormatInt(id, 10)
			if it.Category == "" || strings.HasPrefix(it.Category, "分类 #") {
				if name, ok := catMap[id]; ok && name != "" {
					it.Category = name
				}
			}
			if it.Title == "" || strings.HasPrefix(it.Title, "分类 #") {
				if it.Category != "" {
					it.Title = it.Category
				} else {
					it.Title = fmt.Sprintf("分类 #%d", id)
				}
			}
		} else {
			// 非数字 member（例如分类名 "伦理片"、"电影"）
			// 尝试反查其分类 ID（若在 Category 表中存在则标准化绑定）
			if catId, found := resolveCategoryIDByName(trimmedKey); found {
				it.Key = strconv.FormatInt(catId, 10)
				it.Category = trimmedKey
				it.Title = trimmedKey
			} else {
				// 若不是已知 ID，但为非噪声分类名，予以保留并正常展示
				it.Key = trimmedKey
				if it.Category == "" {
					it.Category = trimmedKey
				}
				if it.Title == "" {
					it.Title = trimmedKey
				}
			}
		}
		validItems = append(validItems, it)
	}
	return validItems
}

func mergeClassifyItems(items []TopItem) []TopItem {
	if len(items) <= 1 {
		return items
	}
	merged := make([]TopItem, 0, len(items))
	idxMap := make(map[string]int, len(items))
	for _, it := range items {
		lookupKey := it.Key
		if it.Title != "" {
			lookupKey = it.Title
		}
		if idx, exists := idxMap[lookupKey]; exists {
			merged[idx].Count += it.Count
		} else {
			idxMap[lookupKey] = len(merged)
			merged = append(merged, it)
		}
	}
	sort.SliceStable(merged, func(i, j int) bool {
		return merged[i].Count > merged[j].Count
	})
	return merged
}

func takeClassifyTops(items []TopItem, limit int) []TopItem {
	return limitTopItems(mergeClassifyItems(enrichClassifyTopItems(items)), limit)
}
