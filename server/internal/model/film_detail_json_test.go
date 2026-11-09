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

func TestFilmDetailAcceptsStringIntFields(t *testing.T) {
	raw := []byte(`{"vod_id":"645395466392961375","type_id":"701","group_id":"0","vod_score_num":"167","vod_hits":"12","vod_up":"","vod_down":null,"vod_score_all":"8.5","vod_time_add":1731115697,"vod_douban_id":""}`)
	var detail FilmDetail
	if err := json.Unmarshal(raw, &detail); err != nil {
		t.Fatalf("字符串整型字段应可解析: %v", err)
	}
	if detail.VodID != 645395466392961375 || detail.TypeID != 701 || detail.GroupID != 0 {
		t.Fatalf("ID 解析不符: %+v", detail)
	}
	if detail.VodScoreNum != 167 || detail.VodHits != 12 || detail.VodUp != 0 || detail.VodDown != 0 || detail.VodScoreAll != 8 {
		t.Fatalf("统计字段解析不符: %+v", detail)
	}
	if detail.VodTimeAdd != 1731115697 || detail.VodDouBanID != 0 {
		t.Fatalf("数字或空字符串解析不符: %+v", detail)
	}
}

func TestFilmDetailRejectsNonNumericIntField(t *testing.T) {
	var detail FilmDetail
	if err := json.Unmarshal([]byte(`{"vod_score_num":"暂无"}`), &detail); err == nil {
		t.Fatal("非数字文本应报错")
	}
}

func TestFilmDetailPageAcceptsStringScoreNum(t *testing.T) {
	raw := []byte(`{"code":1,"list":[{"vod_id":1,"vod_name":"甲","vod_score_num":10},{"vod_id":2,"vod_name":"乙","vod_score_num":"10"}]}`)
	var page FilmDetailLPage
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("分页详情应可解析: %v", err)
	}
	if len(page.List) != 2 || page.List[0].VodScoreNum != 10 || page.List[1].VodScoreNum != 10 || page.List[1].VodName != "乙" {
		t.Fatalf("分页解析结果不符: %+v", page.List)
	}
}

func TestFilmListAcceptsStringIDs(t *testing.T) {
	raw := []byte(`{"code":1,"list":[{"vod_id":"9","type_id":"2","vod_name":"测试"}]}`)
	var page FilmListPage
	if err := json.Unmarshal(raw, &page); err != nil {
		t.Fatalf("列表应可解析: %v", err)
	}
	if len(page.List) != 1 || page.List[0].VodID != 9 || page.List[0].TypeID != 2 || page.List[0].VodName != "测试" {
		t.Fatalf("列表解析结果不符: %+v", page.List)
	}
}

func TestFlexIntOverflowAndFractionalID(t *testing.T) {
	var d FilmDetail
	if err := json.Unmarshal([]byte(`{"vod_hits":"9223372036854775808"}`), &d); err == nil {
		t.Fatalf("越界应报错，得到 %d", d.VodHits)
	}
	if err := json.Unmarshal([]byte(`{"vod_hits":"9223372036854775807"}`), &d); err != nil || d.VodHits != 9223372036854775807 {
		t.Fatalf("MaxInt64 应可解析: %d %v", d.VodHits, err)
	}
	for _, field := range []string{"vod_id", "type_id", "type_id_1", "group_id"} {
		var fd FilmDetail
		if err := json.Unmarshal([]byte(`{"`+field+`":"12.7"}`), &fd); err == nil {
			t.Fatalf("%s 带小数应报错", field)
		}
		if err := json.Unmarshal([]byte(`{"`+field+`":"12.0"}`), &fd); err != nil {
			t.Fatalf("%s 整数值的小数写法应可解析: %v", field, err)
		}
	}
	var l FilmList
	if err := json.Unmarshal([]byte(`{"vod_id":1.5}`), &l); err == nil {
		t.Fatal("列表 vod_id 带小数应报错")
	}
}

func TestPagesAcceptStringCounts(t *testing.T) {
	var cp CommonPage
	if err := json.Unmarshal([]byte(`{"code":1,"pagecount":"16218","total":"324350"}`), &cp); err != nil || cp.PageCount != 16218 || cp.Total != 324350 || cp.Code != 1 {
		t.Fatalf("探页外壳解析不符: %+v %v", cp, err)
	}
	raw := []byte(`{"code":1,"pagecount":"2","total":"30","list":[{"vod_id":"9","vod_name":"甲"}],"class":[{"type_id":"6","type_pid":"1","type_name":"动作片"}]}`)
	var lp FilmListPage
	if err := json.Unmarshal(raw, &lp); err != nil {
		t.Fatalf("列表外壳应可解析: %v", err)
	}
	if lp.PageCount != 2 || lp.Total != 30 || len(lp.List) != 1 || lp.List[0].VodID != 9 {
		t.Fatalf("列表外壳解析不符: %+v", lp)
	}
	if len(lp.Class) != 1 || lp.Class[0].ID != 6 || lp.Class[0].Pid != 1 || lp.Class[0].Name != "动作片" {
		t.Fatalf("分类解析不符: %+v", lp.Class)
	}
	var dp FilmDetailLPage
	if err := json.Unmarshal([]byte(`{"pagecount":"3","total":60,"list":[{"vod_id":1,"vod_name":"乙"}]}`), &dp); err != nil || dp.PageCount != 3 || dp.Total != 60 || len(dp.List) != 1 || dp.List[0].VodName != "乙" {
		t.Fatalf("详情外壳解析不符: %+v %v", dp, err)
	}
}
