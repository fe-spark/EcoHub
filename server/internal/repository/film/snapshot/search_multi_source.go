package snapshot

import "strings"

// patchMultiSourceCounts 沿用上一版计数，只重算本次片名或播放线路有变动的聚合键。
func patchMultiSourceCounts(prev map[string]int64, oldItems []FilmSearchMeta, upsert map[int64]FilmSearchMeta, pos map[int64]int) map[string]int64 {
	counts := make(map[string]int64, len(prev)+len(upsert))
	for src, n := range prev {
		if n > 0 {
			counts[src] = n
		}
	}
	if len(upsert) == 0 {
		return counts
	}
	keys := make(map[string]struct{})
	for mid, meta := range upsert {
		if key := itemAggregateKey(&meta); key != "" {
			keys[key] = struct{}{}
		}
		if idx, ok := pos[mid]; ok && idx >= 0 && idx < len(oldItems) && oldItems[idx].Mid == mid {
			if key := itemAggregateKey(&oldItems[idx]); key != "" {
				keys[key] = struct{}{}
			}
		}
	}
	adjustMultiSource(counts, -1, filmsForKeys(oldItems, nil, keys))
	adjustMultiSource(counts, 1, filmsForKeys(oldItems, upsert, keys))
	for mid, meta := range upsert {
		newKey := itemAggregateKey(&meta)
		if idx, ok := pos[mid]; ok && idx >= 0 && idx < len(oldItems) && oldItems[idx].Mid == mid {
			old := &oldItems[idx]
			if itemAggregateKey(old) == "" {
				if oldSrc := strings.TrimSpace(old.FirstSourceId); oldSrc != "" && len(old.PlaySources) >= 2 {
					counts[oldSrc]--
				}
			}
		}
		if newKey == "" {
			if src := strings.TrimSpace(meta.FirstSourceId); src != "" && len(meta.PlaySources) >= 2 {
				counts[src]++
			}
		}
	}
	for src, n := range counts {
		if n <= 0 {
			delete(counts, src)
		}
	}
	return counts
}

type multiSourceFilm struct {
	src   string
	plays int
}

func filmsForKeys(oldItems []FilmSearchMeta, upsert map[int64]FilmSearchMeta, keys map[string]struct{}) map[string][]multiSourceFilm {
	out := make(map[string][]multiSourceFilm)
	note := func(item *FilmSearchMeta) {
		key := itemAggregateKey(item)
		if key == "" {
			return
		}
		if _, ok := keys[key]; !ok {
			return
		}
		out[key] = append(out[key], multiSourceFilm{
			src:   strings.TrimSpace(item.FirstSourceId),
			plays: len(item.PlaySources),
		})
	}
	for i := range oldItems {
		if _, replaced := upsert[oldItems[i].Mid]; replaced {
			continue
		}
		note(&oldItems[i])
	}
	for mid, meta := range upsert {
		item := meta
		item.Mid = mid
		note(&item)
	}
	return out
}

func adjustMultiSource(counts map[string]int64, sign int64, groups map[string][]multiSourceFilm) {
	for _, films := range groups {
		sources := make(map[string]struct{})
		for _, film := range films {
			if film.src != "" {
				sources[film.src] = struct{}{}
			}
		}
		multi := len(sources) > 1
		for _, film := range films {
			if film.src == "" {
				continue
			}
			if film.plays >= 2 || multi {
				counts[film.src] += sign
			}
		}
	}
}

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
