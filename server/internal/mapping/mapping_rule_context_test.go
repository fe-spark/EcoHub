package mapping

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestGetMappingRuleContext_ReturnsRawCategoriesAndRules(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.Category{}, &model.CategoryMapping{}); err != nil {
		t.Fatal(err)
	}
	prevDB := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prevDB })
	if err := gdb.AutoMigrate(&model.SourceCategory{}, &model.FilmSource{}, &model.MappingRule{}); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if err := gdb.Create(&model.FilmSource{Id: "src1", Name: "爱旦", Uri: "http://src1.test", State: true}).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}
	rows := []model.SourceCategory{
		{SourceId: "src1", SourceTypeId: 5, ParentSourceTypeId: 0, RawName: "福利视频", Sort: 1, Depth: 0},
		{SourceId: "src1", SourceTypeId: 18, ParentSourceTypeId: 0, RawName: "网红主播", Sort: 2, Depth: 0},
		{SourceId: "src1", SourceTypeId: 1, ParentSourceTypeId: 0, RawName: "电影", Sort: 4, Depth: 0},
		{SourceId: "src1", SourceTypeId: 12, ParentSourceTypeId: 1, RawName: "动作片", Sort: 1, Depth: 1},
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatalf("create source categories: %v", err)
	}
	if err := gdb.Create(&model.MappingRule{
		SourceId: "src1", Group: "CategoryRoot", Raw: "福利视频|网红主播", Target: "男人帮", MatchType: "regex",
	}).Error; err != nil {
		t.Fatalf("create rule: %v", err)
	}
	if err := gdb.Create(&model.MappingRule{
		SourceId: "src2", Group: "CategoryRoot", Raw: "他站", Target: "忽略", MatchType: "exact",
	}).Error; err != nil {
		t.Fatalf("create other rule: %v", err)
	}

	ctx, err := GetMappingRuleContext("src1")
	if err != nil {
		t.Fatal(err)
	}
	if ctx.SourceName != "爱旦" || len(ctx.RawCategories) != 4 || len(ctx.Rules) != 1 {
		t.Fatalf("unexpected context: %+v", ctx)
	}
	if ctx.Rules[0].Target != "男人帮" {
		t.Fatalf("unexpected rule target: %+v", ctx.Rules[0])
	}
}
