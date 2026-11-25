package writer

import (
	"errors"
	"testing"

	"server/internal/model"
)

func TestCategorySkipKind(t *testing.T) {
	if categorySkipKind(nil) != skipKindNone {
		t.Fatal("nil")
	}
	if categorySkipKind(ErrCategoryNotMapped) != skipKindUnmapped {
		t.Fatal("unmapped")
	}
	if categorySkipKind(errors.New("影片未映射到分类: 冷门片")) != skipKindUnmapped {
		t.Fatal("unmapped text")
	}
	if categorySkipKind(errors.New("影片分类已停用")) != skipKindNone {
		t.Fatal("hidden category is no longer a skip")
	}
}

func TestCountCategorySkips(t *testing.T) {
	unmapped := countCategorySkips([]peerPageItem{
		{skipKind: skipKindUnmapped},
		{skipKind: skipKindNone},
		{skipKind: skipKindUnmapped},
	})
	if unmapped != 2 {
		t.Fatalf("unmapped=%d", unmapped)
	}
}

func TestDropForeignFilms_KeepsOwnedHidden(t *testing.T) {
	items := []peerPageItem{
		{
			mid: 10,
			film: &model.FilmIndex{
				FilmIndexIdentity: model.FilmIndexIdentity{FirstSourceId: "src"},
				FilmIndexCategory: model.FilmIndexCategory{Cid: 9, Pid: 9},
			},
		},
		{
			mid: 20,
			film: &model.FilmIndex{
				FilmIndexIdentity: model.FilmIndexIdentity{FirstSourceId: "other"},
			},
		},
	}
	dropForeignFilms(items, "src")
	if items[0].mid != 10 || items[0].film == nil {
		t.Fatal("owned film dropped")
	}
	if items[1].mid != 0 || items[1].film != nil {
		t.Fatal("foreign film kept")
	}
}
