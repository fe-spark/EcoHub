package model

import "testing"

func TestDedupPlayURLsKeepsDistinctURLsAndDropsExactDuplicates(t *testing.T) {
	links := []MovieUrlInfo{
		{Episode: "第01集", Link: "http://a/1.m3u8"},
		{Episode: "第02集", Link: "http://a/2.m3u8"},
		{Episode: "第01集", Link: "http://a/1.m3u8"},
		{Episode: "第1集", Link: " http://a/1.m3u8 "},
		{Episode: "第1集", Link: "http://c/1.m3u8"},
		{Episode: "第1-12集", Link: "http://pack.m3u8"},
		{Episode: "正片", Link: "http://movie.m3u8"},
		{Episode: "正片", Link: "http://movie-2.m3u8"},
	}
	got := DedupPlayURLs(links)
	want := []string{"http://a/1.m3u8", "http://a/2.m3u8", "http://c/1.m3u8", "http://pack.m3u8", "http://movie.m3u8", "http://movie-2.m3u8"}
	if len(got) != len(want) {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	for i, link := range want {
		if got[i].Link != link {
			t.Fatalf("got[%d]=%+v want link %s", i, got[i], link)
		}
	}
}

func TestDedupPlayURLsKeepsVarietyEpisodes(t *testing.T) {
	names := []string{"第1期上", "第1期中", "第1期下", "第1期上纯享", "第1期加更上", "第20260803期上", "第20260803期上纯享"}
	links := make([]MovieUrlInfo, 0, len(names))
	for i, name := range names {
		links = append(links, MovieUrlInfo{Episode: name, Link: "http://v/" + string(rune('a'+i)) + ".m3u8"})
	}
	got := DedupPlayURLs(links)
	if len(got) != len(names) {
		t.Fatalf("len=%d want %d: %+v", len(got), len(names), got)
	}
	for i, name := range names {
		if got[i].Episode != name {
			t.Fatalf("got[%d]=%q want %q", i, got[i].Episode, name)
		}
	}
}

func TestDedupPlayURLsKeepsSameNameDifferentURL(t *testing.T) {
	links := []MovieUrlInfo{
		{Episode: "花絮特辑", Link: "http://v/1.m3u8"},
		{Episode: "花絮特辑", Link: "http://v/2.m3u8"},
	}
	if got := DedupPlayURLs(links); len(got) != 2 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
}
