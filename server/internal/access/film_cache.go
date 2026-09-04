package access

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	filmrepo "server/internal/repository/film"
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

type filmSimpleRow struct {
	Mid     int64  `gorm:"column:mid"`
	Name    string `gorm:"column:name"`
	CName   string `gorm:"column:c_name"`
	Picture string `gorm:"column:picture"`
	Year    int64  `gorm:"column:year"`
}

func parseYearInt(s string) int64 {
	if len(s) >= 4 {
		s = s[:4]
	}
	v, _ := strconv.ParseInt(s, 10, 64)
	return v
}

// InvalidateFilmMetaCache 淘汰指定影片或全部热播元数据缓存
func InvalidateFilmMetaCache(mids ...int64) {
	filmMetaCacheMu.Lock()
	defer filmMetaCacheMu.Unlock()
	if len(mids) == 0 {
		filmMetaCache = map[int64]filmMetaCacheItem{}
		return
	}
	for _, id := range mids {
		delete(filmMetaCache, id)
	}
}

// resolveFilmMetas 批量反查影片片名、分类与海报（优先从活跃只读快照与详情反查最新海报，带内存短缓存）
func resolveFilmMetas(filmIDs []int64) map[int64]filmMetaCacheItem {
	if len(filmIDs) == 0 {
		return map[int64]filmMetaCacheItem{}
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

	if len(missing) == 0 || db.Mdb == nil {
		return result
	}

	foundMap := make(map[int64]filmMetaCacheItem, len(missing))
	unresolved := make([]int64, 0, len(missing))

	// 1. 优先从当前活跃快照表 FilmListSnapshot 中查询（包含自定义封面和最新海报源封面）
	activeVersion := filmrepo.GetActiveSnapshotVersion()
	if activeVersion != "" {
		var snapshots []model.FilmListSnapshot
		if err := db.Mdb.Model(&model.FilmListSnapshot{}).
			Select("mid, name, c_name, picture, year").
			Where("snapshot_version = ? AND mid IN ?", activeVersion, missing).
			Find(&snapshots).Error; err == nil {
			for _, s := range snapshots {
				foundMap[s.Mid] = filmMetaCacheItem{
					Title:    s.Name,
					Category: s.CName,
					Poster:   s.Picture,
					Year:     s.Year,
					CachedAt: now,
				}
			}
		}
	}

	for _, id := range missing {
		if _, ok := foundMap[id]; !ok {
			unresolved = append(unresolved, id)
		}
	}

	// 2. 若快照中未找到，从 movie_detail_info 中查（用户自定义主表）
	if len(unresolved) > 0 {
		var detailInfos []model.MovieDetailInfo
		if err := db.Mdb.Model(&model.MovieDetailInfo{}).
			Where("mid IN ?", unresolved).
			Find(&detailInfos).Error; err == nil {
			for _, info := range detailInfos {
				var d model.MovieDetail
				if err := json.Unmarshal([]byte(info.Content), &d); err == nil {
					foundMap[info.Mid] = filmMetaCacheItem{
						Title:    d.Name,
						Category: d.CName,
						Poster:   d.DisplayPicture(),
						Year:     parseYearInt(d.Year),
						CachedAt: now,
					}
				}
			}
		}
	}

	stillMissing := make([]int64, 0, len(unresolved))
	for _, id := range unresolved {
		if _, ok := foundMap[id]; !ok {
			stillMissing = append(stillMissing, id)
		}
	}

	// 3. 最终兜底从原始采集表 film_index 中查
	if len(stillMissing) > 0 {
		var rows []filmSimpleRow
		if err := db.Mdb.Table(model.TableFilmIndex).
			Select("mid, name, c_name, picture, year").
			Where("mid IN ?", stillMissing).
			Find(&rows).Error; err == nil {
			for _, r := range rows {
				foundMap[r.Mid] = filmMetaCacheItem{
					Title:    r.Name,
					Category: r.CName,
					Poster:   r.Picture,
					Year:     r.Year,
					CachedAt: now,
				}
			}
		}
	}

	filmMetaCacheMu.Lock()
	defer filmMetaCacheMu.Unlock()

	for _, id := range missing {
		if item, ok := foundMap[id]; ok {
			filmMetaCache[id] = item
			result[id] = item
		} else {
			// 未在库中找到（可能已被删除），缓存占位
			item := filmMetaCacheItem{
				Title:    fmt.Sprintf("影片 #%d", id),
				Category: "未知",
				Poster:   "",
				Year:     0,
				CachedAt: now,
			}
			filmMetaCache[id] = item
			result[id] = item
		}
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

	return result
}

// enrichPlayTopItems 为热播榜填补真实片名、海报与分类，并过滤非合法数字 ID 的脏数据
func enrichPlayTopItems(items []TopItem) []TopItem {
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		if id, ok := parseFilmID(it.Key); ok {
			ids = append(ids, id)
		}
	}

	metaMap := resolveFilmMetas(ids)
	validItems := make([]TopItem, 0, len(items))
	for _, it := range items {
		id, ok := parseFilmID(it.Key)
		if !ok {
			continue
		}
		it.Key = strconv.FormatInt(id, 10)
		if meta, ok := metaMap[id]; ok {
			it.Title = meta.Title
			it.Category = meta.Category
			it.Poster = meta.Poster
			it.Year = meta.Year
		}
		if it.Title == "" {
			it.Title = fmt.Sprintf("影片 #%d", id)
		}
		validItems = append(validItems, it)
	}
	return validItems
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

func takePlayTops(items []TopItem, limit int) []TopItem {
	return limitTopItems(enrichPlayTopItems(items), limit)
}

var (
	catNameCacheMu sync.RWMutex
	catNameCache   = map[int64]string{}
	catNameCacheAt time.Time
)

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
			catNameCacheAt = time.Now()
		}
		for _, c := range cats {
			catNameCache[c.Id] = c.Name
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

// enrichClassifyTopItems 为分类榜填补真实分类名称，并过滤非数字 ID 的脏数据（如历史遗留的 list/config 等）
func enrichClassifyTopItems(items []TopItem) []TopItem {
	ids := make([]int64, 0, len(items))
	for _, it := range items {
		if id, ok := parseFilmID(it.Key); ok {
			ids = append(ids, id)
		}
	}

	catMap := resolveCategoryNames(ids)
	validItems := make([]TopItem, 0, len(items))
	for _, it := range items {
		id, ok := parseFilmID(it.Key)
		if !ok {
			// 直接丢弃非数字 member（如历史遗留的 "list", "config" 等脏数据）
			continue
		}
		it.Key = strconv.FormatInt(id, 10)
		if name, ok := catMap[id]; ok && name != "" {
			it.Title = name
			it.Category = name
		}
		if it.Title == "" {
			it.Title = fmt.Sprintf("分类 #%d", id)
		}
		validItems = append(validItems, it)
	}
	return validItems
}

func takeClassifyTops(items []TopItem, limit int) []TopItem {
	return limitTopItems(enrichClassifyTopItems(items), limit)
}

func isTvboxPlay(path, query string) bool {
	if strings.HasPrefix(path, "/api/provide/vod") {
		return (strings.Contains(query, "ac=detail") || strings.Contains(query, "ac=videolist") || strings.Contains(query, "ids=")) && strings.Contains(query, "ids=")
	}
	return false
}

// enrichLogEvents 为访问流水记录按动作类型精准补齐详情信息
func enrichLogEvents(events []AccessEvent) []AccessEvent {
	if len(events) == 0 {
		return events
	}
	filmIDs := make([]int64, 0, len(events))
	catIDs := make([]int64, 0, len(events))

	for _, it := range events {
		res := strings.TrimSpace(it.Resource)
		if idx := strings.Index(res, ","); idx > 0 {
			res = strings.TrimSpace(res[:idx])
		}

		// 仅点播行为才提取影片 ID，分类筛选与搜索严禁当做影片处理
		if it.Action == ActionPlay || strings.HasPrefix(it.Path, "/api/filmPlayInfo") || isTvboxPlay(it.Path, it.Query) {
			if id, ok := parseFilmID(res); ok {
				filmIDs = append(filmIDs, id)
			}
		} else if it.Action == ActionClassify {
			if id, ok := parseFilmID(res); ok {
				catIDs = append(catIDs, id)
			}
		}
	}

	metaMap := resolveFilmMetas(filmIDs)
	catMap := resolveCategoryNames(catIDs)

	for i := range events {
		it := &events[i]
		res := strings.TrimSpace(it.Resource)
		if idx := strings.Index(res, ","); idx > 0 {
			res = strings.TrimSpace(res[:idx])
		}

		if it.Action == ActionPlay || strings.HasPrefix(it.Path, "/api/filmPlayInfo") || isTvboxPlay(it.Path, it.Query) {
			if id, ok := parseFilmID(res); ok {
				if meta, ok := metaMap[id]; ok {
					it.ResourceTitle = meta.Title
					it.ResourcePoster = meta.Poster
					it.ResourceCat = meta.Category
				}
			}
		} else if it.Action == ActionClassify {
			if id, ok := parseFilmID(res); ok {
				if name, ok := catMap[id]; ok && name != "" {
					it.ResourceCat = name
				}
			}
		}
	}
	return events
}
