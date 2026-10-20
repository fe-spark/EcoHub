package snapshot

import "testing"

func TestManageMetaCategory_SourceKey(t *testing.T) {
	meta := FilmSearchMeta{Pid: 57, CategoryKey: "source:1208629981:57"}
	if !metaHasSourceType(&meta, "1208629981", []int64{57}) {
		t.Fatal("source type key should match category_key")
	}
	byPidOnly := FilmSearchMeta{Pid: 57, CategoryKey: "source:other:57"}
	if metaHasSourceType(&byPidOnly, "1208629981", []int64{57}) {
		t.Fatal("local pid must not match another source key")
	}
	cid := FilmSearchMeta{Cid: 58, CategoryKey: "source:1208629981:58"}
	if !metaHasSourceType(&cid, "1208629981", []int64{58}) {
		t.Fatal("cid source key should match")
	}
}
