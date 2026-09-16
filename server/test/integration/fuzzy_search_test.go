package integration

import (
	"testing"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/film/writer"
)

// TestFuzzySearchScenarios 覆盖前台检索的模糊匹配、排序、空结果与只读模型边界。
func TestFuzzySearchScenarios(t *testing.T) {
	gdb := newTestDB(t)
	version := "v_it_fuzzy"
	now := time.Now().Unix()
	seedSnapshots(t, gdb, version,
		model.FilmListSnapshot{Mid: 101, Pid: 1, Cid: 10, SeriesKey: "qingyunian", Name: "庆余年 第二季", Hits: 9500, Score: 8.8, Year: 2024, UpdateStamp: now - 1000},
		model.FilmListSnapshot{Mid: 102, Pid: 1, Cid: 10, SeriesKey: "qingyunian", Name: "庆余年 第一季", Hits: 8000, Score: 9.0, Year: 2019, UpdateStamp: now - 5000},
		model.FilmListSnapshot{Mid: 103, Pid: 1, Cid: 10, Name: "关于庆余年的拍摄花絮与解说", Hits: 12000, Score: 6.0, Year: 2024, UpdateStamp: now},
		model.FilmListSnapshot{Mid: 104, Pid: 1, Cid: 11, Name: "流浪地球2", Hits: 18000, Score: 9.2, Year: 2023, UpdateStamp: now - 2000},
		model.FilmListSnapshot{Mid: 105, Pid: 2, Cid: 20, Name: "哈利·波特与魔法石", Hits: 15000, Score: 9.5, Year: 2001, UpdateStamp: now - 8000},
		model.FilmListSnapshot{Mid: 106, Pid: 2, Cid: 21, Name: "凡人修仙传", Hits: 14000, Score: 9.1, Year: 2020, UpdateStamp: now - 300},
		model.FilmListSnapshot{Mid: 107, Pid: 3, Cid: 30, Name: "少林足球", Hits: 16000, Score: 8.9, Year: 2001, UpdateStamp: now - 9000},
		model.FilmListSnapshot{Mid: 108, Pid: 3, Cid: 31, Name: "星际穿越", Hits: 22000, Score: 9.6, Year: 2014, UpdateStamp: now - 100},
	)
	activateVersion(t, version)

	t.Run("KeywordSearch_QingYuNian", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := filmsnapshot.SearchSnapshotsByKeywordAndSortReadModel(version, "庆余年", "", page)
		if len(res) == 0 {
			t.Fatal("关键词「庆余年」应有命中结果")
		}
		if page.Total < 3 {
			t.Errorf("关键词「庆余年」应至少命中 3 条，实际 %d", page.Total)
		}
	})

	t.Run("SpaceToken_LiuLangDiQiu2", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := filmsnapshot.SearchSnapshotsByKeywordAndSortReadModel(version, "流浪地球 2", "", page)
		if len(res) == 0 {
			t.Fatal("关键词「流浪地球 2」应有命中结果")
		}
		if res[0].Mid != 104 {
			t.Errorf("「流浪地球 2」首位应为 Mid=104，实际 Mid=%d（%s）", res[0].Mid, res[0].Name)
		}
	})

	t.Run("SortByHits", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := filmsnapshot.SearchSnapshotsByKeywordAndSortReadModel(version, "庆余年", "hits", page)
		if len(res) < 2 {
			t.Fatalf("按热度排序应至少 2 条，实际 %d", len(res))
		}
		if res[0].Hits < res[1].Hits {
			t.Errorf("应按热度降序，实际 %d < %d", res[0].Hits, res[1].Hits)
		}
	})

	t.Run("NonExistent_Film_Returns_Empty", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		res := filmsnapshot.SearchSnapshotsByKeywordAndSortReadModel(version, "绝对不可能存在的片名XYZ12345", "", page)
		if len(res) != 0 {
			t.Fatalf("不存在的片名应返回 0 条，实际 %d", len(res))
		}
		if page.Total != 0 || page.PageCount != 1 {
			t.Fatalf("空结果分页态应为 total=0 pageCount=1，实际 total=%d pageCount=%d", page.Total, page.PageCount)
		}
	})

	t.Run("ProvideVod_Search_And_Empty", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		if res := filmsnapshot.ListProvideSnapshotsReadModel(version, model.SearchTagsVO{}, "庆余年", 0, page); len(res) == 0 {
			t.Fatal("OpenAPI 检索「庆余年」应有命中结果")
		}

		emptyPage := &dto.Page{Current: 1, PageSize: 10}
		if res := filmsnapshot.ListProvideSnapshotsReadModel(version, model.SearchTagsVO{}, "不存在的片名ABC999", 0, emptyPage); len(res) != 0 || emptyPage.Total != 0 {
			t.Fatalf("OpenAPI 检索不存在的片名应为空，实际 %d 条（total=%d）", len(res), emptyPage.Total)
		}
	})

	t.Run("GetSearchPageReadModel_Scenarios", func(t *testing.T) {
		page := &dto.Page{Current: 1, PageSize: 10}
		if res := filmsnapshot.GetSearchPageReadModel(model.SearchVo{Name: "庆余年", Paging: page}); len(res) == 0 {
			t.Fatal("管理端检索「庆余年」应有命中结果")
		}

		emptyPage := &dto.Page{Current: 1, PageSize: 10}
		if res := filmsnapshot.GetSearchPageReadModel(model.SearchVo{Name: "不存在的片名ZZZ000", Paging: emptyPage}); len(res) != 0 {
			t.Fatalf("管理端检索不存在的片名应为空，实际 %d 条", len(res))
		}

		plotPage := &dto.Page{Current: 1, PageSize: 10}
		if res := filmsnapshot.GetSearchPageReadModel(model.SearchVo{Name: "庆余年", Plot: "古装", Paging: plotPage}); len(res) != 0 {
			t.Fatalf("叠加无匹配剧情条件后应为空，实际 %d 条", len(res))
		}
	})

	t.Run("RelatedCandidates_SeriesAndCategory", func(t *testing.T) {
		curr := model.FilmListSnapshot{Mid: 101, SeriesKey: "qingyunian", Name: "庆余年 第二季", Pid: 1, Cid: 10}
		page := &dto.Page{Current: 1, PageSize: 10}
		cands := filmsnapshot.ListRelatedSnapshotsReadModel(version, curr, page)
		if len(cands) == 0 {
			t.Fatal("「庆余年 第二季」应有相关推荐候选")
		}
		if !midSet(cands)[102] {
			t.Errorf("相关推荐候选应包含同系列兄弟 Mid=102，实际 %v", midSet(cands))
		}
	})

	t.Run("ListRelatedSnapshotsReadModel_EndToEndPaging", func(t *testing.T) {
		curr := model.FilmListSnapshot{Mid: 101, SeriesKey: "qingyunian", Name: "庆余年 第二季", Pid: 1, Cid: 10}

		page1 := &dto.Page{Current: 1, PageSize: 1}
		res1 := filmsnapshot.ListRelatedSnapshotsReadModel(version, curr, page1)
		if len(res1) != 1 {
			t.Fatalf("第 1 页应有 1 条，实际 %d 条", len(res1))
		}
		if page1.Total < 2 || page1.PageCount < 2 {
			t.Fatalf("第 1 页分页态应 total>=2 且 pageCount>=2，实际 total=%d pageCount=%d", page1.Total, page1.PageCount)
		}

		page2 := &dto.Page{Current: 2, PageSize: 1}
		res2 := filmsnapshot.ListRelatedSnapshotsReadModel(version, curr, page2)
		if len(res2) != 1 {
			t.Fatalf("第 2 页应有 1 条，实际 %d 条", len(res2))
		}
		if res1[0].Mid == res2[0].Mid {
			t.Fatalf("跨页结果不应重复，均为 Mid=%d", res1[0].Mid)
		}
	})

	t.Run("EmptyVersionAndBoundary_PaginationState", func(t *testing.T) {
		// 模拟冷启动：清空活跃版本与读模型，仅靠显式传入的空 version 走边界分支
		filmsnapshot.ClearActiveFilmReadModel()
		filmsnapshot.ResetActiveSnapshotFallbackForTest()
		t.Cleanup(func() {
			if err := filmsnapshot.SetActiveSnapshotVersion(version); err != nil {
				t.Fatalf("恢复活跃快照版本失败: %v", err)
			}
			if err := filmsnapshot.LoadActiveFilmReadModel(version); err != nil {
				t.Fatalf("恢复活跃读模型失败: %v", err)
			}
			filmsnapshot.WaitActiveFilmSearchIndexBuilt()
		})

		p1 := &dto.Page{Current: 1, PageSize: 10}
		if r := filmsnapshot.SearchSnapshotsByKeywordAndSortReadModel("", "庆余年", "", p1); len(r) != 0 || p1.Total != 0 || p1.PageCount != 1 {
			t.Errorf("SearchSnapshots 空版本应返回空且分页合法：len=%d total=%d pageCount=%d", len(r), p1.Total, p1.PageCount)
		}

		p2 := &dto.Page{Current: 1, PageSize: 10}
		if r := filmsnapshot.ListRelatedSnapshotsReadModel("", model.FilmListSnapshot{Mid: 101}, p2); len(r) != 0 || p2.Total != 0 || p2.PageCount != 1 {
			t.Errorf("ListRelatedSnapshots 空版本应返回空且分页合法：len=%d total=%d pageCount=%d", len(r), p2.Total, p2.PageCount)
		}

		p3 := &dto.Page{Current: 1, PageSize: 10}
		if r := filmsnapshot.ListRelatedSnapshotsReadModel(version, model.FilmListSnapshot{Mid: 0}, p3); len(r) != 0 || p3.Total != 0 || p3.PageCount != 1 {
			t.Errorf("ListRelatedSnapshots mid=0 应返回空且分页合法：len=%d total=%d pageCount=%d", len(r), p3.Total, p3.PageCount)
		}

		p4 := &dto.Page{Current: 1, PageSize: 10}
		if r := filmsnapshot.ListProvideSnapshotsReadModel("", model.SearchTagsVO{}, "庆余年", 0, p4); len(r) != 0 || p4.Total != 0 || p4.PageCount != 1 {
			t.Errorf("ListProvideSnapshots 空版本应返回空且分页合法：len=%d total=%d pageCount=%d", len(r), p4.Total, p4.PageCount)
		}

		p5 := &dto.Page{Current: 1, PageSize: 10}
		if r := filmsnapshot.ListFilmSnapshotsByTagsReadModel("", model.SearchTagsVO{}, p5); len(r) != 0 || p5.Total != 0 || p5.PageCount != 1 {
			t.Errorf("ListFilmSnapshotsByTags 空版本应返回空且分页合法：len=%d total=%d pageCount=%d", len(r), p5.Total, p5.PageCount)
		}
	})
}

// 快速增量发布后，内存读模型必须在同版本下立即可见，无需重启。
func TestIncrementalPublishReadModelFreshness(t *testing.T) {
	gdb := newTestDB(t)
	version := "v_it_freshness"
	activateVersion(t, version)

	seedSnapshots(t, gdb, version, model.FilmListSnapshot{Mid: 9001, Pid: 1, Cid: 10, Name: "流浪地球", UpdateStamp: time.Now().Unix()})
	if err := filmsnapshot.ApplyActiveFilmReadModelSnapshots(version, nil, nil); err != nil {
		t.Fatalf("ApplyActiveFilmReadModelSnapshots 失败: %v", err)
	}
	filmsnapshot.WaitActiveFilmSearchIndexBuilt()

	rows, page := searchFilms(version, "流浪", 10)
	assertHit(t, "增量发布后", "流浪", rows, page, 9001)

	seedSnapshots(t, gdb, version, model.FilmListSnapshot{Mid: 9002, Pid: 1, Cid: 10, Name: "黑客帝国", UpdateStamp: time.Now().Unix()})
	if err := filmsnapshot.ApplyActiveFilmReadModelSnapshots(version, nil, nil); err != nil {
		t.Fatalf("ApplyActiveFilmReadModelSnapshots 失败: %v", err)
	}
	filmsnapshot.WaitActiveFilmSearchIndexBuilt()

	rows, page = searchFilms(version, "黑客", 10)
	assertHit(t, "增量发布后", "黑客", rows, page, 9002)
}

// 反复触发标签刷新必须保持幂等，分值不得随采集次数累加虚增。
func TestRefreshSearchTagsByMidsIdempotency(t *testing.T) {
	gdb := newTestDB(t)

	film := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 8888},
		FilmIndexCategory: model.FilmIndexCategory{Pid: 1, Cid: 10},
		FilmIndexContent:  model.FilmIndexContent{Name: "凡人修仙传", ClassTag: "玄幻,动作", Year: 2024},
	}
	if err := gdb.Create(&film).Error; err != nil {
		t.Fatalf("写入影片失败: %v", err)
	}

	if err := writer.RefreshSearchTagsByMids(8888); err != nil {
		t.Fatalf("首次 RefreshSearchTagsByMids 失败: %v", err)
	}

	var firstTag model.SearchTagItem
	if err := gdb.Where("pid = ? AND tag_type = ? AND value = ?", 1, "Plot", "玄幻").First(&firstTag).Error; err != nil {
		t.Fatalf("查询首次标签失败: %v", err)
	}
	if firstTag.Score <= 0 {
		t.Fatalf("首次标签分值应为正数，实际 %d", firstTag.Score)
	}

	for i := 0; i < 2; i++ {
		if err := writer.RefreshSearchTagsByMids(8888); err != nil {
			t.Fatalf("重复 RefreshSearchTagsByMids 失败: %v", err)
		}
	}

	var repeatedTag model.SearchTagItem
	if err := gdb.Where("pid = ? AND tag_type = ? AND value = ?", 1, "Plot", "玄幻").First(&repeatedTag).Error; err != nil {
		t.Fatalf("查询重复刷新后标签失败: %v", err)
	}
	if repeatedTag.Score != firstTag.Score {
		t.Fatalf("标签刷新不幂等：首次=%d 重复后=%d", firstTag.Score, repeatedTag.Score)
	}
}
