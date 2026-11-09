package migration

import (
	"strings"
	"testing"
	"time"

	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// legacyBannerRow 旧表 banners_record 的结构，只在测试里建表用。
type legacyBannerRow struct {
	Id            string `gorm:"primaryKey"`
	Mid           int64
	Name          string
	Year          int64
	Poster        string
	Picture       string
	PictureSlide  string
	CustomPicture string
	Blurb         string
	Score         float64
	Sort          int64
	IsCustomPic   bool
	IsPinned      bool
	Source        string
	TmdbID        int64
}

func (legacyBannerRow) TableName() string { return legacyBannerTable }

func TestMigrateBannerSlot_Backfill(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmIndex{}, &model.FilmSourcePlaylist{}, &model.FilmSource{}, &legacyBannerRow{}); err != nil {
		t.Fatal(err)
	}
	now := time.Now()
	gdb.Create(&[]model.FilmSource{
		{Id: "primary", Uri: "http://p.test", State: true, Sort: 1, CreatedAt: now},
		{Id: "other", Uri: "http://o.test", State: true, Sort: 2, CreatedAt: now},
	})
	film := func(mid int64, first string, year int64, score float64, blurb string) {
		gdb.Create(&model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: mid, FirstSourceId: first},
			FilmIndexContent:  model.FilmIndexContent{Year: year, Score: score, Blurb: blurb},
		})
	}
	film(1, "other", 2020, 8.0, "片库简介") // 首选站有线路 → 归首选站
	film(2, "other", 2021, 7.0, "片库二")  // 只在 other → 归 other
	film(3, "", 2022, 6.0, "")          // 没有站点 → 丢弃
	gdb.Create(&model.FilmSourcePlaylist{Mid: 1, SourceId: "primary", LineKind: model.LineKindPlay})

	gdb.Create(&[]legacyBannerRow{
		{Id: "a", Mid: 1, Sort: 2, Year: 2020, Score: 8.0, Blurb: "片库简介", IsPinned: true, Source: "auto"},
		{Id: "b", Mid: 2, Sort: 1, IsCustomPic: true, CustomPicture: "http://custom.jpg", PictureSlide: "http://slide.jpg", Blurb: "改过的简介", Score: 9.5, Year: 1999},
		{Id: "c", Mid: 3, Sort: 3},
		{Id: "d", Mid: 404, Sort: 4},                                       // 影片已删 → 丢弃
		{Id: "e", Mid: 1, Sort: 5},                                         // 同站重复 → 丢弃
		{Id: "f", Mid: 2, Sort: 6, TmdbID: 77, Picture: "http://tmdb.jpg"}, // 同站重复 → 丢弃
	})

	if err := migrateBannerSlot(gdb); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	if gdb.Migrator().HasTable(legacyBannerTable) {
		t.Fatalf("legacy table should be dropped")
	}
	var slots []model.BannerSlot
	gdb.Order("source_id, sort").Find(&slots)
	if len(slots) != 2 {
		t.Fatalf("want 2 slots, got %+v", slots)
	}
	other, primary := slots[0], slots[1]
	if primary.SourceId != "primary" || primary.Mid != 1 || primary.Sort != 1 || primary.ImageMode != model.BannerImageFollow ||
		primary.BlurbOverride != "" || primary.ScoreOverride != 0 || primary.YearOverride != 0 {
		t.Fatalf("primary slot mismatch: %+v", primary)
	}
	if other.SourceId != "other" || other.Mid != 2 || other.Sort != 1 || other.ImageMode != model.BannerImageCustom ||
		other.PosterOverride != "http://custom.jpg" || other.BackdropOverride != "http://slide.jpg" ||
		other.BlurbOverride != "改过的简介" || other.ScoreOverride != 9.5 || other.YearOverride != 1999 {
		t.Fatalf("other slot mismatch: %+v", other)
	}

	// 再跑一次不报错（旧表已删）。
	if err := migrateBannerSlot(gdb); err != nil {
		t.Fatalf("second run: %v", err)
	}
}

// MySQL 的 DROP TABLE 会隐式提交：上次回填已写入 banner_slot、旧表没删掉、迁移没记完成。重跑必须成功且不重复。
func TestMigrateBannerSlot_RerunAfterPartialFailure(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FilmIndex{}, &model.FilmSourcePlaylist{}, &model.FilmSource{}, &model.BannerSlot{}, &legacyBannerRow{}); err != nil {
		t.Fatal(err)
	}
	gdb.Create(&model.FilmSource{Id: "s1", Uri: "http://s1.test", State: true, Sort: 1, CreatedAt: time.Now()})
	gdb.Create(&model.FilmIndex{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, FirstSourceId: "s1"}})
	gdb.Create(&model.FilmIndex{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, FirstSourceId: "s1"}})
	gdb.Create(&[]legacyBannerRow{
		{Id: "a", Mid: 1, Sort: 1},
		{Id: "b", Mid: 2, Sort: 2, IsCustomPic: true, CustomPicture: "http://" + strings.Repeat("x", 600)},
	})
	// 上次失败留下的回填结果。
	gdb.Create(&[]model.BannerSlot{
		{Id: "a", SourceId: "s1", Mid: 1, Sort: 1, ImageMode: model.BannerImageFollow},
		{Id: "b", SourceId: "s1", Mid: 2, Sort: 2, ImageMode: model.BannerImageFollow},
	})

	if err := migrateBannerSlot(gdb); err != nil {
		t.Fatalf("rerun should succeed: %v", err)
	}
	if gdb.Migrator().HasTable(legacyBannerTable) {
		t.Fatalf("legacy table should be dropped")
	}
	var slots []model.BannerSlot
	gdb.Order("sort").Find(&slots)
	if len(slots) != 2 || slots[0].Mid != 1 || slots[1].Mid != 2 {
		t.Fatalf("want 2 slots without duplicates, got %+v", slots)
	}
	if slots[1].PosterOverride != "" {
		t.Fatalf("over-long poster override should be dropped, got %q", slots[1].PosterOverride)
	}
}
