package model

import "strings"

// DedupPlayURLs 同一条线路里，播放地址完全相同（去首尾空白后）的条目只保留第一次出现的那条，保持原有顺序。
// 不按集名合并：综艺常见「第1期上/第1期下/第1期上纯享」，集号相同但内容不同；
// 源站也会出现同名不同地址的条目（如两条「花絮特辑」），都要保留。
func DedupPlayURLs(links []MovieUrlInfo) []MovieUrlInfo {
	if len(links) < 2 {
		return links
	}
	seen := make(map[string]struct{}, len(links))
	out := make([]MovieUrlInfo, 0, len(links))
	for _, item := range links {
		key := strings.TrimSpace(item.Link)
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
