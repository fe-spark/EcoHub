package service

import (
	"strings"
	"testing"

	"server/internal/model"
)

func TestSanitizeTVBoxPlayURL(t *testing.T) {
	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "标准 https 链接",
			input:    "https://example.com/play/123/index.m3u8",
			expected: "https://example.com/play/123/index.m3u8",
		},
		{
			name:     "前置 $ 污染修复",
			input:    "$https://ukzy.ukubf3.com/20260129/PZyVfEtk/index.m3u8",
			expected: "https://ukzy.ukubf3.com/20260129/PZyVfEtk/index.m3u8",
		},
		{
			name:     "重复协议头 httphttps 修复",
			input:    "httphttps://ukzy.ukubf8.com/20261019/fU2PmLZB/index.m3u8",
			expected: "https://ukzy.ukubf8.com/20261019/fU2PmLZB/index.m3u8",
		},
		{
			name:     "连字缺失协议头 s:// 修复",
			input:    "s://ukzy.ukubf8.com/20261010/O6IDH9YS/index.m3u8",
			expected: "https://ukzy.ukubf8.com/20261010/O6IDH9YS/index.m3u8",
		},
		{
			name:     "清晰度前缀 720Phtps:// 修复",
			input:    "720Phtps://ukzyvod3.ukubf5.com/20260430/2cSm2wTr/index.m3u8",
			expected: "https://ukzyvod3.ukubf5.com/20260430/2cSm2wTr/index.m3u8",
		},
		{
			name:     "尾部标点符号与特殊字符剥离",
			input:    "https://domain.com/video/index.m3u8$",
			expected: "https://domain.com/video/index.m3u8",
		},
		{
			name:     "尾部中文标点符号剥离",
			input:    "https://domain.com/video/index.m3u8、、、、",
			expected: "https://domain.com/video/index.m3u8",
		},
		{
			name:     "URL 内部 $ 和 # 转义",
			input:    "https://domain.com/video/index.m3u8?token=abc$123#frag",
			expected: "https://domain.com/video/index.m3u8?token=abc%24123%23frag",
		},
		{
			name:     "携带 https 参数的合法 http 链接不被截断",
			input:    "http://jx.example.com/parse?url=https://cdn.example.com/1.m3u8",
			expected: "http://jx.example.com/parse?url=https://cdn.example.com/1.m3u8",
		},
		{
			name:     "重复协议头 hthttp 修复",
			input:    "hthttp://example.com/play/1.m3u8",
			expected: "http://example.com/play/1.m3u8",
		},
		{
			name:     "非 ASCII 大写前缀后接 https 修复",
			input:    "İhttps://example.com/play/1.m3u8",
			expected: "https://example.com/play/1.m3u8",
		},
		{
			name:     "非 HTTP(S) 协议 wss 丢弃且不被内嵌 https 劫持",
			input:    "wss://example.com/stream?next=https://cdn.example.com/1.m3u8",
			expected: "",
		},
		{
			name:     "非 HTTP(S) 协议 ipfs 丢弃",
			input:    "ipfs://bafybeigdyrzt5sfp7udm7hu76uh7y26nf3efuylqabf3oclgtqy55fbzdi",
			expected: "",
		},
		{
			name:     "非 HTTP(S) 协议 mms 丢弃",
			input:    "mms://example.com/live",
			expected: "",
		},
		{
			name:     "非法无协议链接丢弃",
			input:    "/index.m3u8",
			expected: "",
		},
		{
			name:     "空串丢弃",
			input:    "   ",
			expected: "",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := SanitizeTVBoxPlayURL(tt.input)
			if got != tt.expected {
				t.Errorf("SanitizeTVBoxPlayURL(%q) = %q, 期望 %q", tt.input, got, tt.expected)
			}
		})
	}
}

func TestSanitizeTVBoxEpisode(t *testing.T) {
	if got := SanitizeTVBoxEpisode("第01集", 0); got != "第01集" {
		t.Errorf("expected 第01集, got %q", got)
	}
	if got := SanitizeTVBoxEpisode("第$01#集", 0); got != "第＄01＃集" {
		t.Errorf("expected 第＄01＃集, got %q", got)
	}
	if got := SanitizeTVBoxEpisode("", 3); got != "第4集" {
		t.Errorf("expected 第4集, got %q", got)
	}
	if got := SanitizeTVBoxEpisode("  \n\t  ", -1); got != "正片" {
		t.Errorf("expected 正片, got %q", got)
	}
}

func TestSortTVBoxPlaySources(t *testing.T) {
	sources := []model.PlayLinkVo{
		{
			Id:       "src_uku#0",
			SourceId: "4194624554", // U酷高危源
			Name:     "U酷(UKU)",
			LinkList: []model.MovieUrlInfo{{Episode: "第1集", Link: "https://ukzy.com/1.m3u8"}},
		},
		{
			Id:       "src_subo#0",
			SourceId: "1016684692", // 速博优质源
			Name:     "速博(SUBO)",
			LinkList: []model.MovieUrlInfo{{Episode: "第1集", Link: "https://subo.com/1.m3u8"}},
		},
		{
			Id:       "src_ff#0",
			SourceId: "2898990914", // 非凡高危源
			Name:     "非凡(FF)",
			LinkList: []model.MovieUrlInfo{{Episode: "第1集", Link: "https://ffzy.com/1.m3u8"}},
		},
	}

	sorted := SortTVBoxPlaySources(sources)
	if len(sorted) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(sorted))
	}
	// 首选线路绝不能是 U酷 或 非凡，必须是速博
	if sorted[0].SourceId != "1016684692" {
		t.Fatalf("首选线路应为速博，实际为: %s (%s)", sorted[0].Name, sorted[0].SourceId)
	}
}

func TestFormatTVBoxFilmPlayLists_EmptyAndDirtyFilter(t *testing.T) {
	sources := []model.PlayLinkVo{
		{
			Id:       "src_empty#0",
			SourceId: "empty_src",
			Name:     "空线路源",
			LinkList: []model.MovieUrlInfo{
				{Episode: "第1集", Link: "   "},
				{Episode: "第2集", Link: "/invalid/path.m3u8"},
			},
		},
		{
			Id:       "src_valid#0",
			SourceId: "1016684692",
			Name:     "有效源",
			LinkList: []model.MovieUrlInfo{
				{Episode: "第1集", Link: "$https://subo.com/play/1.m3u8$"},
				{Episode: "第2集", Link: "httphttps://subo.com/play/2.m3u8"},
			},
		},
	}

	vodPlayFrom, vodPlayURL := FormatTVBoxFilmPlayLists(sources)

	// 空线路应被完全剔除
	if strings.Contains(vodPlayFrom, "空线路源") {
		t.Errorf("vodPlayFrom 不应包含空线路源: %s", vodPlayFrom)
	}
	fromCount := len(strings.Split(vodPlayFrom, "$$$"))
	urlCount := len(strings.Split(vodPlayURL, "$$$"))
	if fromCount != urlCount || fromCount != 1 {
		t.Fatalf("线路数量不匹配: from=%d, url=%d", fromCount, urlCount)
	}

	// 验证清洗后的 URL（集名和地址之间的 $ 为协议分隔符，地址本身不得含有 $https 或 %24https 或 httphttps）
	if strings.Contains(vodPlayURL, "$$https") || strings.Contains(vodPlayURL, "%24https") || strings.Contains(vodPlayURL, "httphttps") {
		t.Errorf("vodPlayURL 仍含有未清洗的脏前缀: %s", vodPlayURL)
	}
	if !strings.Contains(vodPlayURL, "https://subo.com/play/1.m3u8") || !strings.Contains(vodPlayURL, "https://subo.com/play/2.m3u8") {
		t.Errorf("vodPlayURL 未正确还原有效地址: %s", vodPlayURL)
	}
}

func TestSortTVBoxPlaySources_PreservesUpstreamOrderOnEqualWeight(t *testing.T) {
	sources := []model.PlayLinkVo{
		{
			Id:       "src_z#0",
			SourceId: "src_z",
			Name:     "高优采集站",
			LinkList: []model.MovieUrlInfo{{Episode: "第1集", Link: "https://z.example.com/1.m3u8"}},
		},
		{
			Id:       "src_a#2",
			SourceId: "src_a",
			Name:     "次优站线路2",
			LinkList: []model.MovieUrlInfo{{Episode: "第1集", Link: "https://a.example.com/2.m3u8"}},
		},
		{
			Id:       "src_a#10",
			SourceId: "src_a",
			Name:     "次优站线路10",
			LinkList: []model.MovieUrlInfo{{Episode: "第1集", Link: "https://a.example.com/10.m3u8"}},
		},
	}

	sorted := SortTVBoxPlaySources(sources)
	if len(sorted) != 3 {
		t.Fatalf("expected 3 sources, got %d", len(sorted))
	}
	want := []string{"src_z#0", "src_a#2", "src_a#10"}
	for i, id := range want {
		if sorted[i].Id != id {
			t.Fatalf("index %d: got %q, want %q", i, sorted[i].Id, id)
		}
	}
}
