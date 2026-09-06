package film

import (
	"fmt"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"

	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func setupTestDBForFuzzySearch(t *testing.T) (*gorm.DB, string) {
	t.Helper()
	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.FilmListSnapshot{}); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}

	version := fmt.Sprintf("v_test_%d", time.Now().UnixNano())
	now := time.Now().Unix()

	testFilms := []model.FilmListSnapshot{
		{
			SnapshotVersion: version,
			Mid:             101,
			Pid:             1,
			Cid:             10,
			SeriesKey:       "qingyunian",
			Name:            "庆余年 第二季",
			Hits:            9500,
			Score:           8.8,
			Year:            2024,
			UpdateStamp:     now - 1000,
		},
		{
			SnapshotVersion: version,
			Mid:             102,
			Pid:             1,
			Cid:             10,
			SeriesKey:       "qingyunian",
			Name:            "庆余年 第一季",
			Hits:            8000,
			Score:           9.0,
			Year:            2019,
			UpdateStamp:     now - 5000,
		},
		{
			SnapshotVersion: version,
			Mid:             103,
			Pid:             1,
			Cid:             10,
			Name:            "关于庆余年的拍摄花絮与解说",
			Hits:            12000,
			Score:           6.0,
			Year:            2024,
			UpdateStamp:     now,
		},
		{
			SnapshotVersion: version,
			Mid:             104,
			Pid:             1,
			Cid:             11,
			Name:            "流浪地球2",
			Hits:            18000,
			Score:           9.2,
			Year:            2023,
			UpdateStamp:     now - 2000,
		},
		{
			SnapshotVersion: version,
			Mid:             105,
			Pid:             2,
			Cid:             20,
			Name:            "哈利·波特与魔法石",
			Hits:            15000,
			Score:           9.5,
			Year:            2001,
			UpdateStamp:     now - 8000,
		},
		{
			SnapshotVersion: version,
			Mid:             106,
			Pid:             2,
			Cid:             21,
			Name:            "凡人修仙传",
			Hits:            14000,
			Score:           9.1,
			Year:            2020,
			UpdateStamp:     now - 300,
		},
		{
			SnapshotVersion: version,
			Mid:             107,
			Pid:             3,
			Cid:             30,
			Name:            "少林足球",
			Hits:            16000,
			Score:           8.9,
			Year:            2001,
			UpdateStamp:     now - 9000,
		},
		{
			SnapshotVersion: version,
			Mid:             108,
			Pid:             3,
			Cid:             31,
			Name:            "星际穿越",
			Hits:            22000,
			Score:           9.6,
			Year:            2014,
			UpdateStamp:     now - 100,
		},
	}

	for _, f := range testFilms {
		if err := gdb.Create(&f).Error; err != nil {
			t.Fatalf("create test film: %v", err)
		}
	}

	oldDB := db.Mdb
	oldModel := activeFilmReadModel.Load()
	db.Mdb = gdb
	activeFilmReadModel.Store(&FilmReadModel{Version: version})
	t.Cleanup(func() {
		db.Mdb = oldDB
		if oldModel != nil {
			activeFilmReadModel.Store(oldModel)
		} else {
			activeFilmReadModel.Store(&FilmReadModel{Version: ""})
		}
	})

	return gdb, version
}

func TestFuzzySearchScenarios(t *testing.T) {
	_, version := setupTestDBForFuzzySearch(t)

	// 1. 测试关键词检索: "庆余年"
	t.Run("KeywordSearch_QingYuNian", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "庆余年", "", page)
		if len(res) == 0 {
			t.Fatalf("expected results for '庆余年', got 0")
		}
		if page.Total < 3 {
			t.Errorf("expected at least 3 results for '庆余年', got %d", page.Total)
		}
	})

	// 2. 测试空格多词检索: "流浪地球 2"
	t.Run("SpaceToken_LiuLangDiQiu2", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "流浪地球 2", "", page)
		if len(res) == 0 {
			t.Fatalf("expected results for '流浪地球 2', got 0")
		}
		if res[0].Mid != 104 {
			t.Errorf("expected top result to be '流浪地球2' (Mid=104), got Mid=%d (%s)", res[0].Mid, res[0].Name)
		}
	})

	// 3. 测试按热度排序切换：当明确指定 sort="hits" 时，按热度降序排序
	t.Run("SortByHits", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "庆余年", "hits", page)
		if len(res) < 2 {
			t.Fatalf("expected at least 2 results")
		}
		if res[0].Hits < res[1].Hits {
			t.Errorf("expected descending hits order, got %d < %d", res[0].Hits, res[1].Hits)
		}
	})

	// 4. 不存在片名直接返回空结果，不穿透查库
	t.Run("NonExistent_Film_Returns_Empty", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "绝对不可能存在的片名XYZ12345", "", page)
		if len(res) != 0 {
			t.Fatalf("expected 0 results, got %d", len(res))
		}
		if page.Total != 0 {
			t.Fatalf("expected page.Total=0, got %d", page.Total)
		}
		if page.PageCount != 1 {
			t.Fatalf("expected page.PageCount=1, got %d", page.PageCount)
		}
	})

	// 5. ProvideVod 关键词搜索
	t.Run("ProvideVod_Search_And_Empty", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		hitRes := ListProvideSnapshotsReadModel(version, model.SearchTagsVO{}, "庆余年", 0, page)
		if len(hitRes) == 0 {
			t.Fatalf("expected results for '庆余年' in provide vod, got 0")
		}

		pageEmpty := &dto.Page{Current: 1, PageSize: 10}
		missRes := ListProvideSnapshotsReadModel(version, model.SearchTagsVO{}, "不存在的片名ABC999", 0, pageEmpty)
		if len(missRes) != 0 {
			t.Fatalf("expected 0 results for non-existent film in provide vod, got %d", len(missRes))
		}
		if pageEmpty.Total != 0 {
			t.Fatalf("expected page.Total=0, got %d", pageEmpty.Total)
		}
	})

	// 6. 管理端搜索：片名检索与复合条件透传
	t.Run("GetSearchPageReadModel_Scenarios", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := GetSearchPageReadModel(model.SearchVo{Name: "庆余年", Paging: page})
		if len(res) == 0 {
			t.Fatalf("expected results for '庆余年', got 0")
		}

		pageEmpty := &dto.Page{Current: 1, PageSize: 10}
		missRes := GetSearchPageReadModel(model.SearchVo{Name: "不存在的片名ZZZ000", Paging: pageEmpty})
		if len(missRes) != 0 {
			t.Fatalf("expected 0 results for non-existent film, got %d", len(missRes))
		}

		pagePlot := &dto.Page{Current: 1, PageSize: 10}
		plotRes := GetSearchPageReadModel(model.SearchVo{Name: "庆余年", Plot: "古装", Paging: pagePlot})
		_ = plotRes
	})

	// 7. 相关推荐候选集召回（同系列优先与同分类兜底）
	t.Run("RelatedCandidates_SeriesAndCategory", func(t *testing.T) {
		curr := model.FilmListSnapshot{
			Mid:       101,
			SeriesKey: "qingyunian",
			Name:      "庆余年 第二季",
			Pid:       1,
			Cid:       10,
		}
		cands := loadRelatedSnapshotCandidates(version, curr, 10)
		if len(cands) == 0 {
			t.Fatalf("expected related candidates for 庆余年 第二季, got 0")
		}
		foundSeries := false
		for _, c := range cands {
			if c.Mid == 102 {
				foundSeries = true
				break
			}
		}
		if !foundSeries {
			t.Errorf("expected related candidates to contain series sibling Mid 102")
		}
	})

	// 8. 相关推荐端到端检索与切片分页
	t.Run("ListRelatedSnapshotsReadModel_EndToEndPaging", func(t *testing.T) {
		curr := model.FilmListSnapshot{
			Mid:       101,
			SeriesKey: "qingyunian",
			Name:      "庆余年 第二季",
			Pid:       1,
			Cid:       10,
		}
		page1 := &dto.Page{Current: 1, PageSize: 1}
		res1 := ListRelatedSnapshotsReadModel(version, curr, page1)
		if len(res1) != 1 {
			t.Fatalf("expected 1 item on page 1, got %d", len(res1))
		}
		if page1.Total < 2 {
			t.Fatalf("expected page1.Total >= 2, got %d", page1.Total)
		}
		if page1.PageCount < 2 {
			t.Fatalf("expected page1.PageCount >= 2 for pageSize=1, got %d", page1.PageCount)
		}

		page2 := &dto.Page{Current: 2, PageSize: 1}
		res2 := ListRelatedSnapshotsReadModel(version, curr, page2)
		if len(res2) != 1 {
			t.Fatalf("expected 1 item on page 2, got %d", len(res2))
		}
		if res1[0].Mid == res2[0].Mid {
			t.Fatalf("expected different items across pages, got same Mid=%d", res1[0].Mid)
		}
	})

	// 9. 冷启动与空版本边界防御：所有只读模型函数在 version="" 时必须确保合法分页状态
	t.Run("EmptyVersionAndBoundary_PaginationState", func(t *testing.T) {
		// 临时清空活跃快照版本模拟冷启动
		origVer := GetActiveSnapshotVersion()
		_ = SetActiveSnapshotVersion("")
		t.Cleanup(func() {
			_ = SetActiveSnapshotVersion(origVer)
		})

		p1 := &dto.Page{Current: 1, PageSize: 10}
		r1 := SearchSnapshotsByKeywordAndSortReadModel("", "庆余年", "", p1)
		if len(r1) != 0 || p1.Total != 0 || p1.PageCount != 1 {
			t.Errorf("SearchSnapshots empty version: len=%d, total=%d, pageCount=%d", len(r1), p1.Total, p1.PageCount)
		}

		p2 := &dto.Page{Current: 1, PageSize: 10}
		r2 := ListRelatedSnapshotsReadModel("", model.FilmListSnapshot{Mid: 101}, p2)
		if len(r2) != 0 || p2.Total != 0 || p2.PageCount != 1 {
			t.Errorf("ListRelatedSnapshots empty version: len=%d, total=%d, pageCount=%d", len(r2), p2.Total, p2.PageCount)
		}

		p3 := &dto.Page{Current: 1, PageSize: 10}
		r3 := ListRelatedSnapshotsReadModel(version, model.FilmListSnapshot{Mid: 0}, p3)
		if len(r3) != 0 || p3.Total != 0 || p3.PageCount != 1 {
			t.Errorf("ListRelatedSnapshots mid=0: len=%d, total=%d, pageCount=%d", len(r3), p3.Total, p3.PageCount)
		}

		p4 := &dto.Page{Current: 1, PageSize: 10}
		r4 := ListProvideSnapshotsReadModel("", model.SearchTagsVO{}, "庆余年", 0, p4)
		if len(r4) != 0 || p4.Total != 0 || p4.PageCount != 1 {
			t.Errorf("ListProvideSnapshots empty version: len=%d, total=%d, pageCount=%d", len(r4), p4.Total, p4.PageCount)
		}

		p5 := &dto.Page{Current: 1, PageSize: 10}
		r5 := ListFilmSnapshotsByTagsReadModel("", model.SearchTagsVO{}, p5)
		if len(r5) != 0 || p5.Total != 0 || p5.PageCount != 1 {
			t.Errorf("ListFilmSnapshotsByTags empty version: len=%d, total=%d, pageCount=%d", len(r5), p5.Total, p5.PageCount)
		}
	})
}
