package film

import (
	"fmt"
	"reflect"
	"strings"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupFilterSnapshotTestDB(t *testing.T) *gorm.DB {
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
		&model.FilmListSnapshot{},
		&model.FilmFilterOptionSnapshot{},
	); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	db.Mdb = gdb
	return gdb
}

func TestFilterOptionSnapshot_CompleteBuildAndDirtyDataRecovery(t *testing.T) {
	gdb := setupFilterSnapshotTestDB(t)
	version := "test_v1"

	// 1. 初始化分类
	rootCat := model.Category{Id: 1, Pid: 0, Name: "电影", Show: true, Sort: 1, StableKey: "movie"}
	subCat := model.Category{Id: 10, Pid: 1, Name: "动作片", Show: true, Sort: 1, StableKey: "action"}
	if err := gdb.Create(&rootCat).Error; err != nil {
		t.Fatalf("create rootCat: %v", err)
	}
	if err := gdb.Create(&subCat).Error; err != nil {
		t.Fatalf("create subCat: %v", err)
	}

	// 2. 准备 FilmListSnapshot 数据，包含完整的 Area/Language/Year/ClassTag
	films := []model.FilmListSnapshot{
		{
			SnapshotVersion: version,
			Mid:             1001,
			Pid:             1,
			Cid:             10,
			Name:            "流浪地球",
			Area:            "中国大陆",
			Language:        "汉语普通话",
			Year:            2023,
			ClassTag:        "科幻,冒险",
		},
		{
			SnapshotVersion: version,
			Mid:             1002,
			Pid:             1,
			Cid:             10,
			Name:            "星际穿越",
			Area:            "美国",
			Language:        "英语",
			Year:            2014,
			ClassTag:        "科幻,悬疑",
		},
	}
	if err := gdb.Create(&films).Error; err != nil {
		t.Fatalf("create films: %v", err)
	}

	// 3. 模拟“脏数据”场景：数据库中只有 Category 和 Sort
	dirtyOptions := []model.FilmFilterOptionSnapshot{
		{SnapshotVersion: version, Pid: 1, TagType: "Category", Name: "全部", Value: "", Sort: 0},
		{SnapshotVersion: version, Pid: 1, TagType: "Category", Name: "动作片", Value: "10", Sort: 1},
		{SnapshotVersion: version, Pid: 1, TagType: "Sort", Name: "最新", Value: "latest", Sort: 0},
	}
	if err := gdb.Create(&dirtyOptions).Error; err != nil {
		t.Fatalf("create dirtyOptions: %v", err)
	}

	// 4. 执行重构 RebuildFilterOptionSnapshot
	if err := RebuildFilterOptionSnapshot(version); err != nil {
		t.Fatalf("RebuildFilterOptionSnapshot: %v", err)
	}

	// 5. 验证各标签完整生成并入库
	var rows []model.FilmFilterOptionSnapshot
	if err := gdb.Where("snapshot_version = ? AND pid = ?", version, 1).Find(&rows).Error; err != nil {
		t.Fatalf("query rows: %v", err)
	}

	tagTypesPresent := make(map[string]bool)
	for _, r := range rows {
		tagTypesPresent[r.TagType] = true
	}
	for _, expectedType := range []string{"Category", "Sort", "Area", "Language", "Year", "Plot"} {
		if !tagTypesPresent[expectedType] {
			t.Fatalf("缺少标签类型 %s", expectedType)
		}
	}

	// 6. 验证 GetFilterOptionSnapshot 返回的响应结构
	res := GetFilterOptionSnapshot(version, 1)
	tags, ok := res["tags"].(map[string]any)
	if !ok {
		t.Fatalf("tags is not map[string]any")
	}
	for _, expectedType := range []string{"Category", "Sort", "Area", "Language", "Year", "Plot"} {
		if _, exists := tags[expectedType]; !exists {
			t.Fatalf("tags 响应中缺少 %s", expectedType)
		}
	}

	sortList, ok := res["sortList"].([]string)
	if !ok {
		t.Fatalf("sortList is not []string")
	}
	expectedSortList := []string{"Category", "Plot", "Area", "Language", "Year", "Sort"}
	if !reflect.DeepEqual(sortList, expectedSortList) {
		t.Fatalf("sortList 不匹配: got %v, want %v", sortList, expectedSortList)
	}
}

func TestFilterOptionSnapshot_NoSubCategories(t *testing.T) {
	gdb := setupFilterSnapshotTestDB(t)
	version := "test_v2"

	movieCat := model.Category{Id: 1, Pid: 0, Name: "电影", Show: true, Sort: 1, StableKey: "movie"}
	shortCat := model.Category{Id: 2, Pid: 0, Name: "短剧", Show: true, Sort: 2, StableKey: "short_play"}
	if err := gdb.Create(&[]model.Category{movieCat, shortCat}).Error; err != nil {
		t.Fatalf("create categories: %v", err)
	}

	films := []model.FilmListSnapshot{
		{
			SnapshotVersion: version,
			Mid:             1001,
			Pid:             1,
			Cid:             10,
			Name:            "星际穿越",
			Area:            "美国",
			Language:        "英语",
			Year:            2014,
			ClassTag:        "科幻",
		},
		{
			SnapshotVersion: version,
			Mid:             2001,
			Pid:             2,
			Cid:             0,
			Name:            "短剧一",
			Area:            "中国大陆",
			Language:        "汉语普通话",
			Year:            2024,
			ClassTag:        "甜宠,都市",
		},
	}
	if err := gdb.Create(&films).Error; err != nil {
		t.Fatalf("create films: %v", err)
	}

	if err := RebuildFilterOptionSnapshot(version); err != nil {
		t.Fatalf("RebuildFilterOptionSnapshot: %v", err)
	}

	res := GetFilterOptionSnapshot(version, 2)
	sortList, ok := res["sortList"].([]string)
	if !ok {
		t.Fatalf("sortList is not []string")
	}
	expectedSortList := []string{"Plot", "Area", "Language", "Year", "Sort"}
	if !reflect.DeepEqual(sortList, expectedSortList) {
		t.Fatalf("sortList 不匹配: got %v, want %v", sortList, expectedSortList)
	}

	tags, ok := res["tags"].(map[string]any)
	if !ok {
		t.Fatalf("tags is not map[string]any")
	}
	if _, exists := tags["Category"]; exists {
		t.Fatalf("无子分类大类不应返回 Category")
	}

	shortAreas := filterTagValues(t, res, "Area")
	if !containsStr(shortAreas, "中国大陆") {
		t.Fatalf("短剧 Area 缺少本类标签: %v", shortAreas)
	}
	if containsStr(shortAreas, "美国") {
		t.Fatalf("短剧 Area 串入了电影标签: %v", shortAreas)
	}

	movieAreas := filterTagValues(t, GetFilterOptionSnapshot(version, 1), "Area")
	if containsStr(movieAreas, "中国大陆") {
		t.Fatalf("电影 Area 串入了短剧标签: %v", movieAreas)
	}
}

func TestFilterOptionSnapshot_GetDoesNotPersistMissingRows(t *testing.T) {
	gdb := setupFilterSnapshotTestDB(t)
	version := "test_v3"
	rootCat := model.Category{Id: 3, Pid: 0, Name: "综艺", Show: true, Sort: 3, StableKey: "show"}
	if err := gdb.Create(&rootCat).Error; err != nil {
		t.Fatalf("create rootCat: %v", err)
	}
	if err := gdb.Create(&model.FilmListSnapshot{
		SnapshotVersion: version,
		Mid:             3001,
		Pid:             3,
		Cid:             0,
		Name:            "综艺一",
		Area:            "中国大陆",
		Language:        "汉语普通话",
		Year:            2024,
		ClassTag:        "真人秀",
	}).Error; err != nil {
		t.Fatalf("create film: %v", err)
	}

	res := GetFilterOptionSnapshot(version, 3)
	sortList, ok := res["sortList"].([]string)
	if !ok {
		t.Fatalf("sortList is not []string: %#v", res["sortList"])
	}
	if !reflect.DeepEqual(sortList, []string{"Sort"}) {
		t.Fatalf("缺快照时应只返回 Sort: got %v", sortList)
	}

	var count int64
	if err := gdb.Model(&model.FilmFilterOptionSnapshot{}).Count(&count).Error; err != nil {
		t.Fatalf("count: %v", err)
	}
	if count != 0 {
		t.Fatalf("GET 路径不得写库, 实际写入 %d 行", count)
	}
}

func filterTagValues(t *testing.T, res map[string]any, tagType string) []string {
	t.Helper()
	tags, ok := res["tags"].(map[string]any)
	if !ok {
		t.Fatalf("tags is not map[string]any")
	}
	raw, exists := tags[tagType]
	if !exists {
		return nil
	}
	list, ok := raw.([]map[string]string)
	if !ok {
		t.Fatalf("%s tags 类型异常: %T", tagType, raw)
	}
	vals := make([]string, 0, len(list))
	for _, item := range list {
		if v := strings.TrimSpace(item["Value"]); v != "" {
			vals = append(vals, v)
		}
	}
	return vals
}

func containsStr(list []string, want string) bool {
	for _, v := range list {
		if v == want {
			return true
		}
	}
	return false
}
