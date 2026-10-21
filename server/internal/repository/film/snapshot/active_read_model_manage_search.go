package snapshot

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"gorm.io/gorm"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"
	"server/internal/utils"
)

func applyNameLikeFilter(query *gorm.DB, keyword string) *gorm.DB {
	tokens := utils.ExtractSearchTokens(keyword)
	if len(tokens) == 0 {
		return query.Where("name LIKE ?", "%"+escapeLikePattern(keyword)+"%")
	}
	for _, tok := range tokens {
		query = query.Where("name LIKE ?", "%"+escapeLikePattern(tok)+"%")
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
		return "year DESC, mid DESC"
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
	idx, err := loadCurrentSearchIndex()
	if err != nil {
		return nil, err
	}
	matched, total, err := pageManageMetas(idx, s, page)
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
	log.Printf("[ManageFilmSearch] 内存检索完成 name=%q pid=%d cid=%d total=%d page=%d size=%d cost=%s", strings.TrimSpace(s.Name), s.Pid, s.Cid, page.Total, page.Current, len(snaps), time.Since(startedAt))
	return convertSnapshotsToFilmIndexes(snaps), nil
}

// pageManageMetas 无关键词时只拷贝当前页；有关键词时先打分再切页。
func pageManageMetas(idx *filmSearchMetaIndex, s model.SearchVo, page *dto.Page) ([]FilmSearchMeta, int, error) {
	if idx == nil {
		return nil, 0, ErrSearchIndexMissing
	}
	if len(idx.ByUpdate) != len(idx.Items) {
		return nil, 0, fmt.Errorf("检索索引损坏")
	}
	name := strings.TrimSpace(s.Name)
	if name != "" {
		hits, err := filterManageMetas(idx, s)
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
			window = append(window, idx.Items[hit])
		}
		return window, total, nil
	}
	sourceID := strings.TrimSpace(s.SourceId)
	offset := shared.PageOffset(page)
	end := offset + page.PageSize
	window := make([]FilmSearchMeta, 0, page.PageSize)
	total := 0
	for _, i := range idx.ByUpdate {
		if i < 0 || i >= len(idx.Items) {
			return nil, 0, fmt.Errorf("检索索引损坏")
		}
		meta := &idx.Items[i]
		if !manageMetaPass(meta, s, sourceID) {
			continue
		}
		if total >= offset && total < end {
			window = append(window, *meta)
		}
		total++
	}
	return window, total, nil
}

func filterManageMetas(idx *filmSearchMetaIndex, s model.SearchVo) ([]int, error) {
	if idx == nil || len(idx.Items) == 0 {
		return nil, nil
	}
	sourceID := strings.TrimSpace(s.SourceId)
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
		meta := &idx.Items[i]
		if !manageMetaPass(meta, s, sourceID) {
			continue
		}
		score := utils.ScoreFilmMatch(meta.Item, q)
		if score <= 0 {
			continue
		}
		hits = append(hits, scored{index: i, score: score})
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := &idx.Items[hits[i].index], &idx.Items[hits[j].index]
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

func manageMetaPass(meta *FilmSearchMeta, s model.SearchVo, sourceID string) bool {
	if sourceID != "" && !meta.HasPlaySource(sourceID) {
		return false
	}
	if !metaMatchesManageCategory(meta, sourceID, s.Pid, s.Cid) {
		return false
	}
	if s.Year > 0 && meta.Item.Year != s.Year {
		return false
	}
	if s.BeginTime > 0 && meta.Item.UpdateStamp < s.BeginTime {
		return false
	}
	if s.EndTime > 0 && meta.Item.UpdateStamp > s.EndTime {
		return false
	}
	if area := strings.TrimSpace(s.Area); area != "" && meta.Area != area {
		return false
	}
	if lang := strings.TrimSpace(s.Language); lang != "" && meta.Language != lang {
		return false
	}
	if plot := strings.TrimSpace(s.Plot); plot != "" && !strings.Contains(meta.ClassTag, plot) {
		return false
	}
	return true
}

func metaMatchesManageCategory(meta *FilmSearchMeta, sourceID string, pid, cid int64) bool {
	if cid <= 0 && pid <= 0 {
		return true
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID != "" {
		field := "pid"
		typeID := pid
		if cid > 0 {
			field = "cid"
			typeID = cid
		}
		return metaHasSourceType(meta, sourceID, repository.PublicSourceTypeIDs(sourceID, field, typeID))
	}
	if cid > 0 {
		return meta.Cid == cid
	}
	return meta.Pid == pid
}

func metaHasSourceType(meta *FilmSearchMeta, sourceID string, typeIDs []int64) bool {
	if meta == nil || len(typeIDs) == 0 {
		return false
	}
	for _, id := range typeIDs {
		key := support.BuildSourceCategoryKey(sourceID, id)
		if key != "" && meta.CategoryKey == key {
			return true
		}
	}
	return false
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

func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}
