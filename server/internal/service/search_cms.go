package service

import (
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmshared "server/internal/repository/film/shared"
	filmsnapshot "server/internal/repository/film/snapshot"
)

// SearchFilmResult 搜索接口业务结果。
type SearchFilmResult struct {
	List    []model.MovieBasicInfo
	Sources []model.SearchSourceTab
	Error   string
}

// SearchFilm 搜索统一走本地只读快照；支持各采集源快速过滤检索，秒级响应，零外部网络 I/O。
func (i *IndexService) SearchFilm(keyword, sourceID, sortField string, page *dto.Page) SearchFilmResult {
	keyword = strings.TrimSpace(keyword)
	sourceID = strings.TrimSpace(sourceID)
	if page == nil {
		page = &dto.Page{Current: 1, PageSize: 12}
	}
	if page.Current <= 0 {
		page.Current = 1
	}
	sources := buildSearchSourceTabs()
	if sourceID == "" {
		if active := repository.GetActiveCollectSource(); active != nil && active.Id != "" {
			sourceID = active.Id
		} else if len(sources) > 0 {
			sourceID = sources[0].Id
		}
	}
	out := SearchFilmResult{
		List:    []model.MovieBasicInfo{},
		Sources: sources,
	}
	if keyword == "" {
		return out
	}

	version := filmsnapshot.GetActiveReadModelVersion()
	var sl []model.FilmListSnapshot
	if sourceID != "" {
		sl = filmsnapshot.SearchSnapshotsByKeywordSourceAndSortFast(version, sourceID, keyword, sortField, page)
	} else {
		sl = filmsnapshot.SearchSnapshotsByKeywordAndSortFast(version, keyword, sortField, page)
	}

	out.List = filmshared.BuildMovieBasicInfosFromSnapshots(sl...)
	setSearchSourceCount(out.Sources, sourceID, page.Total)
	return out
}

func buildSearchSourceTabs() []model.SearchSourceTab {
	sources := repository.GetEnabledCollectSourceList()
	tabs := make([]model.SearchSourceTab, 0, len(sources))
	for _, source := range sources {
		name := strings.TrimSpace(source.Name)
		if name == "" {
			name = source.Id
		}
		tabs = append(tabs, model.SearchSourceTab{Id: source.Id, Name: name})
	}
	return tabs
}

func setSearchSourceCount(tabs []model.SearchSourceTab, sourceID string, total int) {
	for i := range tabs {
		if tabs[i].Id == sourceID {
			tabs[i].Count = total
			return
		}
	}
}
