package snapshot

import (
	"testing"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repobase"
)

func TestListProvideSnapshots_TagFilter(t *testing.T) {
	gdb := setupOthersFilterTestDB(t)
	version := "test_provide_filter_v1"
	const targetPid int64 = 9

	if err := gdb.Create(&model.Category{Id: targetPid, Pid: 0, Name: "电影", Show: true, Sort: 1}).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	snapshots := []model.FilmListSnapshot{
		{SnapshotVersion: version, Mid: 1, Pid: targetPid, Name: "大陆动作片", Area: "中国大陆", Year: 2024, ClassTag: "动作"},
		{SnapshotVersion: version, Mid: 2, Pid: targetPid, Name: "美国动作片", Area: "美国", Year: 2023, ClassTag: "动作"},
		{SnapshotVersion: version, Mid: 3, Pid: targetPid, Name: "大陆喜剧片", Area: "中国大陆", Year: 2024, ClassTag: "喜剧"},
	}
	if err := WriteLiveFilmsFromSnapshots(snapshots); err != nil {
		t.Fatalf("seed live films: %v", err)
	}

	page := &dto.Page{Current: 1, PageSize: 10}
	res := mustProvide(t, version, model.SearchTagsVO{Pid: targetPid, Area: "中国大陆"}, "", 0, page)
	if len(res) != 2 || page.Total != 2 {
		t.Fatalf("Area=中国大陆: expected 2, got len=%d total=%d", len(res), page.Total)
	}

	page2 := &dto.Page{Current: 1, PageSize: 10}
	res2 := mustProvide(t, version, model.SearchTagsVO{Pid: targetPid, Area: "中国大陆", Plot: "动作"}, "", 0, page2)
	if len(res2) != 1 || page2.Total != 1 || res2[0].Mid != 1 {
		t.Fatalf("Area+Plot: expected only Mid=1, got %+v total=%d", res2, page2.Total)
	}

	page3 := &dto.Page{Current: 1, PageSize: 10}
	res3 := mustProvide(t, version, model.SearchTagsVO{Pid: targetPid, Area: "美国"}, "动作片", 0, page3)
	if len(res3) != 1 || page3.Total != 1 || res3[0].Mid != 2 {
		t.Fatalf("keyword+Area: expected only Mid=2, got %+v total=%d", res3, page3.Total)
	}
}

func TestListProvideSnapshots_PrimaryKeywordKeepsArea(t *testing.T) {
	gdb := setupOthersFilterTestDB(t)
	if err := gdb.AutoMigrate(&model.SourceCategory{}, &model.CategoryMapping{}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.SourceCategory{
		SourceId: "source_master", SourceTypeId: 1, RawName: "电影", Sort: 1, Depth: 0,
	}).Error; err != nil {
		t.Fatal(err)
	}
	const version = "v-provide-keyword-area"
	snaps := []model.FilmListSnapshot{
		{SnapshotVersion: version, Mid: 1, Name: "美国动作片", SourceId: "source_master", Area: "美国", CategoryKey: "source:source_master:1", UpdateStamp: 30},
		{SnapshotVersion: version, Mid: 2, Name: "美国喜剧片", SourceId: "source_master", Area: "美国", CategoryKey: "source:source_master:1", UpdateStamp: 20},
		{SnapshotVersion: version, Mid: 3, Name: "大陆动作片", SourceId: "source_master", Area: "中国大陆", CategoryKey: "source:source_master:1", UpdateStamp: 10},
	}
	if err := WriteLiveFilmsFromSnapshots(snaps); err != nil {
		t.Fatal(err)
	}
	for _, mid := range []int64{1, 2, 3} {
		if err := gdb.Create(&model.FilmSourcePlaylist{Mid: mid, SourceId: "source_master", LineKind: "play", GroupIndex: 0}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := SetActiveSnapshotVersion(version); err != nil {
		t.Fatal(err)
	}
	prev := activeFilmSearchMetas.Load()
	activeFilmSearchMetas.Store(nil)
	t.Cleanup(func() { activeFilmSearchMetas.Store(prev) })

	page := &dto.Page{Current: 1, PageSize: 10}
	res := mustProvide(t, version, model.SearchTagsVO{SourceId: "source_master", Area: "美国"}, "动作片", 0, page)
	if page.Total != 1 || len(res) != 1 || res[0].Mid != 1 {
		t.Fatalf("主站关键字加地区应只留美国动作片, total=%d list=%v", page.Total, res)
	}
}

func TestListProvideSnapshots_SourceTypeNotLocalID(t *testing.T) {
	gdb := setupOthersFilterTestDB(t)
	if err := gdb.AutoMigrate(&model.CategoryMapping{}, &model.SourceCategory{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := gdb.Create(&[]model.Category{
		{Id: 90, Pid: 0, Name: "连续剧", Show: true, StableKey: "source:src_tv:2"},
		{Id: 9, Pid: 0, Name: "电影", Show: true, StableKey: "source:src_tv:1"},
	}).Error; err != nil {
		t.Fatalf("categories: %v", err)
	}
	if err := gdb.Create(&[]model.SourceCategory{
		{SourceId: "src_tv", SourceTypeId: 2, ParentSourceTypeId: 0, RawName: "连续剧", Sort: 1, Depth: 0},
		{SourceId: "src_tv", SourceTypeId: 13, ParentSourceTypeId: 2, RawName: "国产剧", Sort: 1, Depth: 1},
		{SourceId: "src_tv", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 2, Depth: 0},
	}).Error; err != nil {
		t.Fatalf("source categories: %v", err)
	}
	if err := gdb.Create(&[]model.CategoryMapping{
		{SourceId: "src_tv", SourceTypeId: 2, CategoryId: 90},
		{SourceId: "src_tv", SourceTypeId: 1, CategoryId: 9},
	}).Error; err != nil {
		t.Fatalf("mappings: %v", err)
	}

	version := "test_provide_source_type"
	snaps := []model.FilmListSnapshot{
		{SnapshotVersion: version, Mid: 1, Pid: 90, Name: "国产剧", CategoryKey: "source:src_tv:13", RootCategoryKey: "source:src_tv:2"},
		{SnapshotVersion: version, Mid: 2, Pid: 2, Name: "撞号", CategoryKey: "source:src_other:2", RootCategoryKey: "source:src_other:2"},
	}
	if err := WriteLiveFilmsFromSnapshots(snaps); err != nil {
		t.Fatalf("films: %v", err)
	}
	for _, mid := range []int64{1, 2} {
		if err := gdb.Create(&model.FilmSourcePlaylist{Mid: mid, SourceId: "src_tv", LineKind: "play", GroupIndex: 0}).Error; err != nil {
			t.Fatalf("playlist: %v", err)
		}
	}

	page := &dto.Page{Current: 1, PageSize: 10}
	res := mustProvide(t, version, model.SearchTagsVO{SourceId: "src_tv", Pid: 2}, "", 0, page)
	if len(res) != 1 || res[0].Mid != 1 {
		t.Fatalf("source type 2: got %+v total=%d", res, page.Total)
	}

	pageLocal := &dto.Page{Current: 1, PageSize: 10}
	local := mustProvide(t, version, model.SearchTagsVO{Pid: 90}, "", 0, pageLocal)
	if len(local) != 1 || local[0].Mid != 1 {
		t.Fatalf("local pid 90: got %+v total=%d", local, pageLocal.Total)
	}

	pageMiss := &dto.Page{Current: 1, PageSize: 10}
	miss := mustProvide(t, version, model.SearchTagsVO{SourceId: "src_tv", Pid: 90}, "", 0, pageMiss)
	if len(miss) != 0 || pageMiss.Total != 0 {
		t.Fatalf("local id must not match this source type, got %+v total=%d", miss, pageMiss.Total)
	}
}

func TestGetSnapshotByMid_UnscopedIncludesSoftDeleted(t *testing.T) {
	gdb := setupOthersFilterTestDB(t)
	version := "test_provide_unscoped_v1"
	idx := model.FilmIndex{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 11}, FilmIndexContent: model.FilmIndexContent{Name: "软删影片"}}
	if err := gdb.Create(&idx).Error; err != nil {
		t.Fatalf("create film: %v", err)
	}
	if err := gdb.Delete(&model.FilmIndex{}, 11).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}

	got, err := GetSnapshotByMid(version, 11)
	if err != nil {
		t.Fatal(err)
	}
	if got != nil {
		t.Fatalf("列表直读应尊重 film_index 软删, got %+v", got)
	}
	rows, err := GetSnapshotsByMidsOrdered(version, []int64{11})
	if err != nil {
		t.Fatal(err)
	}
	if len(rows) != 0 {
		t.Fatalf("软删影片不应出现在列表, got %+v", rows)
	}
}

func mustProvide(t *testing.T, version string, st model.SearchTagsVO, keyword string, recentHours int, page *dto.Page) []model.FilmListSnapshot {
	t.Helper()
	res, err := ListProvideSnapshotsReadModel(version, st, keyword, recentHours, page)
	if err != nil {
		t.Fatalf("采集输出检索失败: %v", err)
	}
	return res
}

func TestListProvideSnapshots_YearSortTieBreakByUpdateStamp(t *testing.T) {
	gdb := setupOthersFilterTestDB(t)
	version := "test_provide_year_sort_v1"
	const targetPid int64 = 9

	if err := gdb.Create(&model.Category{Id: targetPid, Pid: 0, Name: "电影", Show: true, Sort: 1}).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}
	snapshots := []model.FilmListSnapshot{
		{SnapshotVersion: version, Mid: 1, Pid: targetPid, Name: "A", Year: 2024, UpdateStamp: 300},
		{SnapshotVersion: version, Mid: 2, Pid: targetPid, Name: "B", Year: 2024, UpdateStamp: 100},
		{SnapshotVersion: version, Mid: 3, Pid: targetPid, Name: "C", Year: 2024, UpdateStamp: 200},
		{SnapshotVersion: version, Mid: 4, Pid: targetPid, Name: "D", Year: 2023, UpdateStamp: 999},
		{SnapshotVersion: version, Mid: 5, Pid: targetPid, Name: "E", Year: 2024, UpdateStamp: 200},
	}
	if err := WriteLiveFilmsFromSnapshots(snapshots); err != nil {
		t.Fatalf("seed live films: %v", err)
	}

	page := &dto.Page{Current: 1, PageSize: 10}
	res := mustProvide(t, version, model.SearchTagsVO{Pid: targetPid, Sort: "year"}, "", 0, page)
	want := []int64{1, 5, 3, 2, 4}
	if len(res) != len(want) {
		t.Fatalf("expected %d films, got %d", len(want), len(res))
	}
	for i, mid := range want {
		if res[i].Mid != mid {
			got := make([]int64, len(res))
			for j := range res {
				got[j] = res[j].Mid
			}
			t.Fatalf("year sort order = %v, want %v", got, want)
		}
	}

	// 与前台内存排序 tagRankBefore 逐对比较，保证两边次序相同
	for i := 0; i+1 < len(res); i++ {
		a := &FilmSearchMeta{Mid: res[i].Mid}
		a.Item.Year, a.Item.UpdateStamp = res[i].Year, res[i].UpdateStamp
		b := &FilmSearchMeta{Mid: res[i+1].Mid}
		b.Item.Year, b.Item.UpdateStamp = res[i+1].Year, res[i+1].UpdateStamp
		if !tagRankBefore("year", a, b) {
			t.Fatalf("TVBox order disagrees with tagRankBefore at mid %d -> %d", a.Mid, b.Mid)
		}
	}
}

func TestListProvideSnapshots_PrimaryKeywordOnlyRespectsVisibilityAndSourceType(t *testing.T) {
	gdb := setupOthersFilterTestDB(t)
	if err := gdb.AutoMigrate(&model.SourceCategory{}, &model.CategoryMapping{}); err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&[]model.Category{
		{Id: 90, Pid: 0, Name: "连续剧", Show: true, StableKey: "source:source_master:2"},
		{Id: 99, Pid: 0, Name: "停用分类", Show: true, StableKey: "source:source_master:99"},
		{Id: 2, Pid: 0, Name: "撞号电影", Show: true, StableKey: "source:source_master:5"},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Model(&model.Category{}).Where("id = ?", 99).Update("show", false).Error; err != nil {
		t.Fatal(err)
	}
	repobase.RefreshCategoryCache()
	t.Cleanup(repobase.RefreshCategoryCache)

	if err := gdb.Create(&[]model.SourceCategory{
		{SourceId: "source_master", SourceTypeId: 2, RawName: "连续剧", Sort: 1, Depth: 0},
		{SourceId: "source_master", SourceTypeId: 5, RawName: "电影", Sort: 2, Depth: 0},
	}).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&[]model.CategoryMapping{
		{SourceId: "source_master", SourceTypeId: 2, CategoryId: 90},
		{SourceId: "source_master", SourceTypeId: 5, CategoryId: 2},
	}).Error; err != nil {
		t.Fatal(err)
	}

	const version = "v-provide-keyword-only-visibility"
	snaps := []model.FilmListSnapshot{
		{SnapshotVersion: version, Mid: 1, Pid: 90, Name: "星际测试", SourceId: "source_master", CategoryKey: "source:source_master:2", RootCategoryKey: "source:source_master:2", UpdateStamp: 30},
		{SnapshotVersion: version, Mid: 2, Pid: 99, Name: "星际停用", SourceId: "source_master", CategoryKey: "source:source_master:2", RootCategoryKey: "source:source_master:2", UpdateStamp: 20},
		{SnapshotVersion: version, Mid: 3, Pid: 2, Name: "星际撞号", SourceId: "source_master", CategoryKey: "source:source_master:5", RootCategoryKey: "source:source_master:5", UpdateStamp: 10},
	}
	if err := WriteLiveFilmsFromSnapshots(snaps); err != nil {
		t.Fatal(err)
	}
	for _, mid := range []int64{1, 2, 3} {
		if err := gdb.Create(&model.FilmSourcePlaylist{Mid: mid, SourceId: "source_master", LineKind: "play", GroupIndex: 0}).Error; err != nil {
			t.Fatal(err)
		}
	}
	if err := SetActiveSnapshotVersion(version); err != nil {
		t.Fatal(err)
	}
	prev := activeFilmSearchMetas.Load()
	activeFilmSearchMetas.Store(nil)
	t.Cleanup(func() { activeFilmSearchMetas.Store(prev) })

	// 纯关键字检索不得泄漏本地已停用分类下的影片
	pageAll := &dto.Page{Current: 1, PageSize: 10}
	resAll := mustProvide(t, version, model.SearchTagsVO{SourceId: "source_master"}, "星际", 0, pageAll)
	if pageAll.Total != 2 || len(resAll) != 2 {
		t.Fatalf("主站纯关键字检索应过滤停用分类, total=%d list=%+v", pageAll.Total, resAll)
	}
	for _, item := range resAll {
		if item.Mid == 2 {
			t.Fatalf("停用分类影片 mid=2 不应泄漏")
		}
	}

	// 带采集站 type_id 的纯关键字检索应按映射分类键匹配，不与本地 pid=2 撞号
	pageType := &dto.Page{Current: 1, PageSize: 10}
	resType := mustProvide(t, version, model.SearchTagsVO{SourceId: "source_master", Pid: 2}, "星际", 0, pageType)
	if pageType.Total != 1 || len(resType) != 1 || resType[0].Mid != 1 {
		t.Fatalf("主站分类关键字检索应只匹配映射源分类且过滤停用分类, total=%d list=%+v", pageType.Total, resType)
	}
}
