package snapshot

import (
	"sort"

	"gorm.io/gorm"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/repository/film/shared"
	"server/internal/utils"
)

// pageOrderedManageFilms 无关键词时按更新时间分页，并补上不落库的刮削标记。
// knownTotal 小于 0 时再数一遍；否则用调用方已经知道的总数。
func pageOrderedManageFilms(q *gorm.DB, page *dto.Page, order string, knownTotal int64) ([]model.FilmIndex, error) {
	total := knownTotal
	if total < 0 {
		if err := q.Session(&gorm.Session{}).Count(&total).Error; err != nil {
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
	if err := q.Session(&gorm.Session{}).Order(order).Offset(offset).Limit(page.PageSize).Find(&list).Error; err != nil {
		return nil, err
	}
	attachFilmScrapes(list)
	return list, nil
}

// manageScoreKeepCap 非主站关键词只保留得分最高的这些条，避免把整站命中载入内存。
const manageScoreKeepCap = 200

// manageScoreScanBatch 非主站搜索每条语句最多取这么多命中。
// 宽关键词命中很多时，一条语句读完整站会顶满读超时。
var manageScoreScanBatch = 2000

func applyManageScoreIndexHint(q *gorm.DB, sourceID string) *gorm.DB {
	if q == nil || sourceID == "" || q.Dialector == nil || q.Dialector.Name() != "mysql" {
		return q
	}
	return q.Clauses(mysqlUseIndexHint{index: repository.SourceFilmListIndex})
}

type manageScoreRow struct {
	Mid         int64
	Name        string
	Hits        int64
	Year        int64
	UpdateStamp int64
}

type manageScoreHit struct {
	manageScoreRow
	score int
}

func betterManageHit(a, b manageScoreHit) bool {
	if a.score != b.score {
		return a.score > b.score
	}
	if a.Hits != b.Hits {
		return a.Hits > b.Hits
	}
	if a.Year != b.Year {
		return a.Year > b.Year
	}
	if a.UpdateStamp != b.UpdateStamp {
		return a.UpdateStamp > b.UpdateStamp
	}
	return a.Mid > b.Mid
}

func keepManageHit(best []manageScoreHit, item manageScoreHit) []manageScoreHit {
	if len(best) < manageScoreKeepCap {
		return append(best, item)
	}
	worst := 0
	for i := 1; i < len(best); i++ {
		if betterManageHit(best[worst], best[i]) {
			worst = i
		}
	}
	if betterManageHit(item, best[worst]) {
		best[worst] = item
	}
	return best
}

// pageScoredManageFilms 非主站关键词与主站内存列表同一套打分和排序。
// 按更新时间分批读取，逐行打分，只留下得分最高的 manageScoreKeepCap 条。
func pageScoredManageFilms(q *gorm.DB, page *dto.Page, name, sourceID string) ([]model.FilmIndex, error) {
	query := utils.BuildQueryContext(name)
	q = applyNameNeedles(q, utils.NameLikeNeedles(query))
	batchSize := manageScoreScanBatch
	if batchSize <= 0 {
		batchSize = 2000
	}
	hits := make([]manageScoreHit, 0)
	var cursorStamp, cursorMid int64
	started := false
	for {
		batch := q.Session(&gorm.Session{}).Select("mid, name, hits, year, update_stamp").
			Order("update_stamp DESC, mid DESC").
			Limit(batchSize)
		if started {
			batch = batch.Where("(update_stamp < ? OR (update_stamp = ? AND mid < ?))", cursorStamp, cursorStamp, cursorMid)
		}
		batch = applyManageScoreIndexHint(batch, sourceID)
		rows, err := batch.Rows()
		if err != nil {
			return nil, err
		}
		n := 0
		for rows.Next() {
			var row manageScoreRow
			if err := rows.Scan(&row.Mid, &row.Name, &row.Hits, &row.Year, &row.UpdateStamp); err != nil {
				rows.Close()
				return nil, err
			}
			n++
			cursorStamp, cursorMid = row.UpdateStamp, row.Mid
			item := utils.FilmSearchItem{
				Mid:         row.Mid,
				Name:        row.Name,
				Hits:        row.Hits,
				Year:        row.Year,
				UpdateStamp: row.UpdateStamp,
			}
			if query.IsAsciiOnly {
				utils.FillSearchDerivedFields(&item)
			}
			score := utils.ScoreFilmMatch(item, query)
			if score <= 0 {
				continue
			}
			hits = keepManageHit(hits, manageScoreHit{manageScoreRow: row, score: score})
		}
		err = rows.Err()
		rows.Close()
		if err != nil {
			return nil, err
		}
		if n < batchSize {
			break
		}
		started = true
	}
	sort.SliceStable(hits, func(i, j int) bool {
		return betterManageHit(hits[i], hits[j])
	})
	page.Total = len(hits)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	offset := shared.PageOffset(page)
	if offset >= page.Total {
		return []model.FilmIndex{}, nil
	}
	end := offset + page.PageSize
	if end > len(hits) {
		end = len(hits)
	}
	mids := make([]int64, 0, end-offset)
	for _, item := range hits[offset:end] {
		if item.Mid > 0 {
			mids = append(mids, item.Mid)
		}
	}
	if len(mids) == 0 {
		return []model.FilmIndex{}, nil
	}
	var list []model.FilmIndex
	if err := db.Mdb.Where("mid IN ?", mids).Find(&list).Error; err != nil {
		return nil, err
	}
	ordered := orderFilmsByMids(mids, list)
	attachFilmScrapes(ordered)
	if err := fillFilmSourceMids(sourceID, ordered); err != nil {
		return nil, err
	}
	return ordered, nil
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
