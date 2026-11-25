package portal

import (
	"strings"
	"testing"

	"server/internal/model"
)

func TestFormatProvidePlayLists(t *testing.T) {
	sources := []model.PlayLinkVo{
		{
			SourceId: "s1",
			Name:     " 线路A ",
			LinkList: []model.MovieUrlInfo{
				{Episode: "第$1#集", Link: "https://a.com/v$1#2.m3u8"},
				{Episode: "第2集", Link: "http://a.com/2.mp4"},
			},
		},
		{
			SourceId: "manual",
			IsManual: true,
			Name:     "自建线路",
			LinkList: []model.MovieUrlInfo{
				{Episode: "正片", Link: "https://m.com/play.m3u8"},
			},
		},
	}

	gotFrom, gotURL := formatProvidePlayLists(sources)
	wantFrom := "自建线路$$$线路A"
	wantURL := "正片$https://m.com/play.m3u8$$$第＄1＃集$https://a.com/v%241%232.m3u8#第2集$http://a.com/2.mp4"
	if gotFrom != wantFrom {
		t.Fatalf("vod_play_from = %q, want %q", gotFrom, wantFrom)
	}
	if gotURL != wantURL {
		t.Fatalf("vod_play_url = %q, want %q", gotURL, wantURL)
	}
}

func TestFormatProvidePlayLists_AllSourcesIncluded(t *testing.T) {
	var sources []model.PlayLinkVo
	for i := 1; i <= 6; i++ {
		sources = append(sources, model.PlayLinkVo{
			SourceId: string(rune('a' + i)),
			Name:     string(rune('A' + i)),
			LinkList: []model.MovieUrlInfo{
				{Episode: "第1集", Link: "https://example.com/1.m3u8"},
			},
		})
	}
	sources = append(sources, model.PlayLinkVo{
		SourceId: "manual",
		IsManual: true,
		Name:     "自建源",
		LinkList: []model.MovieUrlInfo{
			{Episode: "第1集", Link: "https://manual.com/1.m3u8"},
		},
	})

	gotFrom, gotURL := formatProvidePlayLists(sources)
	fromParts := strings.Split(gotFrom, "$$$")
	if len(fromParts) != 7 { // 1 manual + 6 normal 全部下发
		t.Fatalf("expected 7 sources, got %d (%q)", len(fromParts), gotFrom)
	}
	if fromParts[0] != "自建源" {
		t.Fatalf("expected first source to be 自建源, got %q", fromParts[0])
	}
	urlParts := strings.Split(gotURL, "$$$")
	if len(urlParts) != 7 {
		t.Fatalf("expected 7 url groups, got %d", len(urlParts))
	}
}
