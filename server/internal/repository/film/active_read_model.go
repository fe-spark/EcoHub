package film

import (
	"log"
	"sort"
	"strings"
	"sync/atomic"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/support"
)

type FilmReadModel struct {
	Version string
	ByMid   map[int64]model.FilmListSnapshot
	ByPid   map[int64][]int64
	ByTag   map[string][]int64
	AllMIDs []int64
}

var activeFilmReadModel atomic.Value

func LoadActiveFilmReadModel(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" {
		activeFilmReadModel.Store((*FilmReadModel)(nil))
		return nil
	}

	startedAt := time.Now()
	var snapshots []model.FilmListSnapshot
	if err := applyVisibleCategoryFilter(db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version)).Find(&snapshots).Error; err != nil {
		return err
	}

	readModel := &FilmReadModel{
		Version: version,
		ByMid:   make(map[int64]model.FilmListSnapshot, len(snapshots)),
		ByPid:   make(map[int64][]int64),
		ByTag:   make(map[string][]int64),
		AllMIDs: make([]int64, 0, len(snapshots)),
	}

	for _, snapshot := range snapshots {
		mid := snapshot.Mid
		if mid <= 0 {
			continue
		}
		readModel.ByMid[mid] = snapshot
		readModel.AllMIDs = append(readModel.AllMIDs, mid)
		pid := support.ResolveCategoryID(snapshot.Pid)
		if pid > 0 {
			readModel.ByPid[pid] = append(readModel.ByPid[pid], mid)
		}
		for _, row := range buildFilterIndexRows(version, snapshot) {
			key := readModelTagKey(row.TagType, row.TagValue)
			readModel.ByTag[key] = append(readModel.ByTag[key], mid)
		}
	}

	activeFilmReadModel.Store(readModel)
	log.Printf("[ActiveReadModel] 加载完成 version=%s films=%d tags=%d cost=%s", version, len(readModel.ByMid), len(readModel.ByTag), time.Since(startedAt))
	return nil
}

func ClearActiveFilmReadModel() {
	activeFilmReadModel.Store((*FilmReadModel)(nil))
}

func GetActiveFilmReadModel() *FilmReadModel {
	value := activeFilmReadModel.Load()
	if value == nil {
		return nil
	}
	readModel, _ := value.(*FilmReadModel)
	return readModel
}

func requireActiveFilmReadModel(version string) *FilmReadModel {
	readModel := GetActiveFilmReadModel()
	if readModel == nil {
		panic("ActiveReadModel 未加载")
	}
	return readModel
}

func ListFilmSnapshotsByTagsReadModel(version string, st model.SearchTagsVO, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = ensurePage(page)
	st = normalizeSearchTagsVO(st)
	readModel := requireActiveFilmReadModel(version)

	mids := readModel.matchMIDs(st)
	snapshots := readModel.snapshotsByMIDs(mids)
	sortSnapshotsBySearchTag(snapshots, st.Sort)
	page.Total = len(snapshots)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}

	result := pageSnapshots(snapshots, page)
	log.Printf(
		"[FilmClassifySearch] 内存读模型筛选完成 pid=%d cid=%d plot=%q area=%q language=%q year=%q sort=%q total=%d page=%d size=%d cost=%s",
		st.Pid,
		st.Cid,
		st.Plot,
		st.Area,
		st.Language,
		st.Year,
		st.Sort,
		page.Total,
		page.Current,
		page.PageSize,
		time.Since(startedAt),
	)
	return result
}

func ListProvideSnapshotsReadModel(version string, st model.SearchTagsVO, keyword string, recentHours int, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = ensurePage(page)
	st = normalizeSearchTagsVO(st)
	readModel := requireActiveFilmReadModel(version)

	mids := readModel.matchMIDs(st)
	keyword = strings.TrimSpace(keyword)
	var timeLimit int64
	if recentHours > 0 {
		timeLimit = time.Now().Add(-time.Duration(recentHours) * time.Hour).Unix()
	}

	snapshots := make([]model.FilmListSnapshot, 0, len(mids))
	for _, mid := range mids {
		snapshot, exists := readModel.ByMid[mid]
		if !exists {
			continue
		}
		if keyword != "" && !strings.Contains(snapshot.Name, keyword) && !strings.Contains(snapshot.SubTitle, keyword) {
			continue
		}
		if timeLimit > 0 && snapshot.UpdateStamp < timeLimit {
			continue
		}
		snapshots = append(snapshots, snapshot)
	}

	sortSnapshotsBySearchTag(snapshots, st.Sort)
	page.Total = len(snapshots)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}

	result := pageSnapshots(snapshots, page)
	log.Printf(
		"[ProvideVod] 内存读模型筛选完成 pid=%d cid=%d keyword=%q total=%d page=%d size=%d cost=%s",
		st.Pid,
		st.Cid,
		keyword,
		page.Total,
		page.Current,
		page.PageSize,
		time.Since(startedAt),
	)
	return result
}

func SearchSnapshotsByKeywordReadModel(version string, keyword string, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = ensurePage(page)
	keyword = strings.TrimSpace(keyword)
	readModel := requireActiveFilmReadModel(version)

	snapshots := make([]model.FilmListSnapshot, 0, len(readModel.AllMIDs))
	for _, mid := range readModel.AllMIDs {
		snapshot, exists := readModel.ByMid[mid]
		if !exists {
			continue
		}
		if keyword == "" || strings.Contains(snapshot.Name, keyword) || strings.Contains(snapshot.SubTitle, keyword) {
			snapshots = append(snapshots, snapshot)
		}
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		if snapshots[i].Year != snapshots[j].Year {
			return snapshots[i].Year > snapshots[j].Year
		}
		if snapshots[i].UpdateStamp != snapshots[j].UpdateStamp {
			return snapshots[i].UpdateStamp > snapshots[j].UpdateStamp
		}
		return snapshots[i].Mid > snapshots[j].Mid
	})
	page.Total = len(snapshots)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}

	result := pageSnapshots(snapshots, page)
	log.Printf("[SearchFilm] 内存读模型筛选完成 keyword=%q total=%d page=%d size=%d cost=%s", keyword, page.Total, page.Current, page.PageSize, time.Since(startedAt))
	return result
}

func GetSearchPageReadModel(s model.SearchVo) []model.FilmIndex {
	startedAt := time.Now()
	page := ensurePage(s.Paging)
	readModel := requireActiveFilmReadModel(GetActiveSnapshotVersion())

	indexes := make([]model.FilmIndex, 0, len(readModel.AllMIDs))
	for _, mid := range readModel.AllMIDs {
		snapshot, exists := readModel.ByMid[mid]
		if !exists {
			continue
		}
		index := filmIndexFromSnapshot(snapshot)
		if matchesAdminSearch(index, s) {
			indexes = append(indexes, index)
		}
	}

	sort.SliceStable(indexes, func(i, j int) bool {
		if indexes[i].UpdateStamp != indexes[j].UpdateStamp {
			return indexes[i].UpdateStamp > indexes[j].UpdateStamp
		}
		return indexes[i].Mid > indexes[j].Mid
	})
	page.Total = len(indexes)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}

	offset := getPageOffset(page)
	if offset >= len(indexes) {
		return []model.FilmIndex{}
	}
	end := offset + page.PageSize
	if end > len(indexes) {
		end = len(indexes)
	}
	log.Printf(
		"[ManageFilmSearch] 内存读模型筛选完成 name=%q pid=%d cid=%d plot=%q area=%q language=%q year=%d total=%d page=%d size=%d cost=%s",
		s.Name,
		s.Pid,
		s.Cid,
		s.Plot,
		s.Area,
		s.Language,
		s.Year,
		page.Total,
		page.Current,
		page.PageSize,
		time.Since(startedAt),
	)
	return indexes[offset:end]
}

func (m *FilmReadModel) matchMIDs(st model.SearchTagsVO) []int64 {
	clauses := buildReadModelFilterClauses(&st)

	base := m.baseMIDs(st.Pid)
	if len(clauses) == 0 {
		return append([]int64(nil), base...)
	}

	candidateSet := midsToSet(base)
	for _, clause := range clauses {
		indexedMIDs := m.ByTag[readModelTagKey(clause.TagType, clause.TagValue)]
		if len(indexedMIDs) == 0 {
			return []int64{}
		}
		candidateSet = intersectMIDSet(candidateSet, indexedMIDs)
		if len(candidateSet) == 0 {
			return []int64{}
		}
	}

	mids := make([]int64, 0, len(candidateSet))
	for mid := range candidateSet {
		mids = append(mids, mid)
	}
	return mids
}

func buildReadModelFilterClauses(st *model.SearchTagsVO) []filterIndexClause {
	clauses, ok := buildFilterIndexClauses(st)
	if ok {
		return clauses
	}
	if st.Cid == model.TagUncategorizedValue ||
		st.Plot == model.TagUnknownValue || st.Plot == model.TagOthersValue ||
		st.Area == model.TagUnknownValue || st.Area == model.TagOthersValue ||
		st.Language == model.TagUnknownValue || st.Language == model.TagOthersValue ||
		st.Year == model.TagUnknownValue || st.Year == model.TagOthersValue {
		panic("ActiveReadModel 不支持未知/其他/未分类筛选")
	}
	if support.IsRootCategory(st.Cid) {
		st.Pid = support.ResolveCategoryID(st.Cid)
		st.Cid = 0
	}
	return []filterIndexClause{}
}

func (m *FilmReadModel) baseMIDs(pid int64) []int64 {
	pid = support.ResolveCategoryID(pid)
	if pid <= 0 {
		return m.AllMIDs
	}
	return m.ByPid[pid]
}

func (m *FilmReadModel) snapshotsByMIDs(mids []int64) []model.FilmListSnapshot {
	snapshots := make([]model.FilmListSnapshot, 0, len(mids))
	for _, mid := range mids {
		if snapshot, ok := m.ByMid[mid]; ok {
			snapshots = append(snapshots, snapshot)
		}
	}
	return snapshots
}

func midsToSet(mids []int64) map[int64]struct{} {
	set := make(map[int64]struct{}, len(mids))
	for _, mid := range mids {
		set[mid] = struct{}{}
	}
	return set
}

func intersectMIDSet(current map[int64]struct{}, mids []int64) map[int64]struct{} {
	next := make(map[int64]struct{})
	for _, mid := range mids {
		if _, ok := current[mid]; ok {
			next[mid] = struct{}{}
		}
	}
	return next
}

func readModelTagKey(tagType string, tagValue string) string {
	return strings.TrimSpace(tagType) + "\x00" + strings.TrimSpace(tagValue)
}

func filmIndexFromSnapshot(snapshot model.FilmListSnapshot) model.FilmIndex {
	return model.FilmIndex{
		Model: snapshot.Model,
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:        snapshot.Mid,
			ContentKey: snapshot.ContentKey,
			SourceId:   snapshot.SourceId,
			DbId:       snapshot.DbId,
		},
		FilmIndexCategory: model.FilmIndexCategory{
			Cid:              snapshot.Cid,
			Pid:              snapshot.Pid,
			RootCategoryKey:  snapshot.RootCategoryKey,
			CategoryKey:      snapshot.CategoryKey,
			OriginalCategory: snapshot.OriginalCategory,
			CName:            snapshot.CName,
		},
		FilmIndexContent: model.FilmIndexContent{
			SeriesKey:    snapshot.SeriesKey,
			Name:         snapshot.Name,
			SubTitle:     snapshot.SubTitle,
			ClassTag:     snapshot.ClassTag,
			Area:         snapshot.Area,
			Language:     snapshot.Language,
			Year:         snapshot.Year,
			Initial:      snapshot.Initial,
			Score:        snapshot.Score,
			UpdateStamp:  snapshot.UpdateStamp,
			Hits:         snapshot.Hits,
			State:        snapshot.State,
			Remarks:      snapshot.Remarks,
			Picture:      snapshot.Picture,
			PictureSlide: snapshot.PictureSlide,
			Actor:        snapshot.Actor,
			Director:     snapshot.Director,
			Blurb:        snapshot.Blurb,
		},
		FilmIndexVersion: model.FilmIndexVersion{
			CollectStamp:    snapshot.CollectStamp,
			CategoryVersion: snapshot.CategoryVersion,
			RuleVersion:     snapshot.RuleVersion,
		},
		FilmIndexDerived: model.FilmIndexDerived{
			PlayFromSummary: snapshot.PlayFromSummary,
		},
	}
}

func init() {
	activeFilmReadModel.Store((*FilmReadModel)(nil))
}
