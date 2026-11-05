package snapshot

import "strings"

// countMultiSourceBySource 按当前索引重算各站多源聚合。
// 一部影片自己有两条及以上播放线路，或同一片名出现在另一个采集站，都计入所属站。
func countMultiSourceBySource(items []FilmSearchMeta) map[string]int64 {
	sourceByKey := make(map[string]string, len(items)/2)
	multiSourceKey := make(map[string]bool, len(items)/2)
	keys := make([]string, len(items))
	for i := range items {
		item := &items[i]
		key := itemAggregateKey(item)
		keys[i] = key
		src := strings.TrimSpace(item.FirstSourceId)
		if key == "" || src == "" {
			continue
		}
		if firstSrc, ok := sourceByKey[key]; ok {
			if firstSrc != src {
				multiSourceKey[key] = true
			}
		} else {
			sourceByKey[key] = src
		}
	}
	counts := make(map[string]int64)
	for i := range items {
		item := &items[i]
		src := strings.TrimSpace(item.FirstSourceId)
		if src == "" {
			continue
		}
		if len(item.PlaySources) >= 2 || (keys[i] != "" && multiSourceKey[keys[i]]) {
			counts[src]++
		}
	}
	return counts
}
