package service

import (
	"log"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/repository/film"
	filmshared "server/internal/repository/film/shared"
	filmsnapshot "server/internal/repository/film/snapshot"
)

const (
	dailyUpdateDefaultPageSize = 21
	dailyUpdateMaxPageSize     = 100
	dailyUpdateMaxExclude      = 500
)

// DailyUpdateListReq 每日更新列表请求：分类 + 标准分页 + 随机。
type DailyUpdateListReq struct {
	Pid      int64
	Page     *dto.Page
	Random   bool
	Exclude  []int64
	SourceId string
}

// DailyUpdateCategory 近 24h 分类统计。Pid: 0 全部，-1 其他，>0 导航大类。
type DailyUpdateCategory struct {
	Pid   int64  `json:"pid"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// DailyUpdateResult 每日更新响应。
type DailyUpdateResult struct {
	List       []model.MovieBasicInfo `json:"list"`
	Page       *dto.Page              `json:"page"`
	Categories []DailyUpdateCategory  `json:"categories"`
}

func normalizeDailyUpdateReq(req DailyUpdateListReq) DailyUpdateListReq {
	page := req.Page
	if page == nil {
		page = &dto.Page{}
	}
	if page.Current <= 0 {
		page.Current = 1
	}
	if page.PageSize <= 0 {
		page.PageSize = dailyUpdateDefaultPageSize
	}
	if page.PageSize > dailyUpdateMaxPageSize {
		page.PageSize = dailyUpdateMaxPageSize
	}
	req.Page = page
	if !req.Random {
		req.Exclude = nil
	} else {
		req.Exclude = film.ClampDailyUpdateExclude(req.Exclude, dailyUpdateMaxExclude)
	}
	return req
}

func fillDailyUpdatePage(page *dto.Page, total int) *dto.Page {
	page.Total = total
	if page.PageSize <= 0 {
		page.PageSize = dailyUpdateDefaultPageSize
	}
	page.PageCount = (total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	return page
}

// AssembleDailyUpdateCategories 导航顺序输出有片的大类；全部永远第一项。
func AssembleDailyUpdateCategories(nav []model.Category, countByPid map[int64]int, otherCount, total int) []DailyUpdateCategory {
	out := make([]DailyUpdateCategory, 0, len(nav)+2)
	out = append(out, DailyUpdateCategory{Pid: film.DailyPidAll, Name: "全部", Count: total})
	for _, n := range nav {
		if n.Id <= 0 {
			continue
		}
		if c := countByPid[n.Id]; c > 0 {
			out = append(out, DailyUpdateCategory{Pid: n.Id, Name: n.Name, Count: c})
		}
	}
	if otherCount > 0 {
		out = append(out, DailyUpdateCategory{Pid: film.DailyPidOther, Name: "其他", Count: otherCount})
	}
	return out
}

// DailyUpdates 近 24h 更新列表与分类聚合。
func (i *IndexService) DailyUpdates(req DailyUpdateListReq) (*DailyUpdateResult, error) {
	req = normalizeDailyUpdateReq(req)
	if req.SourceId == "" {
		if active := repository.GetActiveCollectSource(); active != nil {
			req.SourceId = active.Id
		}
	}
	from, to := film.Rolling24hWindow(time.Now())
	nav, err := film.NavTopCategories(req.SourceId)
	if err != nil {
		return nil, err
	}
	navIDs := film.NavTopCategoryIDs(nav)

	mids, total, err := film.ListDailyUpdateMids(film.DailyUpdateListQuery{
		From:     from,
		To:       to,
		Pid:      req.Pid,
		Current:  req.Page.Current,
		PageSize: req.Page.PageSize,
		Random:   req.Random,
		Exclude:  req.Exclude,
		NavIDs:   navIDs,
		SourceId: req.SourceId,
	})
	if err != nil {
		log.Printf("[IndexService] DailyUpdates list mids: %v", err)
		return nil, err
	}

	page := fillDailyUpdatePage(req.Page, total)
	list, err := hydrateDailyUpdateMids(mids)
	if err != nil {
		return nil, err
	}
	countByPid, otherCount, catTotal, catErr := film.DailyUpdatePidCounts(from, to, navIDs, req.SourceId)
	if catErr != nil {
		return nil, catErr
	}
	cats := AssembleDailyUpdateCategories(nav, countByPid, otherCount, catTotal)
	return &DailyUpdateResult{List: list, Page: page, Categories: cats}, nil
}

func hydrateDailyUpdateMids(mids []int64) ([]model.MovieBasicInfo, error) {
	if len(mids) == 0 {
		return []model.MovieBasicInfo{}, nil
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	snaps, err := filmsnapshot.GetProjectedSnapshotsByMidsOrdered(version, mids)
	if err != nil {
		return nil, err
	}
	list := filmshared.BuildMovieBasicInfosFromSnapshots(snaps...)
	if list == nil {
		return []model.MovieBasicInfo{}, nil
	}
	return list, nil
}
