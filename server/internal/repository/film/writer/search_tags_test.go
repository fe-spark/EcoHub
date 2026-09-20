package writer

import (
	"fmt"
	"strings"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func newWriterTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:writer_%s?mode=memory&cache=shared", strings.NewReplacer("/", "_", " ", "_").Replace(t.Name()))
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("打开测试数据库失败: %v", err)
	}
	if err := gdb.AutoMigrate(&model.SearchTagItem{}, &model.FilmIndex{}); err != nil {
		t.Fatalf("迁移测试数据库失败: %v", err)
	}

	origMdb := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() {
		db.Mdb = origMdb
	})
	return gdb
}

func TestAggregateSearchTagItemsDeterministicSorting(t *testing.T) {
	// 构造乱序标签切片
	rawItems := []model.SearchTagItem{
		{Pid: 2, TagType: "Year", Value: "2024", Name: "2024"},
		{Pid: 1, TagType: "Plot", Value: "动作", Name: "动作"},
		{Pid: 1, TagType: "Area", Value: "中国大陆", Name: "中国大陆"},
		{Pid: 1, TagType: "Plot", Value: "爱情", Name: "爱情"},
		{Pid: 2, TagType: "Plot", Value: "科幻", Name: "科幻"},
		{Pid: 1, TagType: "Area", Value: "中国大陆", Name: "中国大陆"}, // 重复项
	}

	for i := 0; i < 10; i++ {
		aggregated := aggregateSearchTagItems(rawItems)
		if len(aggregated) != 5 {
			t.Fatalf("预期聚合后 5 项，实际 %d", len(aggregated))
		}

		// 断言严格按 (Pid, TagType, Value) 升序
		for j := 1; j < len(aggregated); j++ {
			prev, cur := aggregated[j-1], aggregated[j]
			if prev.Pid > cur.Pid {
				t.Fatalf("排序错误: Pid 未升序 prev=%d cur=%d", prev.Pid, cur.Pid)
			}
			if prev.Pid == cur.Pid {
				if prev.TagType > cur.TagType {
					t.Fatalf("排序错误: TagType 未升序 prev=%s cur=%s", prev.TagType, cur.TagType)
				}
				if prev.TagType == cur.TagType && prev.Value >= cur.Value {
					t.Fatalf("排序错误: Value 未升序 prev=%s cur=%s", prev.Value, cur.Value)
				}
			}
		}
	}
}

func TestUpsertDynamicSearchTagsPreservesExistingScores(t *testing.T) {
	gdb := newWriterTestDB(t)

	// 1. 预置已有高热度标签 (Score = 500)
	existingTag := model.SearchTagItem{
		Pid:     1,
		TagType: "Plot",
		Name:    "动作",
		Value:   "动作",
		Score:   500,
	}
	if err := gdb.Create(&existingTag).Error; err != nil {
		t.Fatalf("创建预置标签失败: %v", err)
	}

	// 2. 增量写入一部新动作片 (附带新标签 "科幻")
	film := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 9999},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 10},
		FilmIndexContent:  model.FilmIndexContent{Name: "新片测试", ClassTag: "动作,科幻", Year: 2024},
	}

	if err := UpsertDynamicSearchTags(film); err != nil {
		t.Fatalf("UpsertDynamicSearchTags 失败: %v", err)
	}

	// 3. 校验既有标签的 Score 绝不能被覆盖为 1
	var actionTag model.SearchTagItem
	if err := gdb.Where("pid = ? AND tag_type = ? AND value = ?", 1, "Plot", "动作").First(&actionTag).Error; err != nil {
		t.Fatalf("查询动作标签失败: %v", err)
	}
	if actionTag.Score != 500 {
		t.Fatalf("既有标签 Score 被错误覆盖: 期望 500, 实际 %d", actionTag.Score)
	}

	// 4. 校验全新插入的标签赋予初始分值 Score = 1
	var scifiTag model.SearchTagItem
	if err := gdb.Where("pid = ? AND tag_type = ? AND value = ?", 1, "Plot", "科幻").First(&scifiTag).Error; err != nil {
		t.Fatalf("查询科幻标签失败: %v", err)
	}
	if scifiTag.Score != 1 {
		t.Fatalf("新标签 Score 错误: 期望 1, 实际 %d", scifiTag.Score)
	}
}
