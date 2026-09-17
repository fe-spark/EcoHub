package service

import (
	"testing"

	"server/internal/model"
	"server/internal/repository"
)

func TestCleanKeywordForSearch(t *testing.T) {
	tests := []struct {
		input        string
		expectedName string
		expectedYear string
	}{
		{
			input:        "流浪地球2 4K 60帧 国粤双语",
			expectedName: "流浪地球2",
			expectedYear: "",
		},
		{
			input:        "星际穿越 (2014) [1080P]",
			expectedName: "星际穿越",
			expectedYear: "2014",
		},
		{
			input:        "狂飙 第01集",
			expectedName: "狂飙",
			expectedYear: "",
		},
		{
			input:        "三体 【更新至20集】",
			expectedName: "三体",
			expectedYear: "",
		},
		{
			input:        "奥本海默 Oppenheimer 2023 抢先版",
			expectedName: "奥本海默 Oppenheimer",
			expectedYear: "2023",
		},
		{
			input:        "Cats 2019",
			expectedName: "Cats",
			expectedYear: "2019",
		},
		{
			input:        "Match Point (2005)",
			expectedName: "Match Point",
			expectedYear: "2005",
		},
		{
			input:        "Pitch Perfect",
			expectedName: "Pitch Perfect",
			expectedYear: "",
		},
		{
			input:        "Gotham Knights",
			expectedName: "Gotham Knights",
			expectedYear: "",
		},
	}

	for _, tc := range tests {
		name, year := CleanKeywordForSearch(tc.input)
		if name != tc.expectedName {
			t.Errorf("CleanKeywordForSearch(%q) name = %q, expected %q", tc.input, name, tc.expectedName)
		}
		if year != tc.expectedYear {
			t.Errorf("CleanKeywordForSearch(%q) year = %q, expected %q", tc.input, year, tc.expectedYear)
		}
	}
}

func TestTMDBApiKeyMasking(t *testing.T) {
	key := "12345678abcdef90"
	masked := repository.MaskTMDBApiKey(key)
	if masked != "1234***ef90" {
		t.Errorf("MaskTMDBApiKey(%q) = %q, expected %q", key, masked, "1234***ef90")
	}
	if !repository.IsMaskedTMDBApiKey(masked) {
		t.Errorf("IsMaskedTMDBApiKey(%q) should be true", masked)
	}
	if repository.IsMaskedTMDBApiKey(key) {
		t.Errorf("IsMaskedTMDBApiKey(%q) should be false", key)
	}

	shortKey := "12345"
	maskedShort := repository.MaskTMDBApiKey(shortKey)
	if maskedShort != "*****" {
		t.Errorf("MaskTMDBApiKey(%q) = %q, expected %q", shortKey, maskedShort, "*****")
	}
	if !repository.IsMaskedTMDBApiKey(maskedShort) {
		t.Errorf("IsMaskedTMDBApiKey(%q) should be true", maskedShort)
	}
}

func TestNormalizeTMDBConfig(t *testing.T) {
	cfg := model.TMDBConfig{
		Enabled:     true,
		ApiKey:      "  test_key  ",
		Proxy:       " http://127.0.0.1:7890/ ",
		Language:    "",
		ImageDomain: "https://image.tmdb.org/t/p///",
	}
	normalized := repository.NormalizeTMDBConfig(cfg)
	if normalized.ApiKey != "test_key" {
		t.Errorf("expected test_key, got %q", normalized.ApiKey)
	}
	if normalized.Language != repository.DefaultTMDBLanguage {
		t.Errorf("expected default language, got %q", normalized.Language)
	}
	if normalized.ImageDomain != "https://image.tmdb.org/t/p" {
		t.Errorf("expected trimmed image domain, got %q", normalized.ImageDomain)
	}
}

func TestCandidateYearFilter(t *testing.T) {
	candidates := []model.TMDBCandidate{
		{ID: 1, Title: "毛雪汪", Year: "2021"},
		{ID: 2, Title: "毛雪汪26秋番", Year: "2026"},
	}

	filterYear := "1991"
	filtered := make([]model.TMDBCandidate, 0)
	for _, c := range candidates {
		if filterYear != "" && c.Year != filterYear {
			continue
		}
		filtered = append(filtered, c)
	}
	if len(filtered) != 0 {
		t.Fatalf("expected 0 candidates for year 1991, got %d", len(filtered))
	}

	filterYear2 := "2021"
	filtered2 := make([]model.TMDBCandidate, 0)
	for _, c := range candidates {
		if filterYear2 != "" && c.Year != filterYear2 {
			continue
		}
		filtered2 = append(filtered2, c)
	}
	if len(filtered2) != 1 || filtered2[0].ID != 1 {
		t.Fatalf("expected 1 candidate (ID=1) for year 2021, got %+v", filtered2)
	}
}
