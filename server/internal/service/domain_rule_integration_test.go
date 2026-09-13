package service

import (
	"encoding/json"
	"testing"

	"server/internal/model"
	filmrepo "server/internal/repository/film"
)

func TestBuildPrimaryPlaySources_DomainReplace(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)

	// Create master source with domain replace rule
	sourceID := "src_primary_test"
	gdb.Create(&model.FilmSource{
		Id:                 sourceID,
		Name:               "主站测试源",
		Uri:                "http://test-source.com/api",
		Grade:              model.MasterCollect,
		State:              true,
		DomainReplaceRules: "http://old-cdn.com => https://new-cdn.com\n*.slow-cdn.com => fast-cdn.com",
	})

	snapshot := &model.FilmListSnapshot{
		Mid:      1001,
		SourceId: sourceID,
	}

	detail := &model.MovieDetail{
		Id:       1001,
		PlayFrom: []string{"默认主源"},
		PlayList: [][]model.MovieUrlInfo{
			{
				{Episode: "第01集", Link: "http://old-cdn.com/20230520/1.m3u8"},
				{Episode: "第02集", Link: "http://node1.slow-cdn.com/20230520/2.m3u8"},
				{Episode: "第03集", Link: "http://other-cdn.com/20230520/3.m3u8"},
			},
		},
	}

	sources := buildPrimaryPlaySources(snapshot, detail)
	if len(sources) != 1 {
		t.Fatalf("expected 1 play source, got %d", len(sources))
	}

	links := sources[0].LinkList
	if len(links) != 3 {
		t.Fatalf("expected 3 links, got %d", len(links))
	}

	if links[0].Link != "https://new-cdn.com/20230520/1.m3u8" {
		t.Errorf("link 0 expected https://new-cdn.com/..., got %s", links[0].Link)
	}
	if links[1].Link != "http://fast-cdn.com/20230520/2.m3u8" {
		t.Errorf("link 1 expected http://fast-cdn.com/..., got %s", links[1].Link)
	}
	if links[2].Link != "http://other-cdn.com/20230520/3.m3u8" {
		t.Errorf("link 2 expected untouched http://other-cdn.com/..., got %s", links[2].Link)
	}
}

func TestGetFilmDetail_ChainedRules_NoDoubleSubstitution(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	if err := gdb.AutoMigrate(
		&model.FilmListSnapshot{},
		&model.MovieDetailInfo{},
	); err != nil {
		t.Fatalf("migrate snapshot & detail: %v", err)
	}

	sourceID := "src_chain_test"
	gdb.Create(&model.FilmSource{
		Id:                 sourceID,
		Name:               "链式主站源",
		Uri:                "http://chain-source.com/api",
		Grade:              model.MasterCollect,
		State:              true,
		DomainReplaceRules: "a.com => b.com\nb.com => c.com",
	})

	version := "v_chain_test"
	mid := 2001
	snap := model.FilmListSnapshot{
		SnapshotVersion: version,
		Mid:             int64(mid),
		SourceId:        sourceID,
		Name:            "链式测试影片",
		Pid:             1,
		Cid:             10,
	}
	gdb.Create(&snap)

	origDetail := model.MovieDetail{
		Id:       int64(mid),
		Name:     "链式测试影片",
		PlayFrom: []string{"默认主源"},
		PlayList: [][]model.MovieUrlInfo{
			{
				{Episode: "第01集", Link: "http://a.com/play/1.m3u8"},
			},
		},
		DownloadList: [][]model.MovieUrlInfo{
			{
				{Episode: "第01集", Link: "http://a.com/down/1.mp4"},
			},
		},
	}
	rawDetail, _ := json.Marshal(origDetail)
	gdb.Create(&model.MovieDetailInfo{Mid: int64(mid), Content: string(rawDetail)})

	_ = filmrepo.SetActiveSnapshotVersion(version)
	_ = filmrepo.LoadActiveFilmReadModel(version)
	filmrepo.WaitActiveFilmSearchIndexBuilt()

	res, err := IndexSvc.GetFilmDetail(mid)
	if err != nil {
		t.Fatalf("GetFilmDetail failed: %v", err)
	}

	// 验证链接仅被替换一次变为 b.com，绝对不能二次跳跃变为 c.com！
	if len(res.List) == 0 || len(res.List[0].LinkList) == 0 {
		t.Fatalf("expected play list, got %+v", res.List)
	}
	actualLink := res.List[0].LinkList[0].Link
	if actualLink != "http://b.com/play/1.m3u8" {
		t.Errorf("expected single replacement 'http://b.com/play/1.m3u8', but got %q (possible double substitution!)", actualLink)
	}

	// 验证 res.PlayList 同样保持单次重写结果
	if len(res.PlayList) > 0 && len(res.PlayList[0]) > 0 {
		if res.PlayList[0][0].Link != "http://b.com/play/1.m3u8" {
			t.Errorf("res.PlayList expected 'http://b.com/play/1.m3u8', got %q", res.PlayList[0][0].Link)
		}
	}
	if len(res.DownloadList) == 0 || len(res.DownloadList[0]) == 0 {
		t.Fatalf("expected DownloadList, got %+v", res.DownloadList)
	}
	if res.DownloadList[0][0].Link != "http://b.com/down/1.mp4" {
		t.Errorf("DownloadList expected 'http://b.com/down/1.mp4', got %q", res.DownloadList[0][0].Link)
	}
}

func TestGetFilmDetail_EmptyFirstGroup_NoIndexMisalignment(t *testing.T) {
	gdb := setupCollectServiceTestDB(t)
	if err := gdb.AutoMigrate(
		&model.FilmListSnapshot{},
		&model.MovieDetailInfo{},
	); err != nil {
		t.Fatalf("migrate snapshot & detail: %v", err)
	}

	sourceID := "src_empty_first_test"
	gdb.Create(&model.FilmSource{
		Id:                 sourceID,
		Name:               "空前置测试源",
		Uri:                "http://empty-first.com/api",
		Grade:              model.MasterCollect,
		State:              true,
		DomainReplaceRules: "http://old.com => https://new.com",
	})

	version := "v_empty_first_test"
	mid := 3001
	snap := model.FilmListSnapshot{
		SnapshotVersion: version,
		Mid:             int64(mid),
		SourceId:        sourceID,
		Name:            "空前置影片",
		Pid:             1,
		Cid:             10,
	}
	gdb.Create(&snap)

	origDetail := model.MovieDetail{
		Id:       int64(mid),
		Name:     "空前置影片",
		PlayFrom: []string{"预告源", "正片源"},
		PlayList: [][]model.MovieUrlInfo{
			{}, // 第 0 组为空
			{
				{Episode: "第01集", Link: "http://old.com/play/1.m3u8"},
			},
		},
	}
	rawDetail, _ := json.Marshal(origDetail)
	gdb.Create(&model.MovieDetailInfo{Mid: int64(mid), Content: string(rawDetail)})

	_ = filmrepo.SetActiveSnapshotVersion(version)
	_ = filmrepo.LoadActiveFilmReadModel(version)
	filmrepo.WaitActiveFilmSearchIndexBuilt()

	res, err := IndexSvc.GetFilmDetail(mid)
	if err != nil {
		t.Fatalf("GetFilmDetail failed: %v", err)
	}

	// res.List 应该只包含有效组（正片源）
	if len(res.List) != 1 {
		t.Fatalf("expected 1 play group in res.List, got %d", len(res.List))
	}
	if res.List[0].LinkList[0].Link != "https://new.com/play/1.m3u8" {
		t.Errorf("expected res.List[0] link replaced to https://new.com/..., got %s", res.List[0].LinkList[0].Link)
	}

	// 关键断言：res.PlayList 必须保持原长度 2，第 0 组保持为空，第 1 组替换为 new.com，绝不发生错位！
	if len(res.PlayList) != 2 {
		t.Fatalf("expected len(res.PlayList) == 2, got %d", len(res.PlayList))
	}
	if len(res.PlayList[0]) != 0 {
		t.Errorf("expected res.PlayList[0] to remain empty, got %v", res.PlayList[0])
	}
	if len(res.PlayList[1]) != 1 || res.PlayList[1][0].Link != "https://new.com/play/1.m3u8" {
		t.Errorf("expected res.PlayList[1][0].Link to be https://new.com/..., got %+v", res.PlayList[1])
	}
}
