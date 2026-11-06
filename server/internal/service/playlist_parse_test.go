package service

import (
	"strconv"
	"strings"
	"testing"

	"server/internal/config"
	"server/internal/model"
	filmsnapshot "server/internal/repository/film/snapshot"

	"gorm.io/gorm"
)

func TestPlaylistParse_SkipsBadLineAndDropsFilmWhenAllFail(t *testing.T) {
	gdb, mr := setupTestDBAndRedis(t)
	const version = "v_playlist_parse"
	seedLiveSnaps(t,
		model.FilmListSnapshot{SnapshotVersion: version, Mid: 401, Name: "全坏", Pid: 1, Cid: 10},
		model.FilmListSnapshot{SnapshotVersion: version, Mid: 402, Name: "有一条能播", Pid: 1, Cid: 10},
		model.FilmListSnapshot{SnapshotVersion: version, Mid: 403, Name: "正常", Pid: 1, Cid: 10},
	)
	insertPlayLine(t, gdb, 401, 0, "。")
	insertPlayLine(t, gdb, 402, 0, "。")
	insertPlayLine(t, gdb, 402, 1, `[{"episode":"1","link":"http://ok"}]`)
	insertPlayLine(t, gdb, 403, 0, `[{"episode":"1","link":"http://good"}]`)

	_ = filmsnapshot.SetActiveSnapshotVersion(version)
	_ = filmsnapshot.LoadActiveFilmReadModel(version)
	filmsnapshot.WaitActiveFilmSearchIndexBuilt()

	if _, err := IndexSvc.GetFilmDetail(401); err == nil || !strings.Contains(err.Error(), "播放线路全部解析失败") {
		t.Fatalf("全部解析失败应返回错误, err=%v", err)
	}
	if mr.Exists(playInfoKey(401)) {
		t.Fatal("全部解析失败的影片不应写入播放缓存")
	}

	mixed, err := IndexSvc.GetFilmDetail(402)
	if err != nil {
		t.Fatalf("还有能播的线路不应失败: %v", err)
	}
	if len(mixed.List) != 1 || len(mixed.List[0].LinkList) != 1 || mixed.List[0].LinkList[0].Link != "http://ok" {
		t.Fatalf("应只保留解析成功的线路: %+v", mixed.List)
	}

	lines, broken, err := BatchGetPlayPlaylistsByMids([]int64{401, 402, 403})
	if err != nil {
		t.Fatal(err)
	}
	if _, bad := broken[401]; !bad {
		t.Fatal("全部解析失败的影片应记入跳过集合")
	}
	if _, bad := broken[402]; bad {
		t.Fatal("仍有成功线路的影片不应跳过")
	}
	if len(lines[402]) != 1 || len(lines[403]) != 1 {
		t.Fatalf("批量结果 lines=%v", lines)
	}
	if _, ok := lines[401]; ok {
		t.Fatal("全部解析失败的影片不应出现在批量结果里")
	}

	details, err := ProvideSvc.GetVodDetail([]string{"401", "402", "403"}, "")
	if err != nil {
		t.Fatal(err)
	}
	if len(details) != 2 || details[0].VodID != 402 || details[1].VodID != 403 {
		t.Fatalf("批量详情应跳过全坏影片, got %+v", details)
	}
	if mr.Exists(playInfoKey(401)) {
		t.Fatal("批量详情不应缓存全坏影片")
	}
}

func insertPlayLine(t *testing.T, gdb *gorm.DB, mid int64, group int, content string) {
	t.Helper()
	err := gdb.Create(&model.FilmSourcePlaylist{
		Mid:        mid,
		SourceId:   "manual",
		LineKind:   model.LineKindPlay,
		GroupIndex: group,
		GroupName:  "线路",
		Content:    content,
	}).Error
	if err != nil {
		t.Fatalf("create playlist mid=%d group=%d: %v", mid, group, err)
	}
}

func playInfoKey(mid int64) string {
	return config.FilmPlayInfoKey + ":" + strconv.FormatInt(mid, 10)
}
