package access

import (
	"fmt"
	"sort"
	"strings"
	"time"

	"server/internal/infra/db"

	"github.com/redis/go-redis/v9"
)

func QueryTops(day, kind string, limit int) ([]TopItem, error) {
	return QueryTopsScope(day, kind, "", "", limit)
}

func scopedTopKind(kind, module, platform string) string {
	kind = strings.ToLower(strings.TrimSpace(kind))
	module = strings.ToLower(strings.TrimSpace(module))
	platform = strings.ToLower(strings.TrimSpace(platform))
	if kind == "path" {
		kind = "page"
	}
	switch kind {
	case "page", "play", "search", "classify":
		switch module {
		case "web":
			return "web_" + kind
		case "tvbox":
			return "tvbox_" + kind
		case "app":
			if platform == "android" || platform == "harmony" || platform == "ios" {
				return platform + "_" + kind
			}
			return "app_" + kind
		default:
			return kind
		}
	default:
		return kind
	}
}

func QueryTopsScope(day, kind, module, platform string, limit int) ([]TopItem, error) {
	if limit <= 0 {
		limit = 10
	}
	if limit > zsetKeep {
		limit = zsetKeep
	}
	fetch := limit
	if kind == "play" || kind == "classify" {
		fetch = playTopFetchCount(limit)
	}
	now := time.Now().In(time.Local)
	target, err := parseDay(day, now)
	if err != nil {
		return nil, err
	}
	module = strings.ToLower(strings.TrimSpace(module))
	platform = strings.ToLower(strings.TrimSpace(platform))
	if !isLocalToday(target, now) {
		if target.Before(retentionCutoff(now)) {
			return []TopItem{}, nil
		}
		dayStr := target.Format("2006-01-02")
		if _, ok := loadDailyStats(dayStr); ok {
			queryKind := scopedTopKind(kind, module, platform)
			items := loadDailyTops(dayStr, queryKind, fetch)
			if len(items) == 0 && queryKind == "page" {
				items = mergeTopItemsByCount(
					loadDailyTops(dayStr, "web_page", fetch),
					loadDailyTops(dayStr, "app_page", fetch),
				)
				if fetch > 0 && len(items) > fetch {
					items = items[:fetch]
				}
			}
			if kind == "play" {
				items = takePlayTops(items, limit)
			}
			if kind == "classify" {
				items = takeClassifyTops(items, limit)
			}
			if len(items) > 0 {
				return items, nil
			}
		}
		if db.Rdb == nil {
			return []TopItem{}, nil
		}
	}
	if db.Rdb == nil {
		return nil, fmt.Errorf("redis unavailable")
	}
	dayKey := target.Format("20060102")
	var key string
	switch kind {
	case "search":
		if module == "web" {
			key = webTopSearchKey(dayKey)
		} else if module == "app" {
			if platform != "" && platform != "all" {
				key = appTopSearchKey(platform, dayKey)
			} else {
				key = appAllTopSearchKey(dayKey)
			}
		} else if module == "tvbox" {
			key = tvboxTopSearchKey(dayKey)
		} else {
			key = topSearchKey(dayKey)
		}
	case "play":
		if module == "web" {
			key = webTopPlayKey(dayKey)
		} else if module == "app" {
			if platform != "" && platform != "all" {
				key = appTopPlayKey(platform, dayKey)
			} else {
				key = appAllTopPlayKey(dayKey)
			}
		} else if module == "tvbox" {
			key = tvboxTopPlayKey(dayKey)
		} else {
			key = topPlayKey(dayKey)
		}
	case "classify":
		if module == "web" {
			key = webTopClassifyKey(dayKey)
		} else if module == "app" {
			if platform != "" && platform != "all" {
				key = appTopClassifyKey(platform, dayKey)
			} else {
				key = appAllTopClassifyKey(dayKey)
			}
		} else if module == "tvbox" {
			key = tvboxTopClassifyKey(dayKey)
		} else {
			key = topClassifyKey(dayKey)
		}
	default:
		if module == "web" {
			key = webTopPageKey(dayKey)
		} else if module == "app" {
			if platform != "" && platform != "all" {
				key = appTopPageKey(platform, dayKey)
			} else {
				key = appAllTopPageKey(dayKey)
			}
		} else {
			key = topPathKey(dayKey)
		}
	}
	pairs, err := db.Rdb.ZRevRangeWithScores(db.Cxt, key, 0, int64(fetch-1)).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	items := make([]TopItem, 0, len(pairs))
	for _, p := range pairs {
		member, _ := p.Member.(string)
		items = append(items, TopItem{Key: member, Count: int64(p.Score)})
	}
	if kind == "play" {
		items = takePlayTops(items, limit)
	}
	if kind == "classify" {
		items = takeClassifyTops(items, limit)
	}
	return items, nil
}

func mergeTopItemsByCount(parts ...[]TopItem) []TopItem {
	counts := make(map[string]int64, 16)
	meta := make(map[string]TopItem, 16)
	for _, items := range parts {
		for _, it := range items {
			if it.Key == "" {
				continue
			}
			counts[it.Key] += it.Count
			if _, ok := meta[it.Key]; !ok {
				meta[it.Key] = it
			}
		}
	}
	out := make([]TopItem, 0, len(counts))
	for k, n := range counts {
		it := meta[k]
		it.Count = n
		out = append(out, it)
	}
	sort.Slice(out, func(i, j int) bool {
		if out[i].Count == out[j].Count {
			return out[i].Key < out[j].Key
		}
		return out[i].Count > out[j].Count
	})
	return out
}
