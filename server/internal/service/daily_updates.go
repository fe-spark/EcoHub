package service

import (
	"log"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/notify"
	filmrepo "server/internal/repository/film"
)

const dailyUpdateDefaultPageSize = 21
const dailyUpdateMaxPageSize = 100
const dailyUpdateMaxExclude = 500

// DailyUpdateListReq V2 每日更新：分类 + 标准分页 + 随机。
type DailyUpdateListReq struct {
	Pid     int64
	Page    *dto.Page
	Random  bool
	Exclude []int64
}

// DailyUpdateCategory 近 24h 分类统计。Pid: 0 全部，-1 其他，>0 导航大类。
type DailyUpdateCategory struct {
	Pid   int64  `json:"pid"`
	Name  string `json:"name"`
	Count int    `json:"count"`
}

// DailyUpdateResult V2 响应。
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
		req.Exclude = notify.ClampDailyUpdateExclude(req.Exclude, dailyUpdateMaxExclude)
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
	out = append(out, DailyUpdateCategory{Pid: notify.DailyPidAll, Name: "全部", Count: total})
	for _, n := range nav {
		if n.Id <= 0 {
			continue
		}
		if c := countByPid[n.Id]; c > 0 {
			out = append(out, DailyUpdateCategory{Pid: n.Id, Name: n.Name, Count: c})
		}
	}
	if otherCount > 0 {
		out = append(out, DailyUpdateCategory{Pid: notify.DailyPidOther, Name: "其他", Count: otherCount})
	}
	return out
}

// DailyUpdatesV2 近 24h 更新。破坏性契约：无流式、不走首页 120 池。
func (i *IndexService) DailyUpdatesV2(req DailyUpdateListReq) (*DailyUpdateResult, error) {
	req = normalizeDailyUpdateReq(req)
	from, to := notify.Rolling24hWindow(time.Now())

	// 分类树每请求只取一次，复用给列表筛选、分类计数与组装，避免多次全表扫描。
	nav := notify.NavTopCategories()
	navIDs := notify.NavTopCategoryIDs(nav)

	mids, total, err := notify.ListDailyUpdateMids(notify.DailyUpdateListQuery{
		From:     from,
		To:       to,
		Pid:      req.Pid,
		Current:  req.Page.Current,
		PageSize: req.Page.PageSize,
		Random:   req.Random,
		Exclude:  req.Exclude,
		NavIDs:   navIDs,
	})
	if err != nil {
		log.Printf("[IndexService] DailyUpdatesV2 list mids: %v", err)
		return nil, err
	}

	page := fillDailyUpdatePage(req.Page, total)
	list := hydrateDailyUpdateMids(mids)
	if list == nil {
		list = []model.MovieBasicInfo{}
	}

	countByPid, otherCount, catTotal, catErr := notify.DailyUpdatePidCounts(from, to, navIDs)
	cats := []DailyUpdateCategory{}
	if catErr != nil {
		log.Printf("[IndexService] DailyUpdatesV2 category counts: %v", catErr)
		cats = []DailyUpdateCategory{{Pid: notify.DailyPidAll, Name: "全部", Count: total}}
	} else {
		// 「全部」角标始终用全窗 catTotal；当前 tab 数量只放在 page.total。
		cats = AssembleDailyUpdateCategories(nav, countByPid, otherCount, catTotal)
	}

	return &DailyUpdateResult{List: list, Page: page, Categories: cats}, nil
}

func hydrateDailyUpdateMids(mids []int64) []model.MovieBasicInfo {
	if len(mids) == 0 {
		return []model.MovieBasicInfo{}
	}
	version := filmrepo.GetActiveReadModelVersion()
	snaps := filmrepo.GetProjectedSnapshotsByMidsOrdered(version, mids)
	list := filmrepo.BuildMovieBasicInfosFromSnapshots(snaps...)
	if list == nil {
		return []model.MovieBasicInfo{}
	}
	applyLiveRemarksToMovies(list)
	return list
}
