package service

import (
	"strings"
	"time"
	"unicode/utf8"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	filmrepo "server/internal/repository/film"
)

const (
	hotSearchZSetKey         = "EcoHub:search:hot_keywords"
	hotSearchDefaultLimit    = 8
	hotSearchMaxLimit        = 20
	hotSearchKeywordMaxRunes = 64
	hotSearchZSetKeep        = 200
	hotSearchTTL             = 7 * 24 * time.Hour
)

func clampHotSearchLimit(limit int) int {
	if limit <= 0 {
		return hotSearchDefaultLimit
	}
	if limit > hotSearchMaxLimit {
		return hotSearchMaxLimit
	}
	return limit
}

func truncateHotSearchKeyword(s string) string {
	s = strings.TrimSpace(s)
	if s == "" {
		return ""
	}
	if utf8.RuneCountInString(s) <= hotSearchKeywordMaxRunes {
		return s
	}
	runes := []rune(s)
	return string(runes[:hotSearchKeywordMaxRunes])
}

func shouldTrackHotSearch(page *dto.Page, keyword string) bool {
	if strings.TrimSpace(keyword) == "" || page == nil {
		return false
	}
	return page.Current <= 1 && page.Total > 0
}

func trackHotSearchKeyword(keyword string) {
	if db.Rdb == nil {
		return
	}
	member := truncateHotSearchKeyword(keyword)
	if member == "" {
		return
	}
	pipe := db.Rdb.Pipeline()
	pipe.ZIncrBy(db.Cxt, hotSearchZSetKey, 1, member)
	pipe.ZRemRangeByRank(db.Cxt, hotSearchZSetKey, 0, int64(-(hotSearchZSetKeep + 1)))
	pipe.Expire(db.Cxt, hotSearchZSetKey, hotSearchTTL)
	_, _ = pipe.Exec(db.Cxt)
}

// SearchFilmInfo 获取关键字匹配的影片信息。
// 热搜只在第一页且检索命中后加分，避免翻页/空结果污染榜单。
func (i *IndexService) SearchFilmInfo(key string, page *dto.Page) []model.MovieBasicInfo {
	trimmed := strings.TrimSpace(key)
	if page == nil {
		page = &dto.Page{Current: 1, PageSize: 10}
	}
	version := filmrepo.GetActiveReadModelVersion()
	sl := filmrepo.SearchSnapshotsByKeywordFast(version, trimmed, page)
	if shouldTrackHotSearch(page, trimmed) {
		trackHotSearchKeyword(trimmed)
	}
	return filmrepo.BuildMovieBasicInfosFromSnapshots(sl...)
}

// GetHotSearchKeywords 获取全站热门搜索关键词（优先读 Redis 真实热搜榜，不足时由当前快照最高热度影片名补齐）
func (i *IndexService) GetHotSearchKeywords(limit int) []string {
	limit = clampHotSearchLimit(limit)
	result := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)

	if db.Rdb != nil {
		if keys, err := db.Rdb.ZRevRange(db.Cxt, hotSearchZSetKey, 0, int64(limit-1)).Result(); err == nil {
			for _, k := range keys {
				trimmed := strings.TrimSpace(k)
				if trimmed != "" {
					if _, ok := seen[trimmed]; !ok {
						seen[trimmed] = struct{}{}
						result = append(result, trimmed)
					}
				}
			}
		}
	}

	if len(result) < limit {
		version := filmrepo.GetActiveReadModelVersion()
		if version == "" {
			version = filmrepo.GetActiveSnapshotVersion()
		}
		var snapshots []model.FilmListSnapshot
		query := db.Mdb.Model(&model.FilmListSnapshot{}).
			Select("name").
			Where("snapshot_version = ? AND pid > 0", version).
			Order("hits DESC, id DESC").
			Limit(limit * 2)
		if err := query.Find(&snapshots).Error; err == nil {
			for _, snap := range snapshots {
				name := strings.TrimSpace(snap.Name)
				if name != "" {
					if _, ok := seen[name]; !ok {
						seen[name] = struct{}{}
						result = append(result, name)
						if len(result) >= limit {
							break
						}
					}
				}
			}
		}
	}

	return result
}
