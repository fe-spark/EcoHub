package integration

import (
	"encoding/json"
	"testing"

	"server/internal/model"
	"server/internal/service"
)

// seedDetailFilm 写入主站源、快照与原始详情，并激活该快照版本。
func seedDetailFilm(t *testing.T, version string, mid int64, sourceID string, rules string, detail model.MovieDetail) {
	t.Helper()

	gdb := newTestDB(t)
	gdb.Create(&model.FilmSource{
		Id:                 sourceID,
		Name:               "主站测试源",
		Uri:                "http://test-source.com/api",
		Grade:              model.MasterCollect,
		State:              true,
		DomainReplaceRules: rules,
	})
	seedSnapshots(t, gdb, version, model.FilmListSnapshot{
		SnapshotVersion: version,
		Mid:             mid,
		SourceId:        sourceID,
		Name:            detail.Name,
		Pid:             1,
		Cid:             10,
	})

	raw, err := json.Marshal(detail)
	if err != nil {
		t.Fatalf("序列化详情失败: %v", err)
	}
	if err := gdb.Create(&model.MovieDetailInfo{Mid: mid, Content: string(raw)}).Error; err != nil {
		t.Fatalf("写入详情失败: %v", err)
	}

	activateVersion(t, version)
}

// 域名替换规则应按顺序生效一次：精确规则命中、通配规则命中、未命中规则保持原样。
func TestGetFilmDetail_DomainReplaceRules(t *testing.T) {
	const (
		version  = "v_it_domain_replace"
		mid      = int64(1001)
		sourceID = "src_primary_test"
	)
	seedDetailFilm(t, version, mid, sourceID, "http://old-cdn.com => https://new-cdn.com\n*.slow-cdn.com => fast-cdn.com", model.MovieDetail{
		Id:       mid,
		Name:     "域名替换测试影片",
		PlayFrom: []string{"默认主源"},
		PlayList: [][]model.MovieUrlInfo{
			{
				{Episode: "第01集", Link: "http://old-cdn.com/20230520/1.m3u8"},
				{Episode: "第02集", Link: "http://node1.slow-cdn.com/20230520/2.m3u8"},
				{Episode: "第03集", Link: "http://other-cdn.com/20230520/3.m3u8"},
			},
		},
	})

	res, err := service.IndexSvc.GetFilmDetail(int(mid))
	if err != nil {
		t.Fatalf("GetFilmDetail 失败: %v", err)
	}
	if len(res.List) != 1 {
		t.Fatalf("应返回 1 个播放分组，实际 %d 个", len(res.List))
	}

	links := res.List[0].LinkList
	if len(links) != 3 {
		t.Fatalf("播放分组应有 3 条线路，实际 %d 条", len(links))
	}
	want := []string{
		"https://new-cdn.com/20230520/1.m3u8",
		"http://fast-cdn.com/20230520/2.m3u8",
		"http://other-cdn.com/20230520/3.m3u8",
	}
	for i, link := range links {
		if link.Link != want[i] {
			t.Errorf("第 %d 条线路应为 %s，实际 %s", i+1, want[i], link.Link)
		}
	}
}

// 链式域名规则只能替换一次，不得因规则级联发生二次替换。
func TestGetFilmDetail_ChainedRules_NoDoubleSubstitution(t *testing.T) {
	const (
		version  = "v_it_chain_rule"
		mid      = int64(2001)
		sourceID = "src_chain_test"
	)
	seedDetailFilm(t, version, mid, sourceID, "a.com => b.com\nb.com => c.com", model.MovieDetail{
		Id:       mid,
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
	})

	res, err := service.IndexSvc.GetFilmDetail(int(mid))
	if err != nil {
		t.Fatalf("GetFilmDetail 失败: %v", err)
	}

	if len(res.List) == 0 || len(res.List[0].LinkList) == 0 {
		t.Fatalf("应返回播放列表，实际 %+v", res.List)
	}
	if got := res.List[0].LinkList[0].Link; got != "http://b.com/play/1.m3u8" {
		t.Errorf("播放链接应单次替换为 http://b.com/play/1.m3u8，实际 %q（疑似二次替换）", got)
	}

	if len(res.PlayList) > 0 && len(res.PlayList[0]) > 0 {
		if got := res.PlayList[0][0].Link; got != "http://b.com/play/1.m3u8" {
			t.Errorf("PlayList 应保持单次替换结果 http://b.com/play/1.m3u8，实际 %q", got)
		}
	}

	if len(res.DownloadList) == 0 || len(res.DownloadList[0]) == 0 {
		t.Fatalf("应返回下载列表，实际 %+v", res.DownloadList)
	}
	if got := res.DownloadList[0][0].Link; got != "http://b.com/down/1.mp4" {
		t.Errorf("下载链接应单次替换为 http://b.com/down/1.mp4，实际 %q", got)
	}
}

// 首个播放分组为空时，有效分组不得发生索引错位。
func TestGetFilmDetail_EmptyFirstGroup_NoIndexMisalignment(t *testing.T) {
	const (
		version  = "v_it_empty_first_group"
		mid      = int64(3001)
		sourceID = "src_empty_first_test"
	)
	seedDetailFilm(t, version, mid, sourceID, "http://old.com => https://new.com", model.MovieDetail{
		Id:       mid,
		Name:     "空前置影片",
		PlayFrom: []string{"预告源", "正片源"},
		PlayList: [][]model.MovieUrlInfo{
			{},
			{
				{Episode: "第01集", Link: "http://old.com/play/1.m3u8"},
			},
		},
	})

	res, err := service.IndexSvc.GetFilmDetail(int(mid))
	if err != nil {
		t.Fatalf("GetFilmDetail 失败: %v", err)
	}

	if len(res.List) != 1 {
		t.Fatalf("res.List 应只包含有效分组，实际 %d 个", len(res.List))
	}
	if got := res.List[0].LinkList[0].Link; got != "https://new.com/play/1.m3u8" {
		t.Errorf("有效分组链接应替换为 https://new.com/play/1.m3u8，实际 %s", got)
	}

	if len(res.PlayList) != 2 {
		t.Fatalf("res.PlayList 应保持原始长度 2，实际 %d", len(res.PlayList))
	}
	if len(res.PlayList[0]) != 0 {
		t.Errorf("res.PlayList[0] 应保持为空，实际 %+v", res.PlayList[0])
	}
	if len(res.PlayList[1]) != 1 || res.PlayList[1][0].Link != "https://new.com/play/1.m3u8" {
		t.Errorf("res.PlayList[1][0] 应为 https://new.com/play/1.m3u8，实际 %+v", res.PlayList[1])
	}
}
