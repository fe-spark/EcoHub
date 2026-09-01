package access

import (
	"encoding/json"
	"fmt"
	"strconv"
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
