package query

import (
	"testing"
	"time"

	"server/internal/analytics/config"
	"server/internal/analytics/rollup"
	"server/internal/analytics/store"
	"server/internal/analytics/types"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/alicebob/miniredis/v2"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestQueryDashboardAndHistoricalScopedActionsAndSources(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer rdb.Close()

	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.AccessDailyStats{}, &model.AccessDailyTop{}, &model.FilmIndex{}, &model.Category{}, &model.FilmSource{}); err != nil {
		t.Fatal(err)
	}
	gdb.Create(&model.FilmSource{Id: "src_a", Name: "线路A", State: true})
	gdb.Create(&model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 301, FirstSourceId: "src_a"},
		FilmIndexCategory: model.FilmIndexCategory{CName: "科幻片"},
		FilmIndexContent:  model.FilmIndexContent{Name: "星际穿越"},
	})

	prevRdb, prevMdb := db.Rdb, db.Mdb
	db.Rdb, db.Mdb = rdb, gdb
	prevList := listCollectSources
	listCollectSources = func() ([]model.FilmSource, error) {
		var list []model.FilmSource
		err := gdb.Find(&list).Error
		return list, err
	}
	prevEnabled := config.IsEnabled()
	config.SetEnabled(true)
	ClearOverviewCache()
	t.Cleanup(func() {
		db.Rdb, db.Mdb = prevRdb, prevMdb
		listCollectSources = prevList
		config.SetEnabled(prevEnabled)
		ClearOverviewCache()
	})

	// 1. 写入今日数据并测 Dashboard
	now := time.Now().In(time.Local)
	todayStr := now.Format("2006-01-02")
	store.WriteBatch([]*types.Event{
		{
			Ts:              now,
			Method:          "PAGE",
			Path:            "/play?id=301",
			Page:            "/play?id=301",
			Action:          types.ActionPlay,
			ClientType:      "web",
			Resource:        "301",
			PlayMember:      "301",
			CollectSourceId: "src_a",
			UVMember:        "u1",
		},
	})
	dash, err := QueryDashboard(todayStr, "web", "all", "", "")
	if err != nil {
		t.Fatal(err)
	}
	if dash.Global.PV != 1 || dash.Scoped.PV != 1 || len(dash.Sources) != 1 || dash.Sources[0].Name != "线路A" {
		t.Fatalf("unexpected today dashboard: global=%+v scoped=%+v sources=%+v", dash.Global, dash.Scoped, dash.Sources)
	}

	// 2. 写入历史日归档数据，验证分端 Action 隔离与片源分布持久化查询
	yesterday := store.StartOfLocalDay(now).AddDate(0, 0, -1)
	yStr := yesterday.Format("2006-01-02")
	err = rollup.PersistDaily(model.AccessDailyStats{
		Day:             yStr,
		PV:              10,
		UV:              5,
		WebPV:           6,
		WebUV:           3,
		AppPV:           4,
		AppUV:           2,
		ActionJSON:      `{"play":5,"search":5}`,
		WebActionJSON:   `{"play":5,"browse":1}`,
		AppActionJSON:   `{"search":4}`,
		SourceCallsJSON: `{"src_a":7}`,
		VersionJSON:     `{"android":{"1.0.0":4}}`,
		RolledAt:        now,
	}, []model.AccessDailyTop{
		{Day: yStr, Kind: "web_play", Rank: 1, ItemKey: "301", Count: 5, Title: "星际穿越"},
	})
	if err != nil {
		t.Fatal(err)
	}

	webHist, err := QueryOverviewScope(yStr, "web", "all")
	if err != nil {
		t.Fatal(err)
	}
	if webHist.Action["play"] != 5 || webHist.Action["search"] != 0 {
		t.Fatalf("expected isolated web actions on historical day, got %+v", webHist.Action)
	}

	srcHist, err := QuerySourceCalls(yStr)
	if err != nil {
		t.Fatal(err)
	}
	if len(srcHist) != 1 || srcHist[0].Count != 7 {
		t.Fatalf("expected persisted source calls=7 on historical day, got %+v", srcHist)
	}
}
