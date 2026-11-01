package model

import "testing"

func TestDedupPlayURLsCollapsesPaddedEpisodeNames(t *testing.T) {
	links := []MovieUrlInfo{
		{Episode: "第01集", Link: "http://a/1.m3u8"},
		{Episode: "第02集", Link: "http://a/2.m3u8"},
		{Episode: "第01集", Link: "http://b/1.m3u8"},
		{Episode: "第1集", Link: "http://c/1.m3u8"},
		{Episode: "第2集", Link: "http://c/2.m3u8"},
		{Episode: "第1-12集", Link: "http://pack.m3u8"},
		{Episode: "正片", Link: "http://movie.m3u8"},
		{Episode: "正片", Link: "http://movie-2.m3u8"},
	}
	got := DedupPlayURLs(links)
	if len(got) != 4 {
		t.Fatalf("len=%d %+v", len(got), got)
	}
	if got[0].Link != "http://a/1.m3u8" || got[1].Episode != "第02集" {
		t.Fatalf("order=%+v", got[:2])
	}
	if got[2].Episode != "第1-12集" || got[3].Episode != "正片" || got[3].Link != "http://movie.m3u8" {
		t.Fatalf("tail=%+v", got[2:])
	}
}
