package film

import (
	"fmt"
	"testing"
	"time"

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
		SubTitle    string
		Actor       string
		Director    string
		Hits        int64
		Score       float64
		Year        int64
		UpdateStamp int64
	}
	if err := gdb.Model(&model.FilmListSnapshot{}).
		Select("mid, pid, cid, name, sub_title, actor, director, hits, score, year, update_stamp").
		Scan(&rows).Error; err != nil {
		t.Fatalf("scan: %v", err)
	}
	raw := make([]filmSearchIndexRow, 0, len(rows))
	for _, r := range rows {
		raw = append(raw, filmSearchIndexRow{
			Mid: r.Mid, Pid: r.Pid, Cid: r.Cid, Name: r.Name,
			SubTitle: r.SubTitle, Actor: r.Actor, Director: r.Director,
			Hits: r.Hits, Score: r.Score, Year: r.Year, UpdateStamp: r.UpdateStamp,
		})
	}
	idx := &filmSearchMemoryIndex{Items: parallelBuildItems(raw)}
	idx.buildInverted()
	t.Logf("Loaded %d items from database", len(idx.Items))

	matchedCeshi := scoreMemoryIndex(idx, "ceshi", "", 0, 0)
	for _, m := range matchedCeshi {
		if m.mid == 148491 { // 选择之她·他
			t.Fatalf("search 'ceshi' should NOT match '选择之她·他' (Her Choices, His Decision)")
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
			SubTitle:        "Joy of Life 2",
			Actor:           "张若昀 / 李沁 / 陈道明",
			Director:        "孙皓",
			Hits:            9500,
			Score:           8.8,
			Year:            2024,
			UpdateStamp:     now - 1000,
		},
		{
			SnapshotVersion: version,
			Mid:             102,
			Name:            "庆余年 第一季",
			SubTitle:        "Joy of Life 1",
			Actor:           "张若昀 / 李沁 / 陈道明",
			Director:        "孙皓",
			Hits:            8000,
			Score:           9.0,
			Year:            2019,
			UpdateStamp:     now - 5000,
		},
		{
			SnapshotVersion: version,
			Mid:             103,
			Name:            "关于庆余年的拍摄花絮与解说",
			SubTitle:        "花絮",
			Actor:           "解说员",
			Director:        "未知",
			Hits:            12000,
			Score:           6.0,
			Year:            2024,
			UpdateStamp:     now,
		},
		{
			SnapshotVersion: version,
			Mid:             104,
			Name:            "流浪地球2",
			SubTitle:        "小破球2 / The Wandering Earth II",
			Actor:           "吴京 / 刘德华 / 李雪健 / 沙溢",
			Director:        "郭帆",
			Hits:            18000,
			Score:           9.2,
			Year:            2023,
			UpdateStamp:     now - 2000,
		},
		{
			SnapshotVersion: version,
			Mid:             105,
			Name:            "哈利·波特与魔法石",
			SubTitle:        "Harry Potter and the Sorcerer's Stone",
			Actor:           "丹尼尔·雷德克里夫 / 艾玛·沃森",
			Director:        "克里斯·哥伦布",
			Hits:            15000,
			Score:           9.5,
			Year:            2001,
			UpdateStamp:     now - 8000,
		},
		{
			SnapshotVersion: version,
			Mid:             106,
			Name:            "凡人修仙传",
			SubTitle:        "A Record of a Mortal's Journey to Immortality",
			Actor:           "钱文青 / 杨天翔",
			Director:        "王裕仁",
			Hits:            14000,
			Score:           9.1,
			Year:            2020,
			UpdateStamp:     now - 300,
		},
		{
			SnapshotVersion: version,
			Mid:             107,
			Name:            "少林足球",
			SubTitle:        "Shaolin Soccer",
			Actor:           "周星驰 / 赵薇 / 吴孟达",
			Director:        "周星驰",
			Hits:            16000,
			Score:           8.9,
			Year:            2001,
			UpdateStamp:     now - 9000,
		},
		{
			SnapshotVersion: version,
			Mid:             108,
			Name:            "星际穿越",
			SubTitle:        "Interstellar",
			Actor:           "马修·麦康纳 / 安妮·海瑟薇",
			Director:        "克里斯托弗·诺兰 / Christopher Nolan",
			Hits:            22000,
			Score:           9.6,
			Year:            2014,
			UpdateStamp:     now - 100,
		},
		{
			SnapshotVersion: version,
			Mid:             109,
			Name:            "斗罗大陆：诛神之战",
			SubTitle:        "斗罗大陆 / 凡人修仙传 / 斗破苍穹 / 武动乾坤",
			Actor:           "唐三 / 小舞 / 凡人修仙传 / 萧炎",
			Director:        "未知",
			Hits:            99999,
			Score:           7.0,
			Year:            2018,
			UpdateStamp:     now,
		},
	}

	for _, f := range testFilms {
		if err := gdb.Create(&f).Error; err != nil {
			t.Fatalf("create test film: %v", err)
		}
	}

	oldDB := db.Mdb
	oldIdx := activeFilmSearchIndex.Load()
	db.Mdb = gdb
	t.Cleanup(func() {
		db.Mdb = oldDB
		if oldIdx != nil {
			activeFilmSearchIndex.Store(oldIdx)
		} else {
			activeFilmSearchIndex.Store(&filmSearchMemoryIndex{Version: ""})
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

	// 5. 测试别名召回: "小破球"
	t.Run("SubTitleAlias_XiaoPoQiu", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "小破球", "", page)
		if len(res) == 0 {
			t.Fatalf("expected results for '小破球', got 0")
		}
		if res[0].Mid != 104 {
			t.Errorf("expected top result to be '流浪地球2' (Mid=104), got Mid=%d (%s)", res[0].Mid, res[0].Name)
		}
	})

	// 6. 测试主演搜索: "周星驰"
	t.Run("Actor_ZhouXingChi", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "周星驰", "", page)
		if len(res) == 0 {
			t.Fatalf("expected results for '周星驰', got 0")
		}
		if res[0].Mid != 107 {
			t.Errorf("expected top result to be '少林足球' (Mid=107), got Mid=%d (%s)", res[0].Mid, res[0].Name)
		}
	})

	// 7. 测试导演搜索: "诺兰"
	t.Run("Director_Nolan", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "诺兰", "", page)
		if len(res) == 0 {
			t.Fatalf("expected results for '诺兰', got 0")
		}
		if res[0].Mid != 108 {
			t.Errorf("expected top result to be '星际穿越' (Mid=108), got Mid=%d (%s)", res[0].Mid, res[0].Name)
		}
	})

	// 8. 测试相关度排序优先：搜索 "庆余年"，即使花絮 (Mid=103) 更新时间和热度最高，正剧 (Mid=101/102) 必须排在花絮前面
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

	// 9. 测试 YouTube 风格排序切换：当明确指定 sort="hits" 时，按热度排序
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

	// 10. 相关作品墙不得把热门片顶到「凡人修仙传」前面
	t.Run("RelatedTitleDump_DoesNotMatchFanRen", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "凡人修仙传", "", page)
		if len(res) == 0 {
			t.Fatalf("expected results for '凡人修仙传', got 0")
		}
		if res[0].Mid != 106 {
			t.Errorf("expected top result 凡人修仙传 (Mid=106), got Mid=%d (%s)", res[0].Mid, res[0].Name)
		}
		for _, item := range res {
			if item.Mid == 109 {
				t.Errorf("斗罗大陆 related-title dump should not appear for '凡人修仙传'")
			}
		}
	})

	// 11. 跨字段 AND：片名 + 主演
	t.Run("CrossField_NameAndActor", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "庆余年 张若昀", "", page)
		if len(res) == 0 {
			t.Fatalf("expected results for '庆余年 张若昀', got 0")
		}
		if res[0].Mid != 101 && res[0].Mid != 102 {
			t.Errorf("expected 庆余年 drama, got Mid=%d (%s)", res[0].Mid, res[0].Name)
		}
	})

	t.Run("EnglishPhrase_JoyOfLife", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "joy of life", "", page)
		if len(res) == 0 {
			t.Fatalf("expected results for 'joy of life', got 0")
		}
		if res[0].Mid != 101 && res[0].Mid != 102 {
			t.Errorf("expected 庆余年 for 'joy of life', got Mid=%d (%s)", res[0].Mid, res[0].Name)
		}
	})

	t.Run("AsciiPrefix_Nola", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := SearchSnapshotsByKeywordAndSortReadModel(version, "nola", "", page)
		if len(res) == 0 {
			t.Fatalf("expected results for 'nola', got 0")
		}
		if res[0].Mid != 108 {
			t.Errorf("expected 星际穿越 for 'nola', got Mid=%d (%s)", res[0].Mid, res[0].Name)
		}
	})
}
