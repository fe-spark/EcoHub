package utils

import (
	"testing"
)

func TestParseDomainReplaceRules(t *testing.T) {
	raw := `
# 这是一个注释行
old.com => new.com
// 箭头语法
http://site1.com -> https://site2.com
; 逗号语法
old3.com:8080,new3.com:8443
# 空格分隔
old4.com new4.com
*.cdn.com => *.fastcdn.com
http://old5.com/vod => https://new5.com/live/
`
	rules := ParseDomainReplaceRules(raw)
	if len(rules) != 6 {
		t.Fatalf("expected 6 rules, got %d", len(rules))
	}

	if rules[0].FromHost != "old.com" || rules[0].ToHost != "new.com" {
		t.Errorf("rule 0 mismatch: %+v", rules[0])
	}
	if rules[1].FromScheme != "http" || rules[1].ToScheme != "https" || rules[1].FromHost != "site1.com" || rules[1].ToHost != "site2.com" {
		t.Errorf("rule 1 mismatch: %+v", rules[1])
	}
	if rules[2].FromHost != "old3.com:8080" || rules[2].ToHost != "new3.com:8443" {
		t.Errorf("rule 2 mismatch: %+v", rules[2])
	}
	if rules[3].FromHost != "old4.com" || rules[3].ToHost != "new4.com" {
		t.Errorf("rule 3 mismatch: %+v", rules[3])
	}
	if rules[4].FromHost != "*.cdn.com" || rules[4].ToHost != "*.fastcdn.com" {
		t.Errorf("rule 4 mismatch: %+v", rules[4])
	}
	if rules[5].FromPath != "/vod" || rules[5].ToPath != "/live" {
		t.Errorf("rule 5 mismatch: %+v", rules[5])
	}
}

func TestApplyDomainReplaceRules(t *testing.T) {
	rules := ParseDomainReplaceRules(`
old.com => new.com
http://upgrade.com => https://upgrade.com
oldport.com:8080 => newport.com:8443
*.wildcard.com => *.target.com
oldpath.com/vod => newpath.com/hls
`)

	tests := []struct {
		name     string
		input    string
		expected string
	}{
		{
			name:     "simple domain replace",
			input:    "https://old.com/20230520/index.m3u8?sign=abc#t=10",
			expected: "https://new.com/20230520/index.m3u8?sign=abc#t=10",
		},
		{
			name:     "case insensitive domain replace",
			input:    "http://OLD.COM/test.mp4",
			expected: "http://new.com/test.mp4",
		},
		{
			name:     "scheme upgrade",
			input:    "http://upgrade.com/video.m3u8",
			expected: "https://upgrade.com/video.m3u8",
		},
		{
			name:     "scheme mismatch does not replace",
			input:    "https://upgrade.com/video.m3u8",
			expected: "https://upgrade.com/video.m3u8",
		},
		{
			name:     "port replacement",
			input:    "http://oldport.com:8080/stream.m3u8",
			expected: "http://newport.com:8443/stream.m3u8",
		},
		{
			name:     "port mismatch does not replace",
			input:    "http://oldport.com:9000/stream.m3u8",
			expected: "http://oldport.com:9000/stream.m3u8",
		},
		{
			name:     "wildcard subdomain replacement",
			input:    "https://node1.wildcard.com/path/file.m3u8",
			expected: "https://node1.target.com/path/file.m3u8",
		},
		{
			name:     "path prefix replacement",
			input:    "http://oldpath.com/vod/episode1.m3u8",
			expected: "http://newpath.com/hls/episode1.m3u8",
		},
		{
			name:     "unmatched domain unchanged",
			input:    "https://otherdomain.com/index.m3u8",
			expected: "https://otherdomain.com/index.m3u8",
		},
		{
			name:     "partial domain string not replaced (safety check)",
			input:    "https://notold.com/index.m3u8",
			expected: "https://notold.com/index.m3u8",
		},
		{
			name:     "path prefix boundary avoids false positive (e.g. /vod vs /vodka)",
			input:    "http://oldpath.com/vodka/episode1.m3u8",
			expected: "http://oldpath.com/vodka/episode1.m3u8",
		},
		{
			name:     "domain replacement preserves original port if rule does not specify port",
			input:    "http://old.com:7777/video.m3u8",
			expected: "http://new.com:7777/video.m3u8",
		},
		{
			name:     "empty input",
			input:    "",
			expected: "",
		},
		{
			name:     "rtmp exact host replace",
			input:    "rtmp://old.com/live",
			expected: "rtmp://new.com/live",
		},
		{
			name:     "rtmp substring host not replaced",
			input:    "rtmp://notold.com/live",
			expected: "rtmp://notold.com/live",
		},
		{
			name:     "magnet tracker host boundary replace",
			input:    "magnet:?xt=urn:btih:abc&tr=http://old.com/announce",
			expected: "magnet:?xt=urn:btih:abc&tr=http://new.com/announce",
		},
		{
			name:     "magnet substring host not replaced",
			input:    "magnet:?xt=urn:btih:abc&dn=notold.com",
			expected: "magnet:?xt=urn:btih:abc&dn=notold.com",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			actual := ApplyDomainReplaceRules(tt.input, rules)
			if actual != tt.expected {
				t.Errorf("got %q, want %q", actual, tt.expected)
			}
		})
	}
}

func TestValidateDomainReplaceRules(t *testing.T) {
	if err := ValidateDomainReplaceRules(""); err != nil {
		t.Fatalf("empty rules should be valid: %v", err)
	}
	if err := ValidateDomainReplaceRules("# comment\nold.com => new.com\n"); err != nil {
		t.Fatalf("valid rules rejected: %v", err)
	}
	if err := ValidateDomainReplaceRules("old.com > new.com"); err == nil {
		t.Fatal("wrong arrow should be rejected")
	}
}
