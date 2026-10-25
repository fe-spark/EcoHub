package model

import (
	"regexp"
	"strings"
)

var (
	playEpisodeRangeRe = regexp.MustCompile(`\d+\s*[-~～到至]\s*\d+`)
	playEpisodeNumRe   = regexp.MustCompile(`(?i)(?:第\s*0*(\d+)\s*[集话回期]|^(?:ep|e)?\s*0*(\d+)$)`)
)

// DedupPlayURLs 同一集只保留第一次出现的地址。第01集和第1集视为同一集。
// 区间标题（第1-12集）和无法识别的标题按原文区分，避免误并。
func DedupPlayURLs(links []MovieUrlInfo) []MovieUrlInfo {
	if len(links) < 2 {
		return links
	}
	seen := make(map[string]struct{}, len(links))
	out := make([]MovieUrlInfo, 0, len(links))
	for _, item := range links {
		key := playEpisodeKey(item.Episode)
		if key == "" {
			out = append(out, item)
			continue
		}
		if _, ok := seen[key]; ok {
			continue
		}
		seen[key] = struct{}{}
		out = append(out, item)
	}
	return out
}

func playEpisodeKey(name string) string {
	name = strings.TrimSpace(name)
	if name == "" {
		return ""
	}
	if playEpisodeRangeRe.MatchString(name) {
		return "raw:" + strings.ToLower(strings.Join(strings.Fields(name), ""))
	}
	if m := playEpisodeNumRe.FindStringSubmatch(name); m != nil {
		n := m[1]
		if n == "" {
			n = m[2]
		}
		n = strings.TrimLeft(n, "0")
		if n == "" {
			n = "0"
		}
		return "n:" + n
	}
	return "raw:" + strings.ToLower(strings.Join(strings.Fields(name), ""))
}
