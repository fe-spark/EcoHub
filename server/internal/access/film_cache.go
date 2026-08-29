package access

import (
	"fmt"
	"strconv"
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

const filmMetaCacheTTL = 5 * time.Minute

type filmSimpleRow struct {
	Mid     int64  `gorm:"column:mid"`
	Name    string `gorm:"column:name"`
	CName   string `gorm:"column:c_name"`
	Picture string `gorm:"column:picture"`
	Year    int64  `gorm:"column:year"`
}

// resolveFilmMetas 批量反查影片片名、分类与海报（带内存短缓存）
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

	var rows []filmSimpleRow
	err := db.Mdb.Table(model.TableFilmIndex).
		Select("mid, name, c_name, picture, year").
		Where("mid IN ?", missing).
		Find(&rows).Error

	if err != nil {
		return result
	}

	foundMap := make(map[int64]filmSimpleRow, len(rows))
	for _, r := range rows {
		foundMap[r.Mid] = r
	}

	filmMetaCacheMu.Lock()
	defer filmMetaCacheMu.Unlock()

	for _, id := range missing {
		if row, ok := foundMap[id]; ok {
			item := filmMetaCacheItem{
				Title:    row.Name,
				Category: row.CName,
				Poster:   row.Picture,
				Year:     row.Year,
				CachedAt: now,
			}
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
