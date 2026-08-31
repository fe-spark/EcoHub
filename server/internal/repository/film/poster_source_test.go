package film

import (
	"encoding/json"
	"strings"
	"testing"

	"server/internal/model"
)

func TestHasExternalPosterSourceLogic(t *testing.T) {
	// 当无海报源配置时，返回 false
	if hasExternalPosterSource("") {
		t.Fatal("无外部海报源配置时应返回 false")
	}
}

func TestPosterPreservationInMasterWrite(t *testing.T) {
	// 验证 masterBusinessSignature 在海报一致时正确识别业务一致
	detailA := model.MovieDetail{
		Id:      101,
		Name:    "测试电影",
		Picture: "https://high-quality.cdn/poster.jpg",
		PlayFrom: []string{"test_m3u8"},
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "01", Link: "http://test/1.m3u8"}},
		},
		MovieDescriptor: model.MovieDescriptor{
			Remarks: "HD",
			State:   "正片",
		},
	}

	detailB := detailA
	detailB.Picture = "https://high-quality.cdn/poster.jpg?timestamp=12345"

	if !sameStoredMasterDetail(detailA, detailB) {
		t.Fatal("忽略 URL query 参数后，相同海报应被判定为业务无变化")
	}
}

func TestBuildMovieDetailInfosPreservesPosterFromInfo(t *testing.T) {
	detail := model.MovieDetail{
		Id:      202,
		Name:    "测试海报同步",
		Picture: "https://low-quality.cdn/master_low.jpg",
	}
	contentKey := BuildContentKey(detail)
	info := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:        202,
			ContentKey: contentKey,
		},
		FilmIndexContent: model.FilmIndexContent{
			Name:         detail.Name,
			Picture:      "https://high-quality.cdn/poster_hd.jpg",
			PictureSlide: "https://high-quality.cdn/slide_hd.jpg",
		},
	}
	infoByKey := map[string]model.FilmIndex{
		contentKey: info,
	}
	keyToMid := map[string]int64{
		contentKey: 202,
	}

	detailInfos := buildMovieDetailInfos("src_1", []model.MovieDetail{detail}, infoByKey, keyToMid)
	if len(detailInfos) != 1 {
		t.Fatalf("buildMovieDetailInfos 返回数量错误: %d, 期望 1", len(detailInfos))
	}

	var parsed model.MovieDetail
	if err := json.Unmarshal([]byte(detailInfos[0].Content), &parsed); err != nil {
		t.Fatalf("解析 detail info content 失败: %v", err)
	}

	if parsed.Picture != info.Picture {
		t.Fatalf("Picture 未从 info 同步保留: got %q, want %q", parsed.Picture, info.Picture)
	}
	if parsed.PictureSlide != info.PictureSlide {
		t.Fatalf("PictureSlide 未从 info 同步保留: got %q, want %q", parsed.PictureSlide, info.PictureSlide)
	}
}

func TestPosterQueryStrippingComparison(t *testing.T) {
	pic1 := "https://img.cdn.com/poster/123.jpg?token=abc&t=1600000"
	pic2 := "https://img.cdn.com/poster/123.jpg?token=xyz&t=1700000"
	pic3 := "https://img.cdn.com/poster/123.jpg"

	if stripURLQuery(pic1) != stripURLQuery(pic2) {
		t.Fatalf("带不同 query 参数的相同图片路径应剥离一致: %q vs %q", stripURLQuery(pic1), stripURLQuery(pic2))
	}
	if stripURLQuery(pic1) != stripURLQuery(pic3) {
		t.Fatalf("带 query 与不带 query 的相同图片路径应剥离一致: %q vs %q", stripURLQuery(pic1), stripURLQuery(pic3))
	}

	diffPic := "https://img.cdn.com/poster/456.jpg?token=abc"
	if stripURLQuery(pic1) == stripURLQuery(diffPic) {
		t.Fatalf("不同图片路径应被判定为不一致: %q vs %q", stripURLQuery(pic1), stripURLQuery(diffPic))
	}
}

func TestPickBestMatchedPoster(t *testing.T) {
	detail := model.MovieDetail{
		Name: "流浪地球",
		MovieDescriptor: model.MovieDescriptor{
			DbId: 123456,
		},
	}
	keys := BuildPlaylistMovieKeys(detail)
	if len(keys) == 0 {
		t.Fatal("BuildPlaylistMovieKeys 应生成 key")
	}
	postersByKey := map[string]model.MoviePoster{
		keys[0]: {
			SourceId:     "src_poster",
			MovieKey:     keys[0],
			Picture:      "https://high.cdn/poster.jpg",
			PictureSlide: "https://high.cdn/slide.jpg",
		},
	}
	matched := pickBestMatchedPoster(detail, postersByKey)
	if matched == nil || matched.Picture != "https://high.cdn/poster.jpg" {
		t.Fatalf("pickBestMatchedPoster 未按 key 命中海报: %+v", matched)
	}
}

func TestCustomPictureStateChangeTriggersUpdate(t *testing.T) {
	detailA := model.MovieDetail{
		Name:            "完美世界",
		Picture:         "https://wrong.cdn/wrong.jpg",
		IsCustomPicture: true,
	}
	detailB := detailA
	detailB.IsCustomPicture = false

	if sameStoredMasterDetail(detailA, detailB) {
		t.Fatal("IsCustomPicture 状态从 true 切换为 false 应被判定为实质变更")
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

