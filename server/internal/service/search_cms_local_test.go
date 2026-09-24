package service

import (
	"testing"

	"server/internal/model"
	"server/internal/repository/support"
)

func withXianNiCategories(t *testing.T) {
	t.Helper()
	support.SetCategoryTreeForTest(map[int64]int64{
		20: 0, 21: 20, 34: 0, 35: 34,
	}, map[int64]string{
		20: model.BigCategoryAnimation,
		21: "中国动漫",
		34: model.BigCategoryShortFilm,
		35: "古装仙侠",
	})
}

func xianNiAnimeSnap() model.FilmListSnapshot {
	return model.FilmListSnapshot{
		Mid: 47014, Name: "仙逆", CName: "中国动漫", Year: 2023, Remarks: "第159集",
		Director: "石头熊,冯毅", Pid: 20, Cid: 21,
	}
}

func xianNiDramaSnap() model.FilmListSnapshot {
	return model.FilmListSnapshot{
		Mid: 126574, Name: "仙逆", CName: "古装仙侠", Year: 0, Remarks: "全集完结",
		Pid: 34, Cid: 35,
	}
}

func xianNiCards() []model.MovieBasicInfo {
	return []model.MovieBasicInfo{
		{Name: "仙逆", CName: "中国动漫", Year: "2023", Remarks: "第159集", Director: "石头熊,冯毅", SourceId: "subo", SourceMid: 101},
		{Name: "仙逆", CName: "古装仙侠", Year: "2025", Remarks: "全集完结", SourceId: "subo", SourceMid: 202},
	}
}

func TestAssignCMSSearchLocalIDsDirtySharedMappingSplitsWhenAnimeInPool(t *testing.T) {
	withXianNiCategories(t)
	anime, drama := xianNiAnimeSnap(), xianNiDramaSnap()
	cards := xianNiCards()
	assignCMSSearchLocalIDs(
		cards,
		map[int64]int64{101: 126574, 202: 126574},
		map[int64]model.FilmListSnapshot{126574: drama},
		nil,
		map[int64]model.FilmListSnapshot{47014: anime, 126574: drama},
	)
	if cards[0].Id != 47014 {
		t.Fatalf("脏映射的动漫卡应回退到 47014, got %d", cards[0].Id)
	}
	if cards[1].Id != 126574 {
		t.Fatalf("短剧卡应保留 126574, got %d", cards[1].Id)
	}
}

func TestAssignCMSSearchLocalIDsDirtySharedMappingKeepsDramaWhenAnimeMissing(t *testing.T) {
	withXianNiCategories(t)
	drama := xianNiDramaSnap()
	cards := xianNiCards()
	assignCMSSearchLocalIDs(
		cards,
		map[int64]int64{101: 126574, 202: 126574},
		map[int64]model.FilmListSnapshot{126574: drama},
		nil,
		map[int64]model.FilmListSnapshot{126574: drama},
	)
	if cards[0].Id != 0 {
		t.Fatalf("池里没有动漫时脏映射动漫卡应 live, got %d", cards[0].Id)
	}
	if cards[1].Id != 126574 {
		t.Fatalf("短剧卡不得被 claimed 清掉, got %d", cards[1].Id)
	}
}

func TestAssignCMSSearchLocalIDsRejectedMappingMustNotRebind(t *testing.T) {
	withXianNiCategories(t)
	drama := xianNiDramaSnap()
	cards := xianNiCards()[:1]
	assignCMSSearchLocalIDs(
		cards,
		map[int64]int64{101: 126574},
		map[int64]model.FilmListSnapshot{126574: drama},
		nil,
		map[int64]model.FilmListSnapshot{126574: drama},
	)
	if cards[0].Id != 0 {
		t.Fatalf("映射否决后只剩短剧不得把动漫绑成 126574, got %d", cards[0].Id)
	}
}
