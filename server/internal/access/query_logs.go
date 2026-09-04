package access

import (
	"encoding/json"
	"fmt"
	"sort"
	"strconv"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"

	"github.com/redis/go-redis/v9"
)

func QueryLogs(day, source, status, client, q string, limit int) ([]AccessEvent, error) {
	return QueryLogsScope(day, source, status, client, q, "", "", limit)
}

func QueryLogsScope(day, source, status, client, q, module, platform string, limit int) ([]AccessEvent, error) {
	if db.Rdb == nil {
		return nil, fmt.Errorf("redis unavailable")
	}
	now := time.Now().In(time.Local)
	target, err := parseDay(day, now)
	if err != nil {
		return nil, err
	}
	dayKey := target.Format("20060102")
	source = strings.ToLower(strings.TrimSpace(source))
	module = strings.ToLower(strings.TrimSpace(module))
	platform = strings.ToLower(strings.TrimSpace(platform))
	q = strings.ToLower(strings.TrimSpace(q))

	max := config.AccessRecentLimit
	if source == "slow" || source == "error" {
		max = slowKeep
	}
	if max < 1 {
		max = 200
	}
	if limit <= 0 {
		limit = max
	}
	if limit > max {
		limit = max
	}

	fetch := int64(config.AccessRecentLimit)
	if source == "slow" || source == "error" {
		fetch = slowKeep
	}
	if fetch < 1 {
		fetch = 200
	}

	raw, err := loadRecentLogLines(recentKeysForModule(module, dayKey), fetch)
	if err != nil {
		return nil, err
	}
	events := parseRecentLogEvents(raw)
	sort.SliceStable(events, func(i, j int) bool {
		return events[i].Ts.After(events[j].Ts)
	})

	loc := time.Local
	targetY, targetM, targetD := target.Date()
	client = strings.TrimSpace(client)
	out := make([]AccessEvent, 0, limit)
	for _, evt := range events {
		evtLoc := evt.Ts.In(loc)
		ey, em, ed := evtLoc.Date()
		if ey != targetY || em != targetM || ed != targetD {
			continue
		}
		if !matchLogSource(source, evt) {
			continue
		}
		if module == "web" && evt.ClientType != "web" {
			continue
		}
		if module == "app" && (evt.ClientType == "web" || evt.ClientType == "tvbox" || evt.ClientType == "manage") {
			continue
		}
		if module == "tvbox" && evt.ClientType != "tvbox" {
			continue
		}
		if platform != "" && platform != "all" && evt.ClientType != platform {
			continue
		}
		if client != "" && evt.ClientType != client {
			continue
		}
		if status != "" && !matchStatus(status, evt.Status) {
			continue
		}
		if q != "" {
			matched := strings.Contains(strings.ToLower(evt.Path), q) ||
				strings.Contains(strings.ToLower(evt.Page), q) ||
				strings.Contains(strings.ToLower(evt.PageTitle), q) ||
				strings.Contains(strings.ToLower(evt.Resource), q) ||
				strings.Contains(strings.ToLower(evt.ResourceTitle), q) ||
				strings.Contains(strings.ToLower(evt.DeviceId), q) ||
				strings.Contains(strings.ToLower(evt.Action), q) ||
				strings.Contains(strings.ToLower(evt.IPPreview), q)
			if !matched {
				continue
			}
		}
		out = append(out, evt)
		if len(out) >= limit {
			break
		}
	}
	return enrichLogEvents(out), nil
}

func recentKeysForModule(module, dayKey string) []string {
	switch module {
	case "web":
		return []string{webRecentDayKey(dayKey)}
	case "app":
		return []string{appRecentDayKey(dayKey)}
	case "tvbox":
		return []string{tvboxRecentDayKey(dayKey)}
	default:
		return []string{
			webRecentDayKey(dayKey),
			appRecentDayKey(dayKey),
			tvboxRecentDayKey(dayKey),
		}
	}
}

func loadRecentLogLines(keys []string, fetch int64) ([]string, error) {
	if fetch < 1 {
		fetch = 100
	}
	out := make([]string, 0, int(fetch)*len(keys))
	for _, key := range keys {
		raw, err := db.Rdb.LRange(db.Cxt, key, 0, fetch-1).Result()
		if err != nil && err != redis.Nil {
			return nil, err
		}
		out = append(out, raw...)
	}
	return out, nil
}

func parseRecentLogEvents(raw []string) []AccessEvent {
	events := make([]AccessEvent, 0, len(raw))
	for _, line := range raw {
		var evt AccessEvent
		if json.Unmarshal([]byte(line), &evt) != nil {
			continue
		}
		events = append(events, evt)
	}
	return events
}

func matchLogSource(source string, evt AccessEvent) bool {
	switch source {
	case "slow":
		return evt.LatencyMs >= config.AccessSlowMs
	case "error":
		return evt.Status >= 400
	default:
		return true
	}
}

func matchStatus(filter string, status int) bool {
	switch filter {
	case "2xx":
		return status >= 200 && status < 300
	case "3xx":
		return status >= 300 && status < 400
	case "4xx":
		return status >= 400 && status < 500
	case "5xx":
		return status >= 500 && status < 600
	case "error":
		return status >= 400
	case "all", "":
		return true
	default:
		if s, err := strconv.Atoi(filter); err == nil {
			return status == s
		}
		return true
	}
}

func matchQuery(q string, evt *AccessEvent) bool {
	q = strings.ToLower(strings.TrimSpace(q))
	if q == "" {
		return true
	}
	if evt == nil {
		return false
	}
	if strings.Contains(strings.ToLower(evt.Path), q) {
		return true
	}
	if strings.Contains(strings.ToLower(evt.IPPreview), q) {
		return true
	}
	if strings.Contains(strings.ToLower(evt.Resource), q) {
		return true
	}
	return false
}
