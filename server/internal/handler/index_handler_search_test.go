package handler

import "testing"

func TestClampSearchFilmPageSize(t *testing.T) {
	if got := clampSearchFilmPageSize(20, false); got != 12 {
		t.Fatalf("unspecified should default to 12, got %d", got)
	}
	if got := clampSearchFilmPageSize(0, true); got != 12 {
		t.Fatalf("invalid specified size should default to 12, got %d", got)
	}
	if got := clampSearchFilmPageSize(24, true); got != 24 {
		t.Fatalf("explicit size should be kept, got %d", got)
	}
	if got := clampSearchFilmPageSize(500, true); got != 50 {
		t.Fatalf("oversize should cap at 50, got %d", got)
	}
}

func TestHasSearchOptions(t *testing.T) {
	if hasSearchOptions(nil) {
		t.Fatal("nil should be false")
	}
	if hasSearchOptions(map[string]any{}) {
		t.Fatal("empty should be false")
	}
	if hasSearchOptions(map[string]any{"sortList": []string{"Sort"}, "tags": map[string]any{}}) {
		t.Fatal("sortList without tags should be false")
	}
	if hasSearchOptions(map[string]any{
		"tags": map[string]any{
			"Category": []map[string]string{{"Name": "全部", "Value": ""}},
		},
	}) {
		t.Fatal("Category 仅全部 should be false")
	}

	sortOnly := map[string]any{
		"sortList": []string{"Sort"},
		"tags": map[string]any{
			"Sort": []map[string]string{
				{"Name": "最近更新", "Value": "update_stamp"},
			},
		},
	}
	if !hasSearchOptions(sortOnly) {
		t.Fatal("仅 Sort 应展示筛选面板")
	}

	jsonLike := map[string]any{
		"sortList": []any{"Sort"},
		"tags": map[string]any{
			"Sort": []any{map[string]any{"Name": "最近更新", "Value": "update_stamp"}},
		},
	}
	if !hasSearchOptions(jsonLike) {
		t.Fatal("JSON 反序列化后的仅 Sort 应展示筛选面板")
	}

	if !hasSearchOptions(map[string]any{
		"tags": map[string]any{
			"Plot": []map[string]string{{"Name": "甜宠", "Value": "甜宠"}},
		},
	}) {
		t.Fatal("真实 Plot 标签应展示筛选面板")
	}
}
