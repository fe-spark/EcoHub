package film

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupOthersFilterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}

	if err := gdb.AutoMigrate(
		&model.Category{},
		&model.SearchTagItem{},
		&model.FilmListSnapshot{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	db.Mdb = gdb
	db.Rdb = nil
	return gdb
}

func TestListFilmSnapshotsByTags_OthersFilter(t *testing.T) {
	gdb := setupOthersFilterTestDB(t)
	version := "test_others_v1"
	const targetPid int64 = 9

	// 1. 初始化分类
	if err := gdb.Create(&model.Category{Id: targetPid, Pid: 0, Name: "电影", Show: true, Sort: 1}).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}

	// 2. 初始化前台可见标签
	tagItems := []model.SearchTagItem{
		{Pid: targetPid, TagType: "Area", Name: "中国大陆", Value: "中国大陆", Score: 100},
		{Pid: targetPid, TagType: "Area", Name: "美国", Value: "美国", Score: 90},
		{Pid: targetPid, TagType: "Language", Name: "普通话", Value: "普通话", Score: 100},
		{Pid: targetPid, TagType: "Language", Name: "英语", Value: "英语", Score: 90},
		{Pid: targetPid, TagType: "Year", Name: "2024", Value: "2024", Score: 100},
		{Pid: targetPid, TagType: "Year", Name: "2023", Value: "2023", Score: 90},
		{Pid: targetPid, TagType: "Plot", Name: "动作", Value: "动作", Score: 100},
		{Pid: targetPid, TagType: "Plot", Name: "喜剧", Value: "喜剧", Score: 90},
	}
	for _, item := range tagItems {
		if err := gdb.Create(&item).Error; err != nil {
			t.Fatalf("create tag item: %v", err)
		}
	}

	// 3. 插入测试影片快照
	snapshots := []model.FilmListSnapshot{
		// Snapshot 1: 全部为常见标签
		{
			SnapshotVersion: version, Mid: 1, Pid: targetPid,
			Name: "常见主流大片", Area: "中国大陆", Language: "普通话", Year: 2024, ClassTag: "动作,喜剧",
		},
		// Snapshot 2: 仅 Language 为小众/其他，其余常见
		{
			SnapshotVersion: version, Mid: 2, Pid: targetPid,
			Name: "小众语言片", Area: "美国", Language: "法语", Year: 2023, ClassTag: "动作",
		},
		// Snapshot 3: 全部为冷门其他值
		{
			SnapshotVersion: version, Mid: 3, Pid: targetPid,
			Name: "冷门小众影视", Area: "丹麦", Language: "丹麦语", Year: 1998, ClassTag: "科幻",
		},
		// Snapshot 4: 全部标签为空（应属于其他）
		{
			SnapshotVersion: version, Mid: 4, Pid: targetPid,
			Name: "无标签影视", Area: "", Language: "", Year: 0, ClassTag: "",
		},
		// Snapshot 5: 属于其他一级分类
		{
			SnapshotVersion: version, Mid: 5, Pid: 10,
			Name: "电视剧冷门", Area: "丹麦", Language: "丹麦语", Year: 1998, ClassTag: "科幻",
		},
		// Snapshot 6: 剧情同时含可见标签与不可见标签，用于锁定 Plot 多值时的判定口径
		{
			SnapshotVersion: version, Mid: 6, Pid: targetPid,
			Name: "混合剧情片", Area: "美国", Language: "普通话", Year: 2024, ClassTag: "动作,科幻",
		},
	}
	for _, s := range snapshots {
		if err := gdb.Create(&s).Error; err != nil {
			t.Fatalf("create snapshot: %v", err)
		}
	}

	// 验证 1: 常规筛选 Area=中国大陆 -> 仅命中 Mid=1
	{
		page := &dto.Page{Current: 1, PageSize: 10}
		res := ListFilmSnapshotsByTagsReadModel(version, model.SearchTagsVO{Pid: targetPid, Area: "中国大陆"}, page)
		if len(res) != 1 || res[0].Mid != 1 {
			t.Fatalf("expected only Mid=1, got %+v", res)
		}
		if page.Total != 1 {
			t.Fatalf("expected total=1, got %d", page.Total)
		}
	}

	// 验证 2: Area=others 筛选 -> 应命中 Mid=3, 4（排除 Mid=1, 2）
	{
		page := &dto.Page{Current: 1, PageSize: 10}
		res := ListFilmSnapshotsByTagsReadModel(version, model.SearchTagsVO{Pid: targetPid, Area: "others"}, page)
		if len(res) != 2 {
			t.Fatalf("Area=others: expected 2 results, got %d: %+v", len(res), res)
		}
		mids := map[int64]bool{res[0].Mid: true, res[1].Mid: true}
		if !mids[3] || !mids[4] {
			t.Fatalf("Area=others: expected Mid 3 and 4, got %+v", res)
		}
	}

	// 验证 3: Language=others 筛选 -> 应命中 Mid=2, 3, 4（排除 Mid=1）
	{
		page := &dto.Page{Current: 1, PageSize: 10}
		res := ListFilmSnapshotsByTagsReadModel(version, model.SearchTagsVO{Pid: targetPid, Language: "others"}, page)
		if len(res) != 3 {
			t.Fatalf("Language=others: expected 3 results, got %d", len(res))
		}
		for _, item := range res {
			if item.Mid == 1 {
				t.Fatalf("Language=others: should not contain Mid=1, got %+v", res)
			}
		}
	}

	// 验证 4: Year=others 筛选 -> 应命中 Mid=3, 4
	{
		page := &dto.Page{Current: 1, PageSize: 10}
		res := ListFilmSnapshotsByTagsReadModel(version, model.SearchTagsVO{Pid: targetPid, Year: "others"}, page)
		if len(res) != 2 {
			t.Fatalf("Year=others: expected 2 results, got %d", len(res))
		}
		mids := map[int64]bool{res[0].Mid: true, res[1].Mid: true}
		if !mids[3] || !mids[4] {
			t.Fatalf("Year=others: expected Mid 3 and 4, got %+v", res)
		}
	}

	// 验证 5: Plot=others 筛选 -> 应命中 Mid=3, 4
	// class_tag 是多值文本，判定口径为「不含任何可见剧情标签才算其他」：
	// Mid=6 的「动作,科幻」含可见标签「动作」，因此不命中。
	{
		page := &dto.Page{Current: 1, PageSize: 10}
		res := ListFilmSnapshotsByTagsReadModel(version, model.SearchTagsVO{Pid: targetPid, Plot: "others"}, page)
		if len(res) != 2 {
			t.Fatalf("Plot=others: expected 2 results, got %d", len(res))
		}
		mids := map[int64]bool{res[0].Mid: true, res[1].Mid: true}
		if !mids[3] || !mids[4] {
			t.Fatalf("Plot=others: expected Mid 3 and 4, got %+v", res)
		}
		if mids[6] {
			t.Fatalf("Plot=others: Mid=6 的 class_tag「动作,科幻」含可见标签「动作」，不应命中: %+v", res)
		}
	}

	// 验证 6: 用户复现用例 —— 四个维度全部为 others！
	// 绝对不能返回 pid=9 的全部 4 部数据，必须仅返回符合全部 others 条件的 Mid=3, 4
	{
		page := &dto.Page{Current: 1, PageSize: 10}
		st := model.SearchTagsVO{
			Pid:      targetPid,
			Area:     "others",
			Language: "others",
			Year:     "others",
			Plot:     "others",
		}
		res := ListFilmSnapshotsByTagsReadModel(version, st, page)
		if len(res) != 2 {
			t.Fatalf("All others: expected 2 results (Mid 3 and 4), got %d (all data leaked!)", len(res))
		}
		if page.Total != 2 {
			t.Fatalf("All others: expected total=2, got %d (should not be all data count 4)", page.Total)
		}
		mids := map[int64]bool{res[0].Mid: true, res[1].Mid: true}
		if !mids[3] || !mids[4] {
			t.Fatalf("All others: expected Mid 3 and 4, got %+v", res)
		}
	}

	// 验证 7: 多种输入格式别名兼容（"__others__", "其他", "其它", "other"）
	{
		aliases := []string{model.TagOthersValue, "其他", "其它", "other"}
		for _, alias := range aliases {
			page := &dto.Page{Current: 1, PageSize: 10}
			res := ListFilmSnapshotsByTagsReadModel(version, model.SearchTagsVO{Pid: targetPid, Area: alias}, page)
			if len(res) != 2 {
				t.Fatalf("alias %q: expected 2 results, got %d", alias, len(res))
			}
		}
	}
}

// TestListFilmSnapshotsByTags_OthersFilter_OverDisplayLimit 锁定「其他」的可见集合边界：
// 该维度取值超过展示上限（12）时，展示列表之外的取值必须归入「其他」。
func TestListFilmSnapshotsByTags_OthersFilter_OverDisplayLimit(t *testing.T) {
	gdb := setupOthersFilterTestDB(t)
	version := "test_others_limit_v1"
	const targetPid int64 = 9

	if err := gdb.Create(&model.Category{Id: targetPid, Pid: 0, Name: "电影", Show: true, Sort: 1}).Error; err != nil {
		t.Fatalf("create category: %v", err)
	}

	// 15 个地区标签按 Score 降序：前 12 个进展示列表，后 3 个归入「其他」
	areas := []string{
		"中国大陆", "美国", "日本", "韩国", "英国",
		"法国", "德国", "意大利", "西班牙", "印度",
		"泰国", "俄罗斯", "加拿大", "澳大利亚", "巴西",
	}
	for i, area := range areas {
		item := model.SearchTagItem{Pid: targetPid, TagType: "Area", Name: area, Value: area, Score: int64(100 - i)}
		if err := gdb.Create(&item).Error; err != nil {
			t.Fatalf("create tag item %s: %v", area, err)
		}
	}

	snapshots := []model.FilmListSnapshot{
		{SnapshotVersion: version, Mid: 1, Pid: targetPid, Name: "展示区内影片", Area: "中国大陆"},
		{SnapshotVersion: version, Mid: 2, Pid: targetPid, Name: "展示区外影片", Area: "巴西"},
		{SnapshotVersion: version, Mid: 3, Pid: targetPid, Name: "展示区外影片二", Area: "加拿大"},
	}
	for _, s := range snapshots {
		if err := gdb.Create(&s).Error; err != nil {
			t.Fatalf("create snapshot: %v", err)
		}
	}

	page := &dto.Page{Current: 1, PageSize: 10}
	res := ListFilmSnapshotsByTagsReadModel(version, model.SearchTagsVO{Pid: targetPid, Area: model.TagOthersValue}, page)
	if len(res) != 2 {
		t.Fatalf("Area=others: expected 2 results (Mid 2 and 3), got %d: %+v", len(res), res)
	}
	mids := map[int64]bool{}
	for _, item := range res {
		mids[item.Mid] = true
	}
	if !mids[2] || !mids[3] {
		t.Fatalf("Area=others: expected Mid 2 and 3, got %+v", res)
	}
	if mids[1] {
		t.Fatalf("Area=others: Mid=1 的「中国大陆」在前 12 个展示项内，不应命中")
	}
	if page.Total != 2 {
		t.Fatalf("Area=others: expected total=2, got %d", page.Total)
	}
}
