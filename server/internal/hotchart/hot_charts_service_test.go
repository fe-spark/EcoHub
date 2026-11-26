package hotchart

import "testing"

func TestParseDoubanRexxarShowing(t *testing.T) {
	raw := []byte(`{
		"subject_collection_items": [
			{"id": "36915988", "title": "饥饿游戏：收获日出", "year": "2026", "rating": {"value": 6.3}},
			{"id": "", "title": "  "},
			{"id": "37822528", "title": "珠穆朗玛：云中彼岸", "year": "2026", "rating": {"value": 0}}
		]
	}`)
	got, err := parseDoubanRexxarShowing(raw)
	if err != nil {
		t.Fatalf("parseDoubanRexxarShowing failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 entries, got %d", len(got))
	}
	if got[0].DoubanID != 36915988 || got[0].Year != 2026 || got[0].Rate != "6.3" {
		t.Fatalf("unexpected first item: %+v", got[0])
	}
	if got[1].Rate != "" {
		t.Fatalf("expected empty rate for 0 rating, got %q", got[1].Rate)
	}
}

func TestParseDoubanSearchSubjects(t *testing.T) {
	raw := []byte(`{
		"subjects": [
			{"id": "36847425", "title": "可能的爱", "rate": "8.6"},
			{"id": "37215318", "title": "黑球", "rate": "8.7"}
		]
	}`)
	got, err := parseDoubanSearchSubjects(raw)
	if err != nil {
		t.Fatalf("parseDoubanSearchSubjects failed: %v", err)
	}
	if len(got) != 2 || got[0].DoubanID != 36847425 || got[1].Title != "黑球" {
		t.Fatalf("unexpected subjects: %+v", got)
	}
}

func TestParseBangumiCalendar(t *testing.T) {
	raw := []byte(`[
		{
			"items": [
				{"name": "OrigName", "name_cn": "中文番名", "air_date": "2026-10-05", "rating": {"score": 7.8}, "collection": {"doing": 200}},
				{"name": "OnlyJP", "name_cn": "", "air_date": "2026-10-06", "rating": {"score": 8.2}, "collection": {"doing": 500}},
				{"name": "Dup", "name_cn": "中文番名", "air_date": "2026-10-05", "rating": {"score": 6.0}, "collection": {"doing": 50}}
			]
		}
	]`)
	got, err := parseBangumiCalendar(raw)
	if err != nil {
		t.Fatalf("parseBangumiCalendar failed: %v", err)
	}
	if len(got) != 2 {
		t.Fatalf("expected 2 deduped entries, got %d", len(got))
	}
	// 按 doing 降序排序，OnlyJP 排第一
	if got[0].Title != "OnlyJP" || got[0].Year != 2026 || got[0].Rate != "8.2" {
		t.Fatalf("unexpected first bgm item: %+v", got[0])
	}
	if got[1].Title != "中文番名" {
		t.Fatalf("unexpected second bgm item: %+v", got[1])
	}
}
