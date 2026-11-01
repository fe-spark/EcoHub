package service

import (
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
	filmshared "server/internal/repository/film/shared"
	filmsnapshot "server/internal/repository/film/snapshot"
)

// SearchFilmResult 搜索接口业务结果。
type SearchFilmResult struct {
	List    []model.MovieBasicInfo
	Sources []model.SearchSourceTab
	Error   string
}

// SearchFilm 搜索锁定当前基准源；忽略访客传入的 sourceID。
func (i *IndexService) SearchFilm(keyword, _ string, sortField string, page *dto.Page) SearchFilmResult {
	keyword = strings.TrimSpace(keyword)
	if page == nil {
		page = &dto.Page{Current: 1, PageSize: 12}
	}
	if page.Current <= 0 {
		page.Current = 1
	}
	sourceID := i.BaselineSourceID()
	out := SearchFilmResult{
		List:    []model.MovieBasicInfo{},
		Sources: []model.SearchSourceTab{},
	}
	if keyword == "" || sourceID == "" {
		return out
	}

	version := filmsnapshot.GetActiveReadModelVersion()
	sl, err := filmsnapshot.SearchSnapshotsByKeywordSourceAndSortFast(version, sourceID, keyword, sortField, page)
	if err != nil {
		out.Error = err.Error()
		return out
	}
	out.List = filmshared.BuildMovieBasicInfosFromSnapshots(sl...)
	return out
}
