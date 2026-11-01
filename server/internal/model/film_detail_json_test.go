package model

import (
	"encoding/json"
	"testing"
)

func TestFilmDetailAcceptsNumericStringFields(t *testing.T) {
	raw := []byte(`{
		"vod_id": 1,
		"vod_name": "测试",
		"vod_serial": 90,
		"vod_year": 2024,
		"vod_duration": 108,
		"vod_score": 8.5,
		"vod_douban_score": 9
	}`)
	var detail FilmDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatalf("数字字段应可解析: %v", err)
	}
	if detail.VodSerial != "90" || detail.VodYear != "2024" || detail.VodDuration != "108" || detail.VodScore != "8.5" || detail.VodDouBanScore != "9" {
		t.Fatalf("数字未转成字符串: %+v", detail)
	}
	if detail.VodID != 1 || detail.VodName != "测试" {
		t.Fatalf("其余字段被覆盖: %+v", detail)
	}
}

func TestFilmDetailKeepsStringFields(t *testing.T) {
	raw := []byte(`{"vod_serial":"更新至90集","vod_year":"2020","vod_score":null}`)
	var detail FilmDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatalf("字符串字段应可解析: %v", err)
	}
	if detail.VodSerial != "更新至90集" || detail.VodYear != "2020" || detail.VodScore != "" {
		t.Fatalf("字符串或 null 解析结果不符: %+v", detail)
	}
}

func TestFilmDetailPageAcceptsNumericSerial(t *testing.T) {
	raw := []byte(`{"code":1,"list":[{"vod_name":"测试","vod_serial":90}]}`)
	var page FilmDetailLPage
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("分页详情应可解析: %v", err)
	}
	if len(page.List) != 1 || page.List[0].VodSerial != "90" {
		t.Fatalf("列表中的连载数不符: %+v", page.List)
	}
}
func TestFilmDetailVo_UnmarshalPlayFrom(t *testing.T) {
	// 1. array playFrom
	raw1 := []byte(`{"name":"测试","playFrom":["1080zyk","ffm3u8"]}`)
	var fd1 FilmDetailVo
	if err := json.Unmarshal(raw1, &fd1); err != nil {
		t.Fatal(err)
	}
	if len(fd1.PlayFrom) != 2 || fd1.PlayFrom[0] != "1080zyk" || fd1.PlayFrom[1] != "ffm3u8" {
		t.Fatalf("array playFrom parse failed: %+v", fd1.PlayFrom)
	}

	// 2. string playFrom with $$$
	raw2 := []byte(`{"name":"测试","playFrom":"1080zyk$$$ffm3u8"}`)
	var fd2 FilmDetailVo
	if err := json.Unmarshal(raw2, &fd2); err != nil {
		t.Fatal(err)
	}
	if len(fd2.PlayFrom) != 2 || fd2.PlayFrom[0] != "1080zyk" || fd2.PlayFrom[1] != "ffm3u8" {
		t.Fatalf("string playFrom parse failed: %+v", fd2.PlayFrom)
	}

	// 3. alias playForm string
	raw3 := []byte(`{"name":"测试","playForm":"1080zyk"}`)
	var fd3 FilmDetailVo
	if err := json.Unmarshal(raw3, &fd3); err != nil {
		t.Fatal(err)
	}
	if len(fd3.PlayFrom) != 1 || fd3.PlayFrom[0] != "1080zyk" {
		t.Fatalf("playForm alias parse failed: %+v", fd3.PlayFrom)
	}
}
