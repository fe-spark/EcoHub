package service

import (
	"testing"

	"server/internal/model"
)

func TestFormatProvideFilmDetail_LineNameAndPlayURL(t *testing.T) {
	detail, err := formatProvideFilmDetail(model.FilmListSnapshot{Mid: 1, Name: "片"}, model.MovieDetailVo{
		List: []model.PlayLinkVo{
			{Name: "直连站", LinkList: []model.MovieUrlInfo{{Episode: "1", Link: "http://a"}}},
			{Name: " 代理站 ", Proxy: true, LinkList: []model.MovieUrlInfo{{Episode: "1$2#3", Link: "http://b/x$y#z"}}},
		},
	}, "")
	if err != nil {
		t.Fatal(err)
	}
	if detail.VodPlayFrom != "直连站$$$代理站" {
		t.Fatalf("play from %q", detail.VodPlayFrom)
	}
	if detail.VodPlayURL != "1$http://a$$$1＄2＃3$http://b/x%24y%23z" {
		t.Fatalf("play url %q", detail.VodPlayURL)
	}
}
