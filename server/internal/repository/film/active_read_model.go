package film

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
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
	AllMIDs []int64
}

type ProjectedFilmReadModel struct {
	SnapshotVersion string
	RuleVersion     string
	ByMid           map[int64]model.FilmListSnapshot
	ByPid           map[int64][]int64
	ByTag           map[string][]int64
	FilterOptions   map[int64]map[string]any
	AllMIDs         []int64
}

type readModelProjectionContext struct {
	sourceKeyToCategoryIDs map[string][]int64
	categoriesByID         map[int64]model.Category
	categoryIDByStableKey  map[string]int64
	categoryIDByParentName map[string]int64
	rootIDByID             map[int64]int64
	resolvedSourceCategory map[string]int64
}

var activeFilmReadModel atomic.Value
var activeProjectedFilmReadModel atomic.Value
var activeFilmReadModelMu sync.Mutex

func init() {
	activeFilmReadModel.Store(newEmptyFilmReadModel(""))
	activeProjectedFilmReadModel.Store(newEmptyProjectedFilmReadModel("", ""))
}

func LoadActiveFilmReadModel(version string) error {
	activeFilmReadModelMu.Lock()
	defer activeFilmReadModelMu.Unlock()
	return loadActiveFilmReadModelLocked(version)
}

func loadActiveFilmReadModelLocked(version string) error {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" {
		activeFilmReadModel.Store(newEmptyFilmReadModel(""))
		log.Printf("[ActiveReadModel] 空库启动，已加载空读模型")
		return nil
	}

	startedAt := time.Now()
	var snapshots []model.FilmListSnapshot
	if err := db.Mdb.Where("snapshot_version = ?", version).Find(&snapshots).Error; err != nil {
		return err
	}

	readModel := &FilmReadModel{
		Version: version,
		ByMid:   make(map[int64]model.FilmListSnapshot, len(snapshots)),
		AllMIDs: make([]int64, 0, len(snapshots)),
	}

	for _, snapshot := range snapshots {
		mid := snapshot.Mid
		if mid <= 0 {
			continue
		}
		readModel.ByMid[mid] = snapshot
		readModel.AllMIDs = append(readModel.AllMIDs, mid)
	}

	activeFilmReadModel.Store(readModel)
	activeProjectedFilmReadModel.Store(newEmptyProjectedFilmReadModel(version, ""))
	ensureProjectedFilmReadModel(readModel)
	log.Printf("[ActiveReadModel] 加载完成 version=%s films=%d cost=%s", version, len(readModel.ByMid), time.Since(startedAt))
	return nil
}

func RefreshActiveProjectedReadModel() error {
	readModel := GetActiveFilmReadModel()
	if readModel == nil {
		return nil
	}
	activeProjectedFilmReadModel.Store(newEmptyProjectedFilmReadModel(readModel.Version, ""))
	ensureProjectedFilmReadModel(readModel)
	RefreshAccessDataCaches()
	return nil
}

func ApplyActiveFilmReadModelSnapshots(version string, snapshots []model.FilmListSnapshot, deletedMIDs []int64) error {
	activeFilmReadModelMu.Lock()
	defer activeFilmReadModelMu.Unlock()
	return applyActiveFilmReadModelSnapshotsLocked(version, snapshots, deletedMIDs)
}

func applyActiveFilmReadModelSnapshotsLocked(version string, snapshots []model.FilmListSnapshot, deletedMIDs []int64) error {
	version = strings.TrimSpace(version)
	if version == "" {
		return nil
	}
	current := GetActiveFilmReadModel()
	if current == nil || current.Version != version {
		return fmt.Errorf("active read model version mismatch: current=%s target=%s", GetActiveReadModelVersion(), version)
	}

	byMid := make(map[int64]model.FilmListSnapshot, len(current.ByMid)+len(snapshots))
	allMIDs := make([]int64, 0, len(current.AllMIDs)+len(snapshots))
	for _, mid := range current.AllMIDs {
		if snapshot, ok := current.ByMid[mid]; ok {
			byMid[mid] = snapshot
			allMIDs = append(allMIDs, mid)
		}
	}

	deleted := make(map[int64]struct{}, len(deletedMIDs))
	for _, mid := range deletedMIDs {
		if mid > 0 {
			deleted[mid] = struct{}{}
			delete(byMid, mid)
		}
	}
	if len(deleted) > 0 {
		kept := allMIDs[:0]
		for _, mid := range allMIDs {
			if _, ok := deleted[mid]; !ok {
				kept = append(kept, mid)
			}
		}
		allMIDs = kept
	}

	for _, snapshot := range snapshots {
		if snapshot.Mid <= 0 {
			continue
		}
		if _, existed := byMid[snapshot.Mid]; !existed {
			allMIDs = append(allMIDs, snapshot.Mid)
		}
		byMid[snapshot.Mid] = snapshot
	}

	readModel := &FilmReadModel{Version: version, ByMid: byMid, AllMIDs: allMIDs}
	activeFilmReadModel.Store(readModel)
	activeProjectedFilmReadModel.Store(newEmptyProjectedFilmReadModel(version, ""))
	ensureProjectedFilmReadModel(readModel)
	return nil
}

func newEmptyProjectedFilmReadModel(snapshotVersion string, ruleVersion string) *ProjectedFilmReadModel {
	return &ProjectedFilmReadModel{
		SnapshotVersion: strings.TrimSpace(snapshotVersion),
		RuleVersion:     strings.TrimSpace(ruleVersion),
		ByMid:           make(map[int64]model.FilmListSnapshot),
		ByPid:           make(map[int64][]int64),
		ByTag:           make(map[string][]int64),
		FilterOptions:   make(map[int64]map[string]any),
		AllMIDs:         []int64{},
	}
}

func ensureProjectedFilmReadModel(readModel *FilmReadModel) *ProjectedFilmReadModel {
	ruleVersion := support.GetRuleVersion()
	if value := activeProjectedFilmReadModel.Load(); value != nil {
		projected, _ := value.(*ProjectedFilmReadModel)
		if projected != nil && projected.SnapshotVersion == readModel.Version && projected.RuleVersion == ruleVersion {
			return projected
		}
	}

	startedAt := time.Now()
	ctx := newReadModelProjectionContext()
	projected := newEmptyProjectedFilmReadModel(readModel.Version, ruleVersion)
	for _, mid := range readModel.AllMIDs {
		snapshot, ok := readModel.ByMid[mid]
		if !ok {
			continue
		}
		snapshot = projectSnapshotCategoryWithContext(snapshot, ctx)
		if !isVisibleProjectedSnapshotWithContext(snapshot, ctx) {
			continue
		}
		projected.ByMid[mid] = snapshot
		projected.AllMIDs = append(projected.AllMIDs, mid)
		if snapshot.Pid > 0 {
			projected.ByPid[snapshot.Pid] = append(projected.ByPid[snapshot.Pid], mid)
		}
		for _, row := range buildFilterIndexRowsWithResolvedCategory(readModel.Version, snapshot, snapshot.Pid, snapshot.Cid) {
			projected.ByTag[readModelTagKey(row.TagType, row.TagValue)] = append(projected.ByTag[readModelTagKey(row.TagType, row.TagValue)], mid)
		}
	}
	projected.FilterOptions = buildProjectedFilterOptionResponses(readModel.Version, projected)
	activeProjectedFilmReadModel.Store(projected)
	log.Printf("[ActiveReadModel] 投影索引加载完成 snapshot=%s rule=%s films=%d tags=%d cost=%s", readModel.Version, ruleVersion, len(projected.ByMid), len(projected.ByTag), time.Since(startedAt))
	return projected
}

func newReadModelProjectionContext() readModelProjectionContext {
	ctx := readModelProjectionContext{
		sourceKeyToCategoryIDs: make(map[string][]int64),
		categoriesByID:         make(map[int64]model.Category),
		categoryIDByStableKey:  make(map[string]int64),
		categoryIDByParentName: make(map[string]int64),
		rootIDByID:             make(map[int64]int64),
		resolvedSourceCategory: make(map[string]int64),
	}

	var categories []model.Category
	if err := db.Mdb.Find(&categories).Error; err == nil {
		for _, category := range categories {
			ctx.categoriesByID[category.Id] = category
			if stableKey := strings.TrimSpace(category.StableKey); stableKey != "" {
				ctx.categoryIDByStableKey[stableKey] = category.Id
			}
			ctx.categoryIDByParentName[categoryParentNameKey(category.Pid, category.Name)] = category.Id
		}
	}
	ctx.sourceKeyToCategoryIDs = ctx.buildSourceCategoryProjectionMap()
	return ctx
}

func (ctx readModelProjectionContext) buildSourceCategoryProjectionMap() map[string][]int64 {
	var sourceCategories []model.SourceCategory
	if err := db.Mdb.Order("source_id ASC, depth ASC, parent_source_type_id ASC, sort ASC, id ASC").Find(&sourceCategories).Error; err != nil {
		return map[string][]int64{}
	}

	sourceCategoryByKey := make(map[string]model.SourceCategory, len(sourceCategories))
	for _, item := range sourceCategories {
		key := support.BuildSourceCategoryKey(item.SourceId, item.SourceTypeId)
		if key == "" {
			continue
		}
		sourceCategoryByKey[key] = item
	}

	result := make(map[string][]int64, len(sourceCategories))
	for _, item := range sourceCategories {
		key := support.BuildSourceCategoryKey(item.SourceId, item.SourceTypeId)
		if key == "" {
			continue
		}
		categoryID := ctx.resolveSourceCategoryID(item, sourceCategoryByKey)
		if categoryID <= 0 {
			continue
		}
		result[key] = []int64{categoryID}
	}
	return result
}

func (ctx readModelProjectionContext) resolveSourceCategoryID(item model.SourceCategory, sourceCategoryByKey map[string]model.SourceCategory) int64 {
	key := support.BuildSourceCategoryKey(item.SourceId, item.SourceTypeId)
	if key == "" {
		return 0
	}
	if categoryID, ok := ctx.resolvedSourceCategory[key]; ok {
		return categoryID
	}
	defer func() {
		if _, ok := ctx.resolvedSourceCategory[key]; !ok {
			ctx.resolvedSourceCategory[key] = 0
		}
	}()

	rawName := strings.TrimSpace(item.RawName)
	if rawName == "" {
		return 0
	}
	if item.ParentSourceTypeId <= 0 {
		rootName := support.NormalizeRootCategoryName(rawName)
		if rootName != rawName {
			rootID := ctx.findDisplayCategoryID(0, rootName)
			if rootID <= 0 {
				return 0
			}
			categoryID := ctx.findDisplayCategoryID(rootID, rawName)
			ctx.resolvedSourceCategory[key] = categoryID
			return categoryID
		}
		categoryID := ctx.findDisplayCategoryID(0, rootName)
		ctx.resolvedSourceCategory[key] = categoryID
		return categoryID
	}

	parentKey := support.BuildSourceCategoryKey(item.SourceId, item.ParentSourceTypeId)
	parent, ok := sourceCategoryByKey[parentKey]
	if !ok {
		return 0
	}
	parentID := ctx.resolveSourceCategoryID(parent, sourceCategoryByKey)
	if parentID <= 0 {
		return 0
	}
	subName := support.NormalizeSubCategoryName(rawName)
	categoryID := ctx.findDisplayCategoryID(parentID, subName)
	ctx.resolvedSourceCategory[key] = categoryID
	return categoryID
}

func (ctx readModelProjectionContext) findDisplayCategoryID(pid int64, name string) int64 {
	name = strings.TrimSpace(name)
	if name == "" {
		return 0
	}
	if id := ctx.categoryIDByParentName[categoryParentNameKey(pid, name)]; id > 0 {
		return id
	}
	stableKey := buildProjectionCategoryStableKey(pid, name, ctx.categoriesByID)
	return ctx.categoryIDByStableKey[stableKey]
}

func categoryParentNameKey(pid int64, name string) string {
	return fmt.Sprintf("%d:%s", pid, strings.TrimSpace(name))
}

func buildProjectionCategoryStableKey(pid int64, name string, categoriesByID map[int64]model.Category) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if pid == 0 {
		return fmt.Sprintf("display:root:%s", name)
	}
	parentKey := strings.TrimSpace(categoriesByID[pid].StableKey)
	if parentKey == "" {
		return fmt.Sprintf("display:sub:%d:%s", pid, name)
	}
	return fmt.Sprintf("%s/%s", parentKey, name)
}

func (ctx readModelProjectionContext) rootID(id int64) int64 {
	if id <= 0 {
		return 0
	}
	if rootID, ok := ctx.rootIDByID[id]; ok {
		return rootID
	}
	curr := id
	for range [8]int{} {
		category, ok := ctx.categoriesByID[curr]
		if !ok || category.Pid == 0 {
			ctx.rootIDByID[id] = curr
			return curr
		}
		curr = category.Pid
	}
	ctx.rootIDByID[id] = curr
	return curr
}

func (ctx readModelProjectionContext) isRootCategory(id int64) bool {
	category, ok := ctx.categoriesByID[id]
	return ok && category.Pid == 0
}

func (ctx readModelProjectionContext) categoryName(id int64) string {
	category, ok := ctx.categoriesByID[id]
	if !ok {
		return ""
	}
	return category.Name
}

func (ctx readModelProjectionContext) currentRootCategoryIDs(search model.FilmIndex) []int64 {
	ids := ctx.sourceKeyToCategoryIDs[strings.TrimSpace(search.RootCategoryKey)]
	if len(ids) == 0 && search.Pid > 0 {
		ids = []int64{search.Pid}
	}
	roots := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		rootID := ctx.rootID(id)
		if rootID <= 0 {
			rootID = id
		}
		if rootID <= 0 {
			continue
		}
		if _, ok := seen[rootID]; ok {
			continue
		}
		seen[rootID] = struct{}{}
		roots = append(roots, rootID)
	}
	return roots
}

func (ctx readModelProjectionContext) currentCategoryIDs(search model.FilmIndex) []int64 {
	ids := ctx.sourceKeyToCategoryIDs[strings.TrimSpace(search.CategoryKey)]
	if len(ids) == 0 && search.Cid > 0 {
		ids = []int64{search.Cid}
	}
	return ids
}

func projectSnapshotCategory(snapshot model.FilmListSnapshot) model.FilmListSnapshot {
	return projectSnapshotCategoryWithContext(snapshot, newReadModelProjectionContext())
}

func projectSnapshotCategoryWithContext(snapshot model.FilmListSnapshot, ctx readModelProjectionContext) model.FilmListSnapshot {
	index := filmIndexFromSnapshot(snapshot)
	rootIDs := ctx.currentRootCategoryIDs(index)
	if len(rootIDs) > 0 {
		snapshot.Pid = rootIDs[0]
	}

	categoryIDs := ctx.currentCategoryIDs(index)
	for _, id := range categoryIDs {
		if id <= 0 {
			continue
		}
		rootID := ctx.rootID(id)
		if rootID <= 0 {
			rootID = id
		}
		if snapshot.Pid <= 0 {
			snapshot.Pid = rootID
		}
		if id != snapshot.Pid && !ctx.isRootCategory(id) {
			snapshot.Cid = id
			break
		}
	}

	if snapshot.Cid > 0 {
		snapshot.CName = ctx.categoryName(snapshot.Cid)
	}
	if strings.TrimSpace(snapshot.CName) == "" && snapshot.Pid > 0 {
		snapshot.CName = ctx.categoryName(snapshot.Pid)
	}
	return snapshot
}

func isVisibleProjectedSnapshot(snapshot model.FilmListSnapshot) bool {
	return isVisibleProjectedSnapshotWithContext(snapshot, newReadModelProjectionContext())
}

func isVisibleProjectedSnapshotWithContext(snapshot model.FilmListSnapshot, ctx readModelProjectionContext) bool {
	if snapshot.Pid <= 0 {
		return false
	}
	pid := snapshot.Pid
	rootID := ctx.rootID(pid)
	if rootID <= 0 {
		rootID = pid
	}
	root, ok := ctx.categoriesByID[rootID]
	if !ok || !root.Show {
		return false
	}
	if snapshot.Cid <= 0 {
		return true
	}
	cid := snapshot.Cid
	category, ok := ctx.categoriesByID[cid]
	if !ok || !category.Show {
		return false
	}
	if category.Pid > 0 {
		parent, ok := ctx.categoriesByID[category.Pid]
		return ok && parent.Show
	}
	return true
}

func newEmptyFilmReadModel(version string) *FilmReadModel {
	return &FilmReadModel{
		Version: version,
		ByMid:   make(map[int64]model.FilmListSnapshot),
		AllMIDs: []int64{},
	}
}

func ClearActiveFilmReadModel() {
	activeFilmReadModel.Store(newEmptyFilmReadModel(""))
	activeProjectedFilmReadModel.Store(newEmptyProjectedFilmReadModel("", ""))
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
	version = strings.TrimSpace(version)
	readModel := GetActiveFilmReadModel()
	if readModel == nil {
		panic("ActiveReadModel 未加载")
	}
	if version != "" && readModel.Version != version {
		panic("ActiveReadModel 版本不一致")
	}
	return readModel
}

func ListFilmSnapshotsByTagsReadModel(version string, st model.SearchTagsVO, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = ensurePage(page)
	st = normalizeSearchTagsVO(st)
	readModel := requireActiveFilmReadModel(version)

	snapshots := readModel.projectedSnapshotsByTags(st)
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

	keyword = strings.TrimSpace(keyword)
	var timeLimit int64
	if recentHours > 0 {
		timeLimit = time.Now().Add(-time.Duration(recentHours) * time.Hour).Unix()
	}

	baseSnapshots := readModel.projectedSnapshotsByTags(st)
	snapshots := make([]model.FilmListSnapshot, 0, len(baseSnapshots))
	for _, snapshot := range baseSnapshots {
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

	baseSnapshots := readModel.projectedSnapshots()
	snapshots := make([]model.FilmListSnapshot, 0, len(baseSnapshots))
	for _, snapshot := range baseSnapshots {
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
	readModel := requireActiveFilmReadModel("")

	indexes := make([]model.FilmIndex, 0, len(readModel.AllMIDs))
	for _, snapshot := range readModel.projectedSnapshots() {
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
	projected := ensureProjectedFilmReadModel(m)
	pid = support.ResolveCategoryID(pid)
	if pid <= 0 {
		return projected.AllMIDs
	}
	return projected.ByPid[pid]
}

func (m *FilmReadModel) snapshotsByMIDs(mids []int64) []model.FilmListSnapshot {
	snapshots := make([]model.FilmListSnapshot, 0, len(mids))
	projected := ensureProjectedFilmReadModel(m)
	for _, mid := range mids {
		if snapshot, ok := projected.ByMid[mid]; ok {
			snapshots = append(snapshots, snapshot)
		}
	}
	return snapshots
}

func (m *FilmReadModel) projectedSnapshotByMID(mid int64) (model.FilmListSnapshot, bool) {
	projected := ensureProjectedFilmReadModel(m)
	snapshot, ok := projected.ByMid[mid]
	return snapshot, ok
}

func GetProjectedSnapshotByMid(version string, mid int64) *model.FilmListSnapshot {
	if mid <= 0 {
		return nil
	}
	readModel := requireActiveFilmReadModel(version)
	snapshot, ok := readModel.projectedSnapshotByMID(mid)
	if !ok {
		return nil
	}
	return &snapshot
}

func (m *FilmReadModel) projectedSnapshots() []model.FilmListSnapshot {
	projected := ensureProjectedFilmReadModel(m)
	snapshots := make([]model.FilmListSnapshot, 0, len(projected.AllMIDs))
	for _, mid := range projected.AllMIDs {
		snapshot, ok := projected.ByMid[mid]
		if !ok {
			continue
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots
}

func (m *FilmReadModel) projectedSnapshotsByPid(pid int64) []model.FilmListSnapshot {
	projected := ensureProjectedFilmReadModel(m)
	pid = support.ResolveCategoryID(pid)
	if pid <= 0 {
		return m.projectedSnapshots()
	}
	snapshots := make([]model.FilmListSnapshot, 0)
	for _, mid := range projected.ByPid[pid] {
		snapshot, ok := projected.ByMid[mid]
		if !ok {
			continue
		}
		snapshots = append(snapshots, snapshot)
	}
	return snapshots
}

func (m *FilmReadModel) projectedSnapshotsByTags(st model.SearchTagsVO) []model.FilmListSnapshot {
	st = normalizeSearchTagsVO(st)
	otherFilters := buildOtherReadModelFilters(st)
	stForIndex := clearOtherReadModelFilters(st)
	clauses := buildReadModelFilterClauses(&stForIndex)
	projected := ensureProjectedFilmReadModel(m)
	base := projected.ByPid[support.ResolveCategoryID(stForIndex.Pid)]
	if stForIndex.Pid <= 0 {
		base = projected.AllMIDs
	}
	if len(clauses) == 0 {
		return filterOtherSearchTagSnapshots(m.snapshotsByMIDs(base), stForIndex, otherFilters, projected.FilterOptions)
	}
	candidateSet := midsToSet(base)
	for _, clause := range clauses {
		indexedMIDs := projected.ByTag[readModelTagKey(clause.TagType, clause.TagValue)]
		if len(indexedMIDs) == 0 {
			return []model.FilmListSnapshot{}
		}
		candidateSet = intersectMIDSet(candidateSet, indexedMIDs)
		if len(candidateSet) == 0 {
			return []model.FilmListSnapshot{}
		}
	}
	mids := make([]int64, 0, len(candidateSet))
	for mid := range candidateSet {
		mids = append(mids, mid)
	}
	return filterOtherSearchTagSnapshots(m.snapshotsByMIDs(mids), stForIndex, otherFilters, projected.FilterOptions)
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
