package service

import (
	"testing"

	"server/internal/model"
)

func TestProvidePlayFromName_Proxy(t *testing.T) {
	if got := providePlayFromName(model.PlayLinkVo{Name: "量子", Proxy: true}); got != "量子代理" {
		t.Fatalf("proxy line = %q", got)
	}
	if got := providePlayFromName(model.PlayLinkVo{Name: "量子"}); got != "量子" {
		t.Fatalf("direct line = %q", got)
	}
	if got := providePlayFromName(model.PlayLinkVo{Name: "", Proxy: true}); got != "代理" {
		t.Fatalf("empty proxy line = %q", got)
	}
	if got := providePlayFromName(model.PlayLinkVo{Name: "已是代理", Proxy: true}); got != "已是代理" {
		t.Fatalf("already marked = %q", got)
	}
}

func TestFormatProvideFilmDetail_ProxyMark(t *testing.T) {
	detail := formatProvideFilmDetail(model.FilmListSnapshot{Mid: 1, Name: "片"}, model.MovieDetailVo{
		List: []model.PlayLinkVo{
			{Name: "直连站", LinkList: []model.MovieUrlInfo{{Episode: "1", Link: "http://a"}}},
			{Name: "代理站", Proxy: true, LinkList: []model.MovieUrlInfo{{Episode: "1", Link: "http://b"}}},
		},
	}, "")
	if detail.VodPlayFrom != "直连站$$$代理站代理" {
		t.Fatalf("play from %q", detail.VodPlayFrom)
	}
	if detail.VodPlayURL != "1$http://a$$$1$http://b" {
		t.Fatalf("play url %q", detail.VodPlayURL)
	}
}
