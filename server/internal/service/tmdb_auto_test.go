package service

import (
	"context"
	"testing"

	"server/internal/model"
)

func TestExtractCleanTitle(t *testing.T) {
	cases := []struct {
		input    string
		expected string
	}{
		{"流浪地球2（4K）", "流浪地球2"},
		{"狂飙(全39集)", "狂飙"},
		{"三体【腾讯版】", "三体"},
		{"奥本海默[IMAX]", "奥本海默"},
		{"普通片名", "普通片名"},
		{"  前导空格（测试）  ", "前导空格"},
	}

	for _, c := range cases {
		actual := extractCleanTitle(c.input)
		if actual != c.expected {
			t.Errorf("extractCleanTitle(%q) = %q, expected %q", c.input, actual, c.expected)
		}
	}
}

func TestAutoScrapeFilm_CanceledContext(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	snap := model.FilmListSnapshot{
		Mid:  1001,
		Name: "测试影片",
	}

	_, success, err := TMDBSvc.AutoScrapeFilm(ctx, snap)
	if success {
		t.Errorf("expected success to be false on canceled context")
	}
	if err != context.Canceled {
		t.Errorf("expected context.Canceled error, got %v", err)
	}
}

func TestNormalizeComparableTitle(t *testing.T) {
	cases := []struct {
		name     string
		input1   string
		input2   string
		expected bool
	}{
		{
			name:     "蜡笔小新点号与波浪号差异",
			input1:   "蜡笔小新：爆盛！功夫男孩.拉面大乱.",
			input2:   "蜡笔小新：爆盛！功夫男孩〜拉面大乱〜",
			expected: true,
		},
		{
			name:     "蜘蛛侠冒号与点号差异",
			input1:   "蜘蛛侠：英雄归来",
			input2:   "蜘蛛侠.英雄归来",
			expected: true,
		},
		{
			name:     "哈利波特间隔号与无间隔号",
			input1:   "哈利·波特与魔法石",
			input2:   "哈利波特与魔法石",
			expected: true,
		},
		{
			name:     "不同影片不应匹配",
			input1:   "蜡笔小新：爆盛！功夫男孩.拉面大乱.",
			input2:   "蜡笔小新：呼风唤雨！夕阳下的春日部男孩",
			expected: false,
		},
	}

	for _, c := range cases {
		norm1 := normalizeComparableTitle(c.input1)
		norm2 := normalizeComparableTitle(c.input2)
		match := norm1 != "" && norm1 == norm2
		if match != c.expected {
			t.Errorf("[%s] norm1=%q, norm2=%q, matched=%v, expected=%v", c.name, norm1, norm2, match, c.expected)
		}
	}
}

func TestCalculateTitleSimilarity(t *testing.T) {
	// 蜡笔小新真实案例：两岸三地译名微调（功夫男孩 vs 功夫小子，拉面大乱 vs 拉面大乱斗）
	s1 := normalizeComparableTitle("蜡笔小新：爆盛！功夫男孩.拉面大乱.")
	s2 := normalizeComparableTitle("蜡笔小新：爆盛！功夫小子～拉面大乱斗～")
	sim := calculateTitleSimilarity(s1, s2)
	if sim < 0.70 {
		t.Errorf("expected similarity >= 0.70 for 蜡笔小新功夫男孩 vs 功夫小子, got %.3f", sim)
	}

	// 误判防范：不同剧场版
	diffMovie := normalizeComparableTitle("蜡笔小新：呼风唤雨！夕阳下的春日部男孩")
	simDiff := calculateTitleSimilarity(s1, diffMovie)
	if simDiff >= 0.50 {
		t.Errorf("expected similarity < 0.50 for different movie, got %.3f", simDiff)
	}

	// 季番防范：电视剧单名不应与长剧场版混淆
	tvSeries := normalizeComparableTitle("蜡笔小新")
	simTV := calculateTitleSimilarity(s1, tvSeries)
	if simTV >= 0.60 {
		t.Errorf("expected similarity < 0.60 for series vs movie, got %.3f", simTV)
	}
}


