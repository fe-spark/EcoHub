package snapshot

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"server/internal/film/catalog"
	"server/internal/film/shared"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/taxonomy"
	"server/internal/utils"
)

// likeNeedle 用 ! 转义通配符。连接串开了 interpolateParams，ESCAPE '\' 会被 MySQL 把引号吃掉。
func likeNeedle(s string) string {
	s = strings.ReplaceAll(s, "!", "!!")
	s = strings.ReplaceAll(s, "%", "!%")
	s = strings.ReplaceAll(s, "_", "!_")
	return "%" + s + "%"
}

func applyNameLikeFilter(query *gorm.DB, keyword string) *gorm.DB {
	tokens := utils.ExtractSearchTokens(keyword)
	if len(tokens) == 0 {
		return query.Where("name LIKE ? ESCAPE '!'", likeNeedle(keyword))
	}
	for _, tok := range tokens {
		query = query.Where("name LIKE ? ESCAPE '!'", likeNeedle(tok))
	}
	return query
}

func snapshotSortOrderClause(sortField string, keywordSearch bool) string {
	switch sortField {
	case "hits":
		return "hits DESC, mid DESC"
	case "latest":
		return "update_stamp DESC, mid DESC"
	case "year":
		// 同年内按更新时间，和前台 tagRankBefore 的次序一致
		return "year DESC, update_stamp DESC, mid DESC"
	case "score":
		return "score DESC, mid DESC"
	default:
		if keywordSearch {
			return "hits DESC, year DESC, update_stamp DESC, mid DESC"
		}
		return "update_stamp DESC, mid DESC"
	}
}

func GetSearchPageReadModel(s model.SearchVo) ([]model.FilmIndex, error) {
	startedAt := time.Now()
	page := shared.EnsurePage(s.Paging)
	sourceID := currentPrimarySourceID()
	idx, err := loadCurrentSearchIndex()
	if err != nil {
		return nil, err
	}
	matched, total, err := pageManageMetas(idx, s, page, sourceID)
	if err != nil {
		return nil, err
	}
	page.Total = total
	page.PageCount = (total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	if len(matched) == 0 {
		log.Printf("[ManageFilmSearch] 内存检索完成 name=%q total=%d page=%d cost=%s", strings.TrimSpace(s.Name), page.Total, page.Current, time.Since(startedAt))
		return []model.FilmIndex{}, nil
	}
	mids := make([]int64, 0, len(matched))
	for _, meta := range matched {
		if meta.Mid > 0 {
			mids = append(mids, meta.Mid)
		}
	}
	snaps, err := GetProjectedSnapshotsByMidsOrdered(idx.Version, mids)
	if err != nil {
		return nil, err
	}
	list := convertSnapshotsToFilmIndexes(snaps)
	if err := fillFilmSourceMids(sourceID, list); err != nil {
		return nil, err
	}
	log.Printf("[ManageFilmSearch] 内存检索完成 name=%q source=%s pid=%d cid=%d total=%d page=%d size=%d cost=%s", strings.TrimSpace(s.Name), sourceID, s.Pid, s.Cid, page.Total, page.Current, len(list), time.Since(startedAt))
	return list, nil
}

// fillFilmSourceMids 把当前主站的源站编号写到列表上。库内 mid 是入库序号，不给后台当 ID 展示。
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

// pageManageMetas 无关键词时只拷贝当前页；有关键词时先打分再切页。
func pageManageMetas(idx *filmSearchMetaIndex, s model.SearchVo, page *dto.Page, sourceID string) ([]FilmSearchMeta, int, error) {
	if idx == nil {
		return nil, 0, ErrSearchIndexMissing
	}
	if len(idx.ByUpdate) != len(idx.Items) {
		return nil, 0, fmt.Errorf("检索索引损坏")
	}
	filterCtx, ok := newManageFilterContext(s, sourceID)
	if !ok {
		return nil, 0, nil
	}
	name := strings.TrimSpace(s.Name)
	if name != "" {
		hits, err := filterManageMetas(idx, s, filterCtx)
		if err != nil {
			return nil, 0, err
		}
		total := len(hits)
		offset := shared.PageOffset(page)
		if offset >= total {
			return nil, total, nil
		}
		end := offset + page.PageSize
		if end > total {
			end = total
		}
		window := make([]FilmSearchMeta, 0, end-offset)
		for _, hit := range hits[offset:end] {
			window = append(window, *idx.Items[hit])
		}
		return window, total, nil
	}
	offset := shared.PageOffset(page)
	end := offset + page.PageSize
	window := make([]FilmSearchMeta, 0, page.PageSize)
	total := 0
	for _, i := range idx.ByUpdate {
		if i < 0 || i >= len(idx.Items) {
			return nil, 0, fmt.Errorf("检索索引损坏")
		}
		meta := idx.Items[i]
		if !filterCtx.pass(meta) {
			continue
		}
		if total >= offset && total < end {
			window = append(window, *meta)
		}
		total++
	}
	return window, total, nil
}

type manageFilterContext struct {
	sourceID          string
	categoryCheckMode int // 0: none, 1: local cid, 2: local pid, 3: source type key set
	cid               int64
	pid               int64
	validCategoryKeys map[string]struct{}
	year              int64
	beginTime         int64
	endTime           int64
	area              string
	language          string
	plot              string
}

func newManageFilterContext(s model.SearchVo, sourceID string) (*manageFilterContext, bool) {
	ctx := &manageFilterContext{
		sourceID:  strings.TrimSpace(sourceID),
		cid:       s.Cid,
		pid:       s.Pid,
		year:      s.Year,
		beginTime: s.BeginTime,
		endTime:   s.EndTime,
		area:      strings.TrimSpace(s.Area),
		language:  strings.TrimSpace(s.Language),
		plot:      strings.TrimSpace(s.Plot),
	}
	if s.Cid > 0 || s.Pid > 0 {
		if ctx.sourceID != "" {
			field := "pid"
			typeID := s.Pid
			if s.Cid > 0 {
				field = "cid"
				typeID = s.Cid
			}
			typeIDs := catalog.PublicSourceTypeIDs(ctx.sourceID, field, typeID)
			if len(typeIDs) == 0 {
				return nil, false
			}
			ctx.categoryCheckMode = 3
			ctx.validCategoryKeys = make(map[string]struct{}, len(typeIDs))
			for _, id := range typeIDs {
				key := taxonomy.BuildSourceCategoryKey(ctx.sourceID, id)
				if key != "" {
					ctx.validCategoryKeys[key] = struct{}{}
				}
			}
		} else {
			if s.Cid > 0 {
				ctx.categoryCheckMode = 1
			} else {
				ctx.categoryCheckMode = 2
			}
		}
	}
	return ctx, true
}

func (f *manageFilterContext) pass(meta *FilmSearchMeta) bool {
	if meta == nil {
		return false
	}
	if f.sourceID != "" && !meta.HasPlaySource(f.sourceID) {
		return false
	}
	return f.passOwnedMeta(meta)
}

func (f *manageFilterContext) passOwnedMeta(meta *FilmSearchMeta) bool {
	if meta == nil {
		return false
	}
	switch f.categoryCheckMode {
	case 1:
		if meta.Cid != f.cid {
			return false
		}
	case 2:
		if meta.Pid != f.pid {
			return false
		}
	case 3:
		if _, ok := f.validCategoryKeys[meta.CategoryKey]; !ok {
			return false
		}
	}
	if f.year > 0 && meta.Item.Year != f.year {
		return false
	}
	if f.beginTime > 0 && meta.Item.UpdateStamp < f.beginTime {
		return false
	}
	if f.endTime > 0 && meta.Item.UpdateStamp > f.endTime {
		return false
	}
	if f.area != "" && meta.Area != f.area {
		return false
	}
	if f.language != "" && meta.Language != f.language {
		return false
	}
	if f.plot != "" && !strings.Contains(meta.ClassTag, f.plot) {
		return false
	}
	return true
}

func filterManageMetas(idx *filmSearchMetaIndex, s model.SearchVo, filterCtx *manageFilterContext) ([]int, error) {
	if idx == nil || len(idx.Items) == 0 {
		return nil, nil
	}
	name := strings.TrimSpace(s.Name)
	q := utils.BuildQueryContext(name)
	type scored struct {
		index int
		score int
	}
	hits := make([]scored, 0)
	for _, i := range idx.ByUpdate {
		if i < 0 || i >= len(idx.Items) {
			return nil, fmt.Errorf("检索索引损坏")
		}
		meta := idx.Items[i]
		if !filterCtx.pass(meta) {
			continue
		}
		score := utils.ScoreFilmMatch(meta.Item, q)
		if score <= 0 {
			continue
		}
		hits = append(hits, scored{index: i, score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := idx.Items[hits[i].index], idx.Items[hits[j].index]
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		if a.Item.Hits != b.Item.Hits {
			return a.Item.Hits > b.Item.Hits
		}
		if a.Item.Year != b.Item.Year {
			return a.Item.Year > b.Item.Year
		}
		if a.Item.UpdateStamp != b.Item.UpdateStamp {
			return a.Item.UpdateStamp > b.Item.UpdateStamp
		}
		return a.Mid > b.Mid
	})
	out := make([]int, len(hits))
	for i, hit := range hits {
		out[i] = hit.index
	}
	return out, nil
}

func convertSnapshotsToFilmIndexes(snapshots []model.FilmListSnapshot) []model.FilmIndex {
	if len(snapshots) == 0 {
		return []model.FilmIndex{}
	}
	result := make([]model.FilmIndex, len(snapshots))
	for i, snap := range snapshots {
		result[i] = model.FilmIndex{
			CreatedAt: snap.CreatedAt,
			UpdatedAt: snap.UpdatedAt,
			FilmIndexIdentity: model.FilmIndexIdentity{
				Mid:  snap.Mid,
				DbId: snap.DbId,
			},
			FilmIndexCategory: model.FilmIndexCategory{
				Cid:              snap.Cid,
				Pid:              snap.Pid,
				RootCategoryKey:  snap.RootCategoryKey,
				CategoryKey:      snap.CategoryKey,
				OriginalCategory: snap.OriginalCategory,
				CName:            snap.CName,
			},
			FilmIndexContent: model.FilmIndexContent{
				SeriesKey:          snap.SeriesKey,
				Name:               snap.Name,
				SubTitle:           snap.SubTitle,
				ClassTag:           snap.ClassTag,
				Area:               snap.Area,
				Language:           snap.Language,
				Year:               snap.Year,
				Initial:            snap.Initial,
				Score:              snap.Score,
				UpdateStamp:        snap.UpdateStamp,
				Hits:               snap.Hits,
				State:              snap.State,
				Remarks:            snap.Remarks,
				Picture:            snap.Picture,
				PictureSlide:       snap.PictureSlide,
				CustomPicture:      snap.CustomPicture,
				CustomPictureSlide: snap.CustomPictureSlide,
				IsCustomPicture:    snap.IsCustomPicture,
				Actor:              snap.Actor,
				Director:           snap.Director,
				Writer:             snap.Writer,
				Blurb:              snap.Blurb,
				Content:            snap.Content,
				ReleaseDate:        snap.ReleaseDate,
			},
			FilmIndexVersion: model.FilmIndexVersion{
				CollectStamp:    snap.CollectStamp,
				CategoryVersion: snap.CategoryVersion,
				RuleVersion:     snap.RuleVersion,
			},
			FilmIndexDerived: model.FilmIndexDerived{
				PlayFromSummary: snap.PlayFromSummary,
			},
			IsScraped: snap.IsScraped,
			TmdbID:    snap.TmdbID,
		}
	}
	return result
}
