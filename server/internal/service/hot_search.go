package service

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	filmrepo "server/internal/repository/film"
)

const (
	hotSearchDefaultLimit = 8
	hotSearchMaxLimit     = 20
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
	result := make([]string, 0, limit)
	seen := make(map[string]struct{}, limit)

	if db.Mdb == nil {
		return result
	}

	version := filmrepo.GetActiveReadModelVersion()
	if version == "" {
		version = filmrepo.GetActiveSnapshotVersion()
	}
	if version == "" {
		return result
	}

	var snapshots []model.FilmListSnapshot
	query := db.Mdb.Model(&model.FilmListSnapshot{}).
		Select("name").
		Where("snapshot_version = ? AND pid > 0", version).
		Order("hits DESC, id DESC").
		Limit(limit * 3)
	if err := query.Find(&snapshots).Error; err != nil {
		return result
	}

	for _, snap := range snapshots {
		name := strings.TrimSpace(snap.Name)
		if name == "" {
			continue
		}
		if _, ok := seen[name]; ok {
			continue
		}
		seen[name] = struct{}{}
		result = append(result, name)
		if len(result) >= limit {
			break
		}
	}

	return result
}
