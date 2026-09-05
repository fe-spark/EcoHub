package film

import (
	"fmt"
	"testing"
	"time"
	"unsafe"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"

	mysqlDriver "gorm.io/driver/mysql"
	"gorm.io/driver/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestRealDatabaseSearchQueries(t *testing.T) {
	dsn := "eco:ecohub@tcp(127.0.0.1:3306)/eco?charset=utf8mb4&parseTime=True&loc=Local"
	gdb, err := gorm.Open(mysqlDriver.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Skipf("cannot connect to mysql: %v", err)
	}

	var rows []struct {
		Mid         int64
		Pid         int64
		Cid         int64
		Name        string
		Hits        int64
		Score       float64
		Year        int64
		UpdateStamp int64
	}
	if err := gdb.Model(&model.FilmListSnapshot{}).
		Select("mid, pid, cid, name, hits, score, year, update_stamp").
		Scan(&rows).Error; err != nil {
		t.Fatalf("scan: %v", err)
	}
	raw := make([]filmSearchIndexRow, 0, len(rows))
	for _, r := range rows {
		raw = append(raw, filmSearchIndexRow{
			Mid: r.Mid, Pid: r.Pid, Cid: r.Cid, Name: r.Name,
			Hits: r.Hits, Score: r.Score, Year: r.Year, UpdateStamp: r.UpdateStamp,
		})
	}
	idx := buildSearchIndexFromRows("test", raw)
	t.Logf("Loaded %d items from database", len(idx.Items))

	matchedCeshi := scoreMemoryIndex(idx, "ceshi", "", 0, 0)
	for _, m := range matchedCeshi {
		if m.mid == 148491 { // 选择之她·他
			t.Fatalf("search 'ceshi' should NOT match '选择之她·他'")
		}
	}
}

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
			Name:            "庆余年 第二季",
			Hits:            9500,
			Score:           8.8,
			Year:            2024,
			UpdateStamp:     now - 1000,
		},
		{
			SnapshotVersion: version,
			Mid:             102,
			Name:            "庆余年 第一季",
			Hits:            8000,
			Score:           9.0,
			Year:            2019,
			UpdateStamp:     now - 5000,
		},
		{
			SnapshotVersion: version,
			Mid:             103,
			Name:            "关于庆余年的拍摄花絮与解说",
			Hits:            12000,
			Score:           6.0,
			Year:            2024,
			UpdateStamp:     now,
		},
		{
			SnapshotVersion: version,
			Mid:             104,
			Name:            "流浪地球2",
			Hits:            18000,
			Score:           9.2,
			Year:            2023,
			UpdateStamp:     now - 2000,
		},
		{
			SnapshotVersion: version,
			Mid:             105,
			Name:            "哈利·波特与魔法石",
			Hits:            15000,
			Score:           9.5,
			Year:            2001,
			UpdateStamp:     now - 8000,
		},
		{
			SnapshotVersion: version,
			Mid:             106,
			Name:            "凡人修仙传",
			Hits:            14000,
			Score:           9.1,
			Year:            2020,
			UpdateStamp:     now - 300,
		},
		{
			SnapshotVersion: version,
			Mid:             107,
			Name:            "少林足球",
			Hits:            16000,
			Score:           8.9,
			Year:            2001,
			UpdateStamp:     now - 9000,
		},
		{
			SnapshotVersion: version,
			Mid:             108,
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
	oldIdx := activeFilmSearchIndex.Load()
	oldModel := activeFilmReadModel.Load()
	db.Mdb = gdb
	activeFilmReadModel.Store(&FilmReadModel{Version: version})
	t.Cleanup(func() {
		db.Mdb = oldDB
		if oldIdx != nil {
			activeFilmSearchIndex.Store(oldIdx)
		} else {
			activeFilmSearchIndex.Store(&filmSearchMemoryIndex{Version: ""})
		}
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

	// 1. 测试空格多词检索: "庆余年 2"
	t.Run("SpaceToken_QingYuNian2", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "庆余年 2", "", page)
		if len(res) == 0 {
			t.Fatalf("expected results for '庆余年 2', got 0")
		}
		if res[0].Mid != 101 {
			t.Errorf("expected top result to be '庆余年 第二季' (Mid=101), got Mid=%d (%s)", res[0].Mid, res[0].Name)
		}
	})

	// 2. 测试拼音首字母缩写简拼: "lldq"
	t.Run("PinyinInitials_lldq", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "lldq", "", page)
		if len(res) == 0 {
			t.Fatalf("expected results for 'lldq', got 0")
		}
		if res[0].Mid != 104 {
			t.Errorf("expected top result to be '流浪地球2' (Mid=104), got Mid=%d (%s)", res[0].Mid, res[0].Name)
		}
	})

	// 3. 测试标点与特殊字符容错: "哈利波特与魔法石" (原片名为 "哈利·波特与魔法石")
	t.Run("PunctuationTolerance_HarryPotter", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "哈利波特与魔法石", "", page)
		if len(res) == 0 {
			t.Fatalf("expected results for '哈利波特与魔法石', got 0")
		}
		if res[0].Mid != 105 {
			t.Errorf("expected top result to be Mid=105, got Mid=%d (%s)", res[0].Mid, res[0].Name)
		}
	})

	// 4. 测试子序列跳字模糊匹配: "凡人传" -> 《凡人修仙传》
	t.Run("Subsequence_FanRenChuan", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "凡人传", "", page)
		if len(res) == 0 {
			t.Fatalf("expected results for '凡人传', got 0")
		}
		if res[0].Mid != 106 {
			t.Errorf("expected top result to be '凡人修仙传' (Mid=106), got Mid=%d (%s)", res[0].Mid, res[0].Name)
		}
	})

	// 5. 测试相关度排序优先：搜索 "庆余年"，即使花絮 (Mid=103) 更新时间和热度最高，正剧 (Mid=101/102) 必须排在花絮前面
	t.Run("Relevance_Beats_Popularity_On_ExactMatch", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "庆余年", "", page)
		if len(res) < 3 {
			t.Fatalf("expected at least 3 results for '庆余年', got %d", len(res))
		}
		// Mid 101 or 102 必须在 Mid 103 (花絮) 前面
		if res[0].Mid == 103 {
			t.Errorf("expected main drama to rank higher than bloopers花絮, got Mid 103 at first position")
		}
	})

	// 6. 测试按热度排序切换：当明确指定 sort="hits" 时，按热度排序
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

	// 7. 拼音全拼搜索
	t.Run("PinyinFull_FanRenXiuXian", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "fanrenxiuxian", "", page)
		if len(res) == 0 {
			t.Fatalf("expected results for 'fanrenxiuxian', got 0")
		}
		if res[0].Mid != 106 {
			t.Errorf("expected '凡人修仙传' (Mid=106), got Mid=%d (%s)", res[0].Mid, res[0].Name)
		}
	})

	// 8. 内存索引就绪下不存在片名直接返回空结果，不穿透查库
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

	// 9. ProvideVod 关键词搜索走内存索引，未命中直接返回空
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

	// 10. 管理端搜索：走内存索引与复合条件直接透传
	t.Run("GetSearchPageReadModel_Scenarios", func(t *testing.T) {
		// 纯片名走内存索引
		page := &dto.Page{Current: 1, PageSize: 10}
		res := GetSearchPageReadModel(model.SearchVo{Name: "庆余年", Paging: page})
		if len(res) == 0 {
			t.Fatalf("expected results for '庆余年', got 0")
		}

		// 不存在的片名直接返回空
		pageEmpty := &dto.Page{Current: 1, PageSize: 10}
		missRes := GetSearchPageReadModel(model.SearchVo{Name: "不存在的片名ZZZ000", Paging: pageEmpty})
		if len(missRes) != 0 {
			t.Fatalf("expected 0 results for non-existent film, got %d", len(missRes))
		}

		// 复合条件（例如 plot）透传快照表查询
		pagePlot := &dto.Page{Current: 1, PageSize: 10}
		plotRes := GetSearchPageReadModel(model.SearchVo{Name: "庆余年", Plot: "古装", Paging: pagePlot})
		_ = plotRes // 校验不报错即可
	})

	// 11. 相关推荐候选集倒排词条召回
	t.Run("RelatedCandidates_InvertedIndex", func(t *testing.T) {
		curr := model.FilmListSnapshot{
			Mid:  101,
			Name: "庆余年 第二季",
			Pid:  1,
			Cid:  1,
		}
		cands := loadRelatedSnapshotCandidates(version, curr, 10)
		if len(cands) == 0 {
			t.Fatalf("expected related candidates for 庆余年 第二季, got 0")
		}
		foundRelated := false
		for _, c := range cands {
			if c.Mid == 102 || c.Mid == 103 {
				foundRelated = true
				break
			}
		}
		if !foundRelated {
			t.Errorf("expected related candidate to contain Mid 102 or 103")
		}
	})

	// 12. 校验紧凑内存索引结构体为 48 字节，杜绝内存膨胀
	t.Run("StructSize_48Bytes", func(t *testing.T) {
		sz := unsafe.Sizeof(filmSearchMemoryItem{})
		if sz != 48 {
			t.Errorf("expected unsafe.Sizeof(filmSearchMemoryItem{}) to be 48 bytes, got %d", sz)
		}
	})
}
