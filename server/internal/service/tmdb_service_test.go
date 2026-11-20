package service

import (
	"testing"

	"server/internal/model"
	"server/internal/repository"
)

func TestPrepareManualSearchKeepsQueryAndYear(t *testing.T) {
	query, year := prepareManualSearch("流浪地球：飞跃2020 1080p", "2020")
	if query != "流浪地球：飞跃2020" {
		t.Fatalf("query = %q", query)
	}
	if year != "2020" {
		t.Fatalf("year = %q", year)
	}

	query, year = prepareManualSearch("星际穿越 (2014) [1080P]", "")
	if query != "星际穿越" || year != "2014" {
		t.Fatalf("query = %q year = %q", query, year)
	}

	query, year = prepareManualSearch("流浪地球：飞跃2020", "1999")
	if query != "流浪地球：飞跃2020" || year != "1999" {
		t.Fatalf("query = %q year = %q", query, year)
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

func TestValidateTMDBImageDomain(t *testing.T) {
	for _, valid := range []string{"", "https://image.tmdb.org/t/p", "http://img.example.com/t/p"} {
		if err := validateTMDBImageDomain(valid); err != nil {
			t.Fatalf("expected %q to be valid, got %v", valid, err)
		}
	}
	for _, invalid := range []string{"image.tmdb.org/t/p", "ftp://image.tmdb.org/t/p", "https://"} {
		if err := validateTMDBImageDomain(invalid); err == nil {
			t.Fatalf("expected %q to be rejected", invalid)
		}
	}
}
