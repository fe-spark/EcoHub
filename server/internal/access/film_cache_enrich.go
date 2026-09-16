package access

import (
	"fmt"
	"strings"
	"time"
)

func isTvboxPlay(path, query string) bool {
	if strings.HasPrefix(path, "/api/provide/vod") {
		return (strings.Contains(query, "ac=detail") || strings.Contains(query, "ac=videolist") || strings.Contains(query, "ids=")) && strings.Contains(query, "ids=")
	}
	return false
}

// enrichLogEvents 为访问流水记录按动作类型精准补齐详情信息
func enrichLogEvents(events []AccessEvent) []AccessEvent {
	if len(events) == 0 {
		return events
	}
	filmIDs := make([]int64, 0, len(events))
	catIDs := make([]int64, 0, len(events))

	for _, it := range events {
		res := strings.TrimSpace(it.Resource)
		if idx := strings.Index(res, ","); idx > 0 {
			res = strings.TrimSpace(res[:idx])
		}

		// 仅点播行为才提取影片 ID，分类筛选与搜索严禁当做影片处理
		// 若事件自带不可变快照（已有合法片名），直接使用快照，无需反查
		if it.Action == ActionPlay || strings.HasPrefix(it.Path, "/api/filmPlayInfo") || isTvboxPlay(it.Path, it.Query) {
			if it.ResourceTitle != "" && !strings.HasPrefix(it.ResourceTitle, "影片 #") {
				continue
			}
			if id, ok := parseFilmID(res); ok {
				filmIDs = append(filmIDs, id)
			}
		} else if it.Action == ActionClassify {
			if it.ResourceCat != "" && !strings.HasPrefix(it.ResourceCat, "分类 #") {
				continue
			}
			if id, ok := parseFilmID(res); ok {
				catIDs = append(catIDs, id)
			}
		}
	}

	metaMap := resolveFilmMetas(filmIDs)
	catMap := resolveCategoryNames(catIDs)

	for i := range events {
		it := &events[i]
		res := strings.TrimSpace(it.Resource)
		if idx := strings.Index(res, ","); idx > 0 {
			res = strings.TrimSpace(res[:idx])
		}

		if it.Action == ActionPlay || strings.HasPrefix(it.Path, "/api/filmPlayInfo") || isTvboxPlay(it.Path, it.Query) {
			// 若未固化片名，才尝试补齐
			if it.ResourceTitle == "" || strings.HasPrefix(it.ResourceTitle, "影片 #") {
				if id, ok := parseFilmID(res); ok {
					if meta, ok := metaMap[id]; ok {
						it.ResourceTitle = meta.Title
						it.ResourcePoster = meta.Poster
						it.ResourceCat = meta.Category
					} else if it.ResourceTitle == "" {
						it.ResourceTitle = fmt.Sprintf("影片 #%d", id)
					}
				}
			}
		} else if it.Action == ActionClassify {
			// 若未固化分类名，才尝试补齐
			if it.ResourceCat == "" || strings.HasPrefix(it.ResourceCat, "分类 #") {
				if id, ok := parseFilmID(res); ok {
					if name, ok := catMap[id]; ok && name != "" {
						it.ResourceCat = name
					} else if it.ResourceCat == "" {
						it.ResourceCat = fmt.Sprintf("分类 #%d", id)
					}
				}
			}
		}
	}
	return events
}

func sanitizePosterURL(s string) string {
	s = strings.TrimSpace(s)
	if s == "" || strings.ContainsAny(s, "\r\n") {
		return ""
	}
	lower := strings.ToLower(s)
	if strings.HasPrefix(lower, "https://") || strings.HasPrefix(lower, "http://") {
		return s
	}
	return ""
}

func getFilmMetaFromMemory(id int64) (filmMetaCacheItem, bool) {
	filmMetaCacheMu.RLock()
	defer filmMetaCacheMu.RUnlock()
	item, ok := filmMetaCache[id]
	if !ok || time.Since(item.CachedAt) >= filmMetaCacheTTL {
		return filmMetaCacheItem{}, false
	}
	return item, true
}

func getCategoryNameFromMemory(id int64) (string, bool) {
	catNameCacheMu.RLock()
	defer catNameCacheMu.RUnlock()
	if time.Since(catNameCacheAt) >= 5*time.Minute {
		return "", false
	}
	name, ok := catNameCache[id]
	return name, ok
}

func snapshotAccessEvent(evt *AccessEvent) {
	if evt == nil {
		return
	}
	evt.ResourcePoster = sanitizePosterURL(evt.ResourcePoster)

	res := strings.TrimSpace(evt.Resource)
	if idx := strings.Index(res, ","); idx > 0 {
		res = strings.TrimSpace(res[:idx])
	}

	if evt.Action == ActionPlay || strings.HasPrefix(evt.Path, "/api/filmPlayInfo") || isTvboxPlay(evt.Path, evt.Query) {
		// 若已有合法片名快照（如客户端/上游已提供），直接复用，杜绝重复查库
		if evt.ResourceTitle != "" && !strings.HasPrefix(evt.ResourceTitle, "影片 #") {
			return
		}
		id, ok := parseFilmID(res)
		if !ok {
			return
		}
		// 写入链路严禁同步查库阻断采集协程，仅从纯内存缓存中尝试获取，未命中则保留空值待查询端批量懒补齐
		m, found := getFilmMetaFromMemory(id)
		if !found || strings.HasPrefix(m.Title, "影片 #") {
			return
		}
		evt.ResourceTitle = m.Title
		if p := sanitizePosterURL(m.Poster); p != "" {
			evt.ResourcePoster = p
		}
		if m.Category != "" {
			evt.ResourceCat = m.Category
		}
		return
	}

	if evt.Action != ActionClassify {
		return
	}
	// 若已有合法分类名快照，直接复用
	if evt.ResourceCat != "" && !strings.HasPrefix(evt.ResourceCat, "分类 #") {
		if evt.ResourceTitle == "" {
			evt.ResourceTitle = evt.ResourceCat
		}
		return
	}
	id, ok := parseFilmID(res)
	if !ok {
		return
	}
	name, found := getCategoryNameFromMemory(id)
	if !found || name == "" || strings.HasPrefix(name, "分类 #") {
		return
	}
	evt.ResourceCat = name
	if evt.ResourceTitle == "" {
		evt.ResourceTitle = name
	}
}
