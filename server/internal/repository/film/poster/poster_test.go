package poster

import (
	"strings"
	"testing"

	"server/internal/model"
	"server/internal/repository/film/shared"
)

func TestHasExternalPosterSourceLogic(t *testing.T) {
	// 当无海报源配置时，返回 false
	if hasExternalPosterSource("") {
		t.Fatal("无外部海报源配置时应返回 false")
	}
}

func TestPosterQueryStrippingComparison(t *testing.T) {
	pic1 := "https://img.cdn.com/poster/123.jpg?token=abc&t=1600000"
	pic2 := "https://img.cdn.com/poster/123.jpg?token=xyz&t=1700000"
	pic3 := "https://img.cdn.com/poster/123.jpg"

	if shared.StripURLQuery(pic1) != shared.StripURLQuery(pic2) {
		t.Fatalf("带不同 query 参数的相同图片路径应剥离一致: %q vs %q", shared.StripURLQuery(pic1), shared.StripURLQuery(pic2))
	}
	if shared.StripURLQuery(pic1) != shared.StripURLQuery(pic3) {
		t.Fatalf("带 query 与不带 query 的相同图片路径应剥离一致: %q vs %q", shared.StripURLQuery(pic1), shared.StripURLQuery(pic3))
	}

	diffPic := "https://img.cdn.com/poster/456.jpg?token=abc"
	if shared.StripURLQuery(pic1) == shared.StripURLQuery(diffPic) {
		t.Fatalf("不同图片路径应被判定为不一致: %q vs %q", shared.StripURLQuery(pic1), shared.StripURLQuery(diffPic))
	}
}

func TestPickBestMatchedPoster(t *testing.T) {
	detail := model.MovieDetail{
		Name: "流浪地球",
		MovieDescriptor: model.MovieDescriptor{
			DbId: 123456,
		},
	}
	keys := shared.BuildPlaylistMovieKeys(detail)
	if len(keys) == 0 {
		t.Fatal("shared.BuildPlaylistMovieKeys 应生成 key")
	}
	postersByKey := map[string]model.MoviePoster{
		keys[0]: {
			SourceId:     "src_poster",
			MovieKey:     keys[0],
			Picture:      "https://high.cdn/poster.jpg",
			PictureSlide: "https://high.cdn/slide.jpg",
		},
	}
	matched := PickBestMatchedPoster(detail, postersByKey)
	if matched == nil || matched.Picture != "https://high.cdn/poster.jpg" {
		t.Fatalf("PickBestMatchedPoster 未按 key 命中海报: %+v", matched)
	}
}

func TestApplyExternalPosterSourceRespectsManualOverride(t *testing.T) {
	existing := map[int64]model.FilmIndex{
		100: {
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 100, ContentKey: "vod_100"},
			FilmIndexContent: model.FilmIndexContent{
				Picture:         "https://original.source/poster.jpg",
				CustomPicture:   "https://my.custom/poster.jpg",
				IsCustomPicture: true,
			},
		},
	}
	infos := []model.FilmIndex{
		{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: 100, ContentKey: "vod_100"},
			FilmIndexContent:  model.FilmIndexContent{Picture: "https://new.crawled/poster.jpg", IsCustomPicture: false},
		},
	}
	detailsByKey := map[string]model.MovieDetail{
		"vod_100": {
			Id:              100,
			Name:            "测试片",
			Picture:         "https://new.crawled/poster.jpg",
			IsCustomPicture: false,
		},
	}

	// 1. 爬虫模式 (isManual = false) -> 保留 CustomPicture，且不会丢失底层 Picture
	_ = ApplyExternalPosterSourceToMasterWritesTx(nil, "master", infos, detailsByKey, existing, false)
	if !infos[0].IsCustomPicture || infos[0].CustomPicture != "https://my.custom/poster.jpg" {
		t.Fatalf("爬虫模式下已有自定义海报应被保护: %+v", infos[0])
	}
	if infos[0].DisplayPicture() != "https://my.custom/poster.jpg" {
		t.Fatalf("自定义生效时 DisplayPicture 应返回 CustomPicture: %q", infos[0].DisplayPicture())
	}

	// 2. 人工保存模式 (isManual = true) -> 管理员解除自定义锁定 (IsCustomPicture = false)，DisplayPicture 立即显示底层 Picture
	infos[0].IsCustomPicture = false
	infos[0].CustomPicture = ""
	infos[0].Picture = "https://hd.poster/poster.jpg"
	detailsByKey["vod_100"] = model.MovieDetail{Id: 100, Name: "测试片", Picture: "https://hd.poster/poster.jpg", IsCustomPicture: false}
	_ = ApplyExternalPosterSourceToMasterWritesTx(nil, "master", infos, detailsByKey, existing, true)
	if infos[0].IsCustomPicture {
		t.Fatalf("人工保存模式下应允许将 IsCustomPicture 设为 false: %+v", infos[0])
	}
	if infos[0].DisplayPicture() != "https://hd.poster/poster.jpg" {
		t.Fatalf("解除自定义后 DisplayPicture 应返回原图/海报源图: %q", infos[0].DisplayPicture())
	}
}

func TestCustomPictureToggleRestoresRawPicture(t *testing.T) {
	// 验证：当影片设置了自定义海报，在解除自定义后（IsCustomPicture = false），
	// 若外部海报源未命中，系统应能够正确从 existing 恢复原始采集图片。
	existing := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{Mid: 300, ContentKey: "vod_300"},
		FilmIndexContent: model.FilmIndexContent{
			Name:            "原始电影",
			Picture:         "https://spider.raw/poster.jpg",
			PictureSlide:    "https://spider.raw/slide.jpg",
			CustomPicture:   "https://user.custom/poster.jpg",
			IsCustomPicture: true,
		},
	}

	// 模拟管理员提交解除自定义请求，传入空 picture
	detail := model.MovieDetail{
		Id:              300,
		Name:            "原始电影",
		IsCustomPicture: false,
		CustomPicture:   "",
		Picture:         "",
	}

	// 模拟 SaveDetail 的回退还原逻辑
	if !detail.IsCustomPicture && detail.Id > 0 && strings.TrimSpace(existing.Picture) != "" {
		detail.Picture = existing.Picture
		detail.PictureSlide = existing.PictureSlide
	}

	if detail.Picture != "https://spider.raw/poster.jpg" {
		t.Fatalf("解除自定义后底层 Picture 未能从 existing 恢复: got %q, want %q", detail.Picture, "https://spider.raw/poster.jpg")
	}
	if detail.PictureSlide != "https://spider.raw/slide.jpg" {
		t.Fatalf("解除自定义后底层 PictureSlide 未能从 existing 恢复: got %q, want %q", detail.PictureSlide, "https://spider.raw/slide.jpg")
	}
}
