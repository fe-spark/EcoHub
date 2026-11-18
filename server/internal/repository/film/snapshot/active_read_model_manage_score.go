package snapshot

import (
	"gorm.io/gorm"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/repository/film/shared"
)

// pageOrderedManageFilms 无关键词时按更新时间分页，并补上不落库的刮削标记。
// knownTotal 小于 0 时再数一遍；否则用调用方已经知道的总数。
// sourceID 非空时强制走所属站更新索引，避免整站 filesort 把读超时打满。
func pageOrderedManageFilms(q *gorm.DB, page *dto.Page, order string, knownTotal int64, sourceID string) ([]model.FilmIndex, error) {
	total := knownTotal
	if total < 0 {
		if err := applyManageScoreIndexHint(q.Session(&gorm.Session{}), sourceID).Count(&total).Error; err != nil {
			return nil, err
		}
	}
	page.Total = int(total)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	offset := shared.PageOffset(page)
	if offset >= page.Total {
		return []model.FilmIndex{}, nil
	}
	var list []model.FilmIndex
	if err := applyManageScoreIndexHint(q.Session(&gorm.Session{}), sourceID).Order(order).Offset(offset).Limit(page.PageSize).Find(&list).Error; err != nil {
		return nil, err
	}
	attachFilmScrapes(list)
	return list, nil
}

func applyManageScoreIndexHint(q *gorm.DB, sourceID string) *gorm.DB {
	if q == nil || sourceID == "" || q.Dialector == nil || q.Dialector.Name() != "mysql" {
		return q
	}
	return q.Clauses(mysqlUseIndexHint{index: repository.SourceFilmListIndex})
}

func orderFilmsByMids(mids []int64, list []model.FilmIndex) []model.FilmIndex {
	byMid := make(map[int64]model.FilmIndex, len(list))
	for _, row := range list {
		byMid[row.Mid] = row
	}
	out := make([]model.FilmIndex, 0, len(mids))
	for _, mid := range mids {
		row, ok := byMid[mid]
		if !ok {
			continue
		}
		out = append(out, row)
	}
	return out
}

func attachFilmScrapes(list []model.FilmIndex) {
	if len(list) == 0 {
		return
	}
	mids := make([]int64, 0, len(list))
	for i := range list {
		if list[i].Mid > 0 {
			mids = append(mids, list[i].Mid)
		}
	}
	scrapes := LoadActiveScrapesByMids(mids)
	for i := range list {
		if sc, ok := scrapes[list[i].Mid]; ok {
			ApplyTMDBScrapeToFilmIndex(&list[i], &sc)
		}
	}
}

// fillFilmSourceMids 把当前来源的源站编号写到列表上。库内 mid 是入库序号，不给后台当 ID 展示。
func fillFilmSourceMids(sourceID string, list []model.FilmIndex) error {
	if sourceID == "" || len(list) == 0 {
		return nil
	}
	mids := make([]int64, 0, len(list))
	for i := range list {
		if list[i].Mid > 0 {
			mids = append(mids, list[i].Mid)
		}
	}
	byGlobal, err := shared.LoadSourceMidsByGlobalMids(sourceID, mids)
	if err != nil {
		return err
	}
	for i := range list {
		list[i].SourceMid = byGlobal[list[i].Mid]
	}
	return nil
}
