package access

import (
	"fmt"
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
	case "page", "play", "search":
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
			if kind == "page" {
				return "web_page"
			}
			return kind
		}
	case "classify":
		return "classify"
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
	if kind == "play" {
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
			if kind == "play" {
				items = takePlayTops(items, limit)
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
		key = topClassifyKey(dayKey)
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
	return items, nil
}
