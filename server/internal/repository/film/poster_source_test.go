package film

import (
	"encoding/json"
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
