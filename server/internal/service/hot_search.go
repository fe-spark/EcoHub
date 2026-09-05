package service

import (
	"encoding/json"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	filmrepo "server/internal/repository/film"
)

const (
	hotSearchDefaultLimit = 8
	hotSearchMaxLimit     = 20
)

var hotKeywordsSfGroup singleflight.Group

func clampHotSearchLimit(limit int) int {
	if limit <= 0 {
		return hotSearchDefaultLimit
	}
	if limit > hotSearchMaxLimit {
		return hotSearchMaxLimit
	}
	return limit
}

func sliceHotKeywords(keywords []string, limit int) []string {
	if len(keywords) == 0 {
		return []string{}
	}
	if len(keywords) > limit {
		res := make([]string, limit)
		copy(res, keywords[:limit])
		return res
	}
	res := make([]string, len(keywords))
	copy(res, keywords)
	return res
}

// SearchFilmInfo 获取关键字匹配的影片信息（默认相关度优先）
func (i *IndexService) SearchFilmInfo(key string, page *dto.Page) []model.MovieBasicInfo {
	return i.SearchFilmInfoWithSort(key, "", page)
}

// SearchFilmInfoWithSort 获取关键字匹配的影片信息（支持相关度/热度/最新/评分排序）
func (i *IndexService) SearchFilmInfoWithSort(key string, sortField string, page *dto.Page) []model.MovieBasicInfo {
	trimmed := strings.TrimSpace(key)
	if page == nil {
		page = &dto.Page{Current: 1, PageSize: 12}
	}
	version := filmrepo.GetActiveReadModelVersion()
	sl := filmrepo.SearchSnapshotsByKeywordAndSortFast(version, trimmed, sortField, page)
	return filmrepo.BuildMovieBasicInfosFromSnapshots(sl...)
}

// GetHotSearchKeywords 全站热门推荐：按当前快照播放热度取片名，与个人搜索历史无关。
func (i *IndexService) GetHotSearchKeywords(limit int) []string {
	limit = clampHotSearchLimit(limit)

	version := filmrepo.GetActiveReadModelVersion()
	if version == "" {
		version = filmrepo.GetActiveSnapshotVersion()
	}
	if version == "" {
		return []string{}
	}

	cacheKey := fmt.Sprintf("EcoHub:hotKeywords:v%s", version)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []string
			if json.Unmarshal([]byte(data), &cached) == nil {
				return sliceHotKeywords(cached, limit)
			}
		}
	}

	val, err, _ := hotKeywordsSfGroup.Do(cacheKey, func() (any, error) {
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				var cached []string
				if json.Unmarshal([]byte(data), &cached) == nil {
					return cached, nil
				}
			}
		}

		if db.Mdb == nil {
			return []string{}, nil
		}

		var snapshots []model.FilmListSnapshot
		query := db.Mdb.Model(&model.FilmListSnapshot{}).
			Select("name").
			Where("snapshot_version = ? AND pid > 0", version).
			Order("hits DESC").
			Limit(hotSearchMaxLimit * 3)
		if err := query.Find(&snapshots).Error; err != nil {
			return []string{}, err
		}

		topKeywords := make([]string, 0, hotSearchMaxLimit)
		seen := make(map[string]struct{}, hotSearchMaxLimit)
		for _, snap := range snapshots {
			name := strings.TrimSpace(snap.Name)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			topKeywords = append(topKeywords, name)
			if len(topKeywords) >= hotSearchMaxLimit {
				break
			}
		}

		if db.Rdb != nil {
			ttl := 30 * time.Minute
			if len(topKeywords) == 0 {
				ttl = 60 * time.Second
			}
			if raw, err := json.Marshal(topKeywords); err == nil {
				_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), ttl).Err()
			}
		}
		return topKeywords, nil
	})

	if err != nil || val == nil {
		return []string{}
	}
	keywords, ok := val.([]string)
	if !ok {
		return []string{}
	}
	return sliceHotKeywords(keywords, limit)
}
