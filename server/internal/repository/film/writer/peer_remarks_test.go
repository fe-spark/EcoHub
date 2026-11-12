package writer

import (
	"testing"

	"server/internal/model"
)

func TestPickPlaylistRemarks_IgnoresDisabledSources(t *testing.T) {
	sources := []model.FilmSource{
		{Id: "src-active", State: true},
		{Id: "src-disabled", State: false},
	}
	lines := []model.FilmSourcePlaylist{
		{SourceId: "src-active", EpisodeCount: 10, LastEpisode: "第10集"},
		{SourceId: "src-disabled", EpisodeCount: 15, LastEpisode: "第15集"},
	}
	if got := pickPlaylistRemarks(lines, sources); got != "第10集" {
		t.Fatalf("want active source remark '第10集', got %q", got)
	}
}

func TestStampNewFilmPlaySummary_UsesPlayLineNotLongerDownload(t *testing.T) {
	source := model.FilmSource{Id: "src-secondary", Name: "副站", State: true}
	detail := testPeerDetail(8802, "下载更长", "第1集")
	detail.DownFrom = "下载"
	detail.DownloadList = [][]model.MovieUrlInfo{{
		{Episode: "文件1", Link: "http://down/1"},
		{Episode: "文件2", Link: "http://down/2"},
		{Episode: "文件3", Link: "http://down/3"},
	}}

	var film model.FilmIndex
	stampNewFilmPlaySummary(&film, &source, detail, []model.FilmSource{source})
	if film.Remarks != "第1集" {
		t.Fatalf("remarks=%q", film.Remarks)
	}
	if film.PlayFromSummary == "" {
		t.Fatal("play summary empty")
	}
}
