package utils

import (
	"sort"
	"testing"
)

func testFilmItem(mid int64, name string, hits int64) FilmSearchItem {
	item := FilmSearchItem{
		Mid:  mid,
		Name: name,
		Hits: hits,
	}
	FillSearchDerivedFields(&item)
	return item
}

func TestPinyinConversion(t *testing.T) {
	tests := []struct {
		input       string
		wantFull    string
		wantInitial string
	}{
		{"流浪地球 2", "liulangdiqiu2", "lldq2"},
		{"庆余年", "qingyunian", "qyn"},
		{"凡人修仙传", "fanrenxiuxianchuan", "frxxc"},
		{"哈利·波特与魔法石", "haliboteyumofashi", "hlbtymfs"},
		{"阿凡达：水之道", "afandashuizhidao", "afdszd"},
	}

	for _, tt := range tests {
		gotFull := ToPinyin(tt.input)
		gotInitial := ToPinyinInitials(tt.input)
		if gotFull != tt.wantFull {
			t.Errorf("ToPinyin(%q) = %q; want %q", tt.input, gotFull, tt.wantFull)
		}
		if gotInitial != tt.wantInitial {
			t.Errorf("ToPinyinInitials(%q) = %q; want %q", tt.input, gotInitial, tt.wantInitial)
		}
	}

	variants := ToPinyinInitialVariants("凡人修仙传")
	hasC, hasZ := false, false
	for _, v := range variants {
		if v == "frxxc" {
			hasC = true
		}
		if v == "frxxz" {
			hasZ = true
		}
	}
	if !hasC || !hasZ {
		t.Errorf("ToPinyinInitialVariants('凡人修仙传') should include frxxc and frxxz, got %#v", variants)
	}

	if !MatchPinyinInitials("frxxz", "凡人修仙传") {
		t.Error("MatchPinyinInitials('frxxz', '凡人修仙传') should be true with polyphone 'zhuan'")
	}
	if !MatchPinyinInitials("frxxc", "凡人修仙传") {
		t.Error("MatchPinyinInitials('frxxc', '凡人修仙传') should be true with polyphone 'chuan'")
	}
}

func TestNormalizeSearchText(t *testing.T) {
	input := " 慶餘年 ２： 第一季！ "
	norm := NormalizeSearchText(input)
	wantNorm := "庆余年 2 第一季"
	if norm != wantNorm {
		t.Errorf("NormalizeSearchText(%q) = %q; want %q", input, norm, wantNorm)
	}

	compact := CleanCompactText(input)
	wantCompact := "庆余年2第一季"
	if compact != wantCompact {
		t.Errorf("CleanCompactText(%q) = %q; want %q", input, compact, wantCompact)
	}
}

func TestExtractSearchTokens(t *testing.T) {
	tokens := ExtractSearchTokens("流浪 地球  2")
	if len(tokens) != 3 || tokens[0] != "流浪" || tokens[1] != "地球" || tokens[2] != "2" {
		t.Errorf("ExtractSearchTokens failed, got %#v", tokens)
	}
}

func TestNormalizeSearchSortField(t *testing.T) {
	cases := map[string]string{
		"":             "",
		"relevance":    "",
		"HITS":         "hits",
		"latest":       "latest",
		"update_stamp": "latest",
		"year":         "year",
		"rating":       "score",
		"score":        "score",
		"foo":          "",
	}
	for in, want := range cases {
		if got := NormalizeSearchSortField(in); got != want {
			t.Errorf("NormalizeSearchSortField(%q) = %q; want %q", in, got, want)
		}
	}
}

func TestIsSubsequence(t *testing.T) {
	if !IsSubsequence("凡人传", "凡人修仙传") {
		t.Error("IsSubsequence('凡人传', '凡人修仙传') should be true")
	}
	if !IsSubsequence("唐朝西行", "唐朝诡事录之西行") {
		t.Error("IsSubsequence('唐朝西行', '唐朝诡事录之西行') should be true")
	}
	if IsSubsequence("西行唐朝", "唐朝诡事录之西行") {
		t.Error("IsSubsequence('西行唐朝', '唐朝诡事录之西行') should be false")
	}
}

func TestScoreFilmMatchAndRanking(t *testing.T) {
	films := []FilmSearchItem{
		testFilmItem(1, "庆余年 第二季", 1000),
		testFilmItem(2, "庆余年 第一季", 500),
		testFilmItem(3, "关于庆余年的幕后花絮", 2000),
		testFilmItem(4, "流浪地球2", 8000),
		testFilmItem(5, "凡人修仙传", 3000),
	}

	// 1. 搜索空格分词 "庆余年 2"
	q1 := BuildQueryContext("庆余年 2")
	s1_1 := ScoreFilmMatch(films[0], q1)
	s1_3 := ScoreFilmMatch(films[2], q1)
	if s1_1 <= 0 {
		t.Errorf("ScoreFilmMatch for '庆余年 2' on '庆余年 第二季' should be > 0, got %d", s1_1)
	}
	if s1_3 != 0 {
		t.Errorf("ScoreFilmMatch for '庆余年 2' on '关于庆余年的幕后花絮' should be 0 (no '2'), got %d", s1_3)
	}

	// 2. 搜索拼音首字母简拼 "lldq"
	q2 := BuildQueryContext("lldq")
	s2_4 := ScoreFilmMatch(films[3], q2)
	if s2_4 < 300 {
		t.Errorf("ScoreFilmMatch for 'lldq' on '流浪地球2' should be >= 300, got %d", s2_4)
	}

	// 2b. 多音字简拼走预计算变体
	qPoly := BuildQueryContext("frxxz")
	sPoly := ScoreFilmMatch(films[4], qPoly)
	if sPoly < 300 {
		t.Errorf("ScoreFilmMatch for 'frxxz' on '凡人修仙传' should be >= 300, got %d", sPoly)
	}

	// 3. 搜索 "庆余年" 精确度排序验证：前缀命中必须高于包含命中
	q5 := BuildQueryContext("庆余年")
	scored := make([]struct {
		f     FilmSearchItem
		score int
	}, len(films))
	for i, f := range films {
		scored[i] = struct {
			f     FilmSearchItem
			score int
		}{f: f, score: ScoreFilmMatch(f, q5)}
	}
	sort.SliceStable(scored, func(i, j int) bool {
		if scored[i].score != scored[j].score {
			return scored[i].score > scored[j].score
		}
		return scored[i].f.Hits > scored[j].f.Hits
	})

	if scored[0].f.Mid != 1 && scored[0].f.Mid != 2 {
		t.Errorf("Top result for '庆余年' should be 正剧, got %s (score=%d)", scored[0].f.Name, scored[0].score)
	}

	// 4. 短英文不误伤
	qShort := BuildQueryContext("l")
	if ScoreFilmMatch(films[3], qShort) != 0 {
		t.Errorf("short ascii 'l' should not match 流浪地球2, got %d", ScoreFilmMatch(films[3], qShort))
	}

	// 5. 两字跳字不召回，三字跳字召回
	q2char := BuildQueryContext("凡传")
	if ScoreFilmMatch(films[4], q2char) != 0 {
		t.Errorf("2-char subsequence '凡传' should not match, got %d", ScoreFilmMatch(films[4], q2char))
	}
	q3char := BuildQueryContext("凡人传")
	if ScoreFilmMatch(films[4], q3char) <= 0 {
		t.Error("3-char subsequence '凡人传' should still match 凡人修仙传")
	}

	// 6. 拼音精准性：ceshi 命中 小夜测试
	filmXiaoYe := testFilmItem(10, "小夜测试", 100)
	filmChoice := testFilmItem(11, "选择之她·他", 200)
	qCeshi := BuildQueryContext("ceshi")
	if ScoreFilmMatch(filmXiaoYe, qCeshi) <= 0 {
		t.Errorf("expected 'ceshi' to match '小夜测试', got score %d", ScoreFilmMatch(filmXiaoYe, qCeshi))
	}
	if ScoreFilmMatch(filmChoice, qCeshi) != 0 {
		t.Errorf("expected 'ceshi' NOT to match '选择之她·他', got score %d", ScoreFilmMatch(filmChoice, qCeshi))
	}

	// 7. 音节独立性：xian 命中 仙逆，严禁误伤 项羽(xiangyu) 或 想你了(xiangnile)
	filmXianNi := testFilmItem(12, "仙逆", 500)
	filmXiangYu := testFilmItem(13, "项羽", 500)
	filmXiangNi := testFilmItem(14, "想你了", 500)
	qXian := BuildQueryContext("xian")
	if ScoreFilmMatch(filmXianNi, qXian) <= 0 {
		t.Errorf("expected 'xian' to match '仙逆', got score %d", ScoreFilmMatch(filmXianNi, qXian))
	}
	if ScoreFilmMatch(filmXiangYu, qXian) != 0 {
		t.Errorf("expected 'xian' NOT to match '项羽' (xiang != xian), got score %d", ScoreFilmMatch(filmXiangYu, qXian))
	}
	if ScoreFilmMatch(filmXiangNi, qXian) != 0 {
		t.Errorf("expected 'xian' NOT to match '想你了' (xiang != xian), got score %d", ScoreFilmMatch(filmXiangNi, qXian))
	}
}
