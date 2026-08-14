package film

import (
	"encoding/json"
	"fmt"
	"testing"
	"time"

	"server/internal/model"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func openContentKeyTestDB(t *testing.T) *gorm.DB {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmIndex{}, &model.MovieDetailInfo{}); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	return gdb
}

func seedIndexWithKey(t *testing.T, gdb *gorm.DB, mid int64, contentKey, name string) model.FilmIndex {
	t.Helper()
	row := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:        mid,
			ContentKey: contentKey,
			SourceId:   "master",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name:        name,
			UpdateStamp: time.Now().Unix(),
		},
	}
	if err := gdb.Create(&row).Error; err != nil {
		t.Fatalf("seed index mid=%d key=%s: %v", mid, contentKey, err)
	}
	return row
}

func seedDetail(t *testing.T, gdb *gorm.DB, mid int64, detail model.MovieDetail) {
	t.Helper()
	data, err := json.Marshal(detail)
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&model.MovieDetailInfo{Mid: mid, SourceId: "master", Content: string(data)}).Error; err != nil {
		t.Fatalf("seed detail mid=%d: %v", mid, err)
	}
}

// TestUpsertByMidLazilyUpgradesContentKey 旧 name_* 行按 mid 命中后懒升 content_key→vod_*。
func TestUpsertByMidLazilyUpgradesContentKey(t *testing.T) {
	gdb := openContentKeyTestDB(t)
	seedIndexWithKey(t, gdb, 87682, "name_oldhash", "烬九州第四季")

	incoming := []model.FilmIndex{{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:        87682,
			ContentKey: "vod_87682",
			SourceId:   "master",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name:        "烬九州第四季",
			UpdateStamp: time.Now().Unix(),
		},
	}}
	if err := upsertFilmIndexesTx(gdb, incoming); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var row model.FilmIndex
	if err := gdb.Where("mid = ?", 87682).First(&row).Error; err != nil {
		t.Fatalf("reload: %v", err)
	}
	if row.ContentKey != "vod_87682" {
		t.Fatalf("content_key want vod_87682 got %s", row.ContentKey)
	}
	var n int64
	if err := gdb.Model(&model.FilmIndex{}).Count(&n).Error; err != nil {
		t.Fatal(err)
	}
	if n != 1 {
		t.Fatalf("want 1 row after upsert, got %d", n)
	}
}

// TestUpsertByMidFreesSoftDeletedContentKey 软删占用 vod_* 时先释放再懒升。
func TestUpsertByMidFreesSoftDeletedContentKey(t *testing.T) {
	gdb := openContentKeyTestDB(t)
	tomb := seedIndexWithKey(t, gdb, 999, "vod_1", "tomb")
	if err := gdb.Delete(&tomb).Error; err != nil {
		t.Fatalf("soft delete: %v", err)
	}
	seedIndexWithKey(t, gdb, 1, "name_one", "live")

	incoming := []model.FilmIndex{{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:        1,
			ContentKey: "vod_1",
			SourceId:   "master",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name:        "live",
			UpdateStamp: time.Now().Unix(),
		},
	}}
	if err := upsertFilmIndexesTx(gdb, incoming); err != nil {
		t.Fatalf("upsert: %v", err)
	}

	var live model.FilmIndex
	if err := gdb.Where("mid = ?", 1).First(&live).Error; err != nil {
		t.Fatalf("live: %v", err)
	}
	if live.ContentKey != "vod_1" {
		t.Fatalf("live content_key=%s", live.ContentKey)
	}

	var tombKey string
	if err := gdb.Unscoped().Model(&model.FilmIndex{}).
		Select("content_key").
		Where("id = ?", tomb.ID).
		Scan(&tombKey).Error; err != nil {
		t.Fatal(err)
	}
	want := fmt.Sprintf("del_%d", tomb.ID)
	if tombKey != want {
		t.Fatalf("tomb key=%s want %s", tombKey, want)
	}
}

// TestUpsertByMidRejectsLiveOccupantContentKey 活跃行占着目标 key 时拒绝写入，不抢键。
func TestUpsertByMidRejectsLiveOccupantContentKey(t *testing.T) {
	gdb := openContentKeyTestDB(t)
	seedIndexWithKey(t, gdb, 999, "vod_1", "wrong-owner")
	seedIndexWithKey(t, gdb, 1, "name_one", "live")

	incoming := []model.FilmIndex{{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:        1,
			ContentKey: "vod_1",
			SourceId:   "master",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name:        "live",
			UpdateStamp: time.Now().Unix(),
		},
	}}
	if err := upsertFilmIndexesTx(gdb, incoming); err == nil {
		t.Fatal("expect error when live row occupies content_key")
	}

	// 冲突方与本行均未改
	var wrong model.FilmIndex
	if err := gdb.Where("mid = ?", 999).First(&wrong).Error; err != nil {
		t.Fatal(err)
	}
	if wrong.ContentKey != "vod_1" {
		t.Fatalf("live occupant must stay, got %s", wrong.ContentKey)
	}
	var live model.FilmIndex
	if err := gdb.Where("mid = ?", 1).First(&live).Error; err != nil {
		t.Fatal(err)
	}
	if live.ContentKey != "name_one" {
		t.Fatalf("target mid content_key must stay name_*, got %s", live.ContentKey)
	}
}

// TestLegacyContentKeyForcesWriteEvenIfBusinessSame 业务无变更但 name_*→vod_* 必须强制写。
func TestLegacyContentKeyForcesWriteEvenIfBusinessSame(t *testing.T) {
	gdb := openContentKeyTestDB(t)
	seedIndexWithKey(t, gdb, 100, "name_legacy", "片A")
	detail := model.MovieDetail{
		Id: 100, Name: "片A",
		PlayFrom:        []string{"线路1"},
		PlayList:        [][]model.MovieUrlInfo{{{Episode: "1", Link: "http://x/1"}}},
		MovieDescriptor: model.MovieDescriptor{Remarks: "完结", State: "正片"},
	}
	seedDetail(t, gdb, 100, detail)

	infos := []model.FilmIndex{{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 100, ContentKey: "vod_100", SourceId: "master"},
		FilmIndexContent:  model.FilmIndexContent{Name: "片A", UpdateStamp: time.Now().Unix()},
	}}
	detailsByKey := map[string]model.MovieDetail{"vod_100": detail}
	unchanged, _, _, err := applyMasterBusinessUpdateStampsTx(gdb, infos, detailsByKey)
	if err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if _, ok := unchanged["vod_100"]; ok {
		t.Fatal("legacy content_key must force write, must not be unchanged")
	}
}

// TestStampUnchangedWhenContentKeyAlreadyVod 已是 vod_* 且业务无变更 → unchanged。
func TestStampUnchangedWhenContentKeyAlreadyVod(t *testing.T) {
	gdb := openContentKeyTestDB(t)
	seedIndexWithKey(t, gdb, 100, "vod_100", "片A")
	detail := model.MovieDetail{
		Id: 100, Name: "片A",
		PlayFrom:        []string{"线路1"},
		PlayList:        [][]model.MovieUrlInfo{{{Episode: "1", Link: "http://x/1"}}},
		MovieDescriptor: model.MovieDescriptor{Remarks: "完结", State: "正片"},
	}
	seedDetail(t, gdb, 100, detail)

	infos := []model.FilmIndex{{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 100, ContentKey: "vod_100", SourceId: "master"},
		FilmIndexContent:  model.FilmIndexContent{Name: "片A", UpdateStamp: time.Now().Unix()},
	}}
	// query 噪声不应触发业务变更
	detailsByKey := map[string]model.MovieDetail{
		"vod_100": {
			Id: 100, Name: "片A",
			PlayFrom:        []string{"线路1"},
			PlayList:        [][]model.MovieUrlInfo{{{Episode: "1", Link: "http://x/1?sig=new"}}},
			MovieDescriptor: model.MovieDescriptor{Remarks: "完结", State: "正片"},
		},
	}
	unchanged, _, _, err := applyMasterBusinessUpdateStampsTx(gdb, infos, detailsByKey)
	if err != nil {
		t.Fatalf("stamp: %v", err)
	}
	if _, ok := unchanged["vod_100"]; !ok {
		t.Fatalf("expect unchanged when already vod_*, got %v", unchanged)
	}
}

// TestKeyToMidFromIndexes 映射不依赖 DB content_key。
func TestKeyToMidFromIndexes(t *testing.T) {
	m := keyToMidFromIndexes([]model.FilmIndex{
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 1, ContentKey: "vod_1"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 2, ContentKey: "vod_2"}},
		{FilmIndexIdentity: model.FilmIndexIdentity{Mid: 0, ContentKey: "vod_0"}},
	})
	if len(m) != 2 || m["vod_1"] != 1 || m["vod_2"] != 2 {
		t.Fatalf("map=%v", m)
	}
}
