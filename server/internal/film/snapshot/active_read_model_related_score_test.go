package snapshot

import (
	"testing"
	"time"

	"server/internal/model"
)

func TestExtractCoreSearchToken(t *testing.T) {
	tests := []struct {
		input    string
		expected string
	}{
		{"仙逆", "仙逆"},
		{"仙逆 年番", "仙逆"},
		{"斗罗大陆 第1季", "斗罗大陆"},
		{"斗破苍穹特别篇", "斗破苍穹"},
		{"凡人修仙传：星海飞驰篇", "凡人修仙传"},
		{"海贼王（剧场版）", "海贼王"},
	}

	for _, tt := range tests {
		got := extractCoreSearchToken(tt.input)
		if got != tt.expected {
			t.Errorf("extractCoreSearchToken(%q) = %q, want %q", tt.input, got, tt.expected)
		}
	}
}

func TestRankRelatedCandidates_ScoringOrder(t *testing.T) {
	now := time.Now().Unix()
	current := model.FilmListSnapshot{
		Mid:         100,
		Name:        "仙逆",
		SeriesKey:   "series_xianni",
		CategoryKey: "dongman:guoman",
		Cid:         10,
		ClassTag:    "修真,玄幻,古装",
		Area:        "中国大陆",
		Language:    "国语",
		Year:        2023,
	}

	// 候选 1：同系列（SeriesKey 匹配，+80 分）
	candSeries := model.FilmListSnapshot{
		Mid:         101,
		Name:        "仙逆 剧场版",
		SeriesKey:   "series_xianni",
		CategoryKey: "dongman:guoman",
		Hits:        10,
		UpdateStamp: now,
	}

	// 候选 2：同细类 + 题材标签重合 + 同年大陆（无系列，约 40+16+16=72 分）
	candGenre := model.FilmListSnapshot{
		Mid:         102,
		Name:        "凡人修仙传",
		SeriesKey:   "series_fanren",
		CategoryKey: "dongman:guoman",
		Cid:         10,
		ClassTag:    "修真,古装",
		Area:        "中国大陆",
		Language:    "国语",
		Year:        2023,
		Hits:        1000,
		UpdateStamp: now,
	}

	// 候选 3：完全不同品类的大陆热播电影（分值极低，仅靠 Hits）
	candIrrelevant := model.FilmListSnapshot{
		Mid:         103,
		Name:        "热播动作片",
		CategoryKey: "dianying:dongzuo",
		Hits:        99999,
		UpdateStamp: now,
	}

	ranked := rankRelatedCandidates(current, []model.FilmListSnapshot{candIrrelevant, candGenre, candSeries}, 2)
	if len(ranked) != 2 {
		t.Fatalf("expected 2 ranked results, got %d", len(ranked))
	}

	// 第一名必须是同系列（分值最高）
	if ranked[0].Mid != 101 {
		t.Errorf("expected 1st ranked film to be candSeries (101), got mid=%d", ranked[0].Mid)
	}

	// 第二名必须是同细类同题材动漫（而不是 Hits 99999 的无关动作片）
	if ranked[1].Mid != 102 {
		t.Errorf("expected 2nd ranked film to be candGenre (102), got mid=%d", ranked[1].Mid)
	}
}
