package access

import (
	"context"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"

	"github.com/redis/go-redis/v9"
)

// 采集站归属在请求写入前决定。页面埋点的 source 是客户端类型，不能当作采集站。
var (
	lookupActiveCollectSourceID = func() string {
		active := repository.GetActiveCollectSource()
		if active == nil {
			return ""
		}
		return strings.TrimSpace(active.Id)
	}
	lookupCollectSourceExists = func(id string) bool {
		return repository.FindCollectSourceById(id) != nil
	}
	listCollectSources = func() []model.FilmSource {
		return repository.GetCollectSourceList()
	}
)

// SourceCallRow 某个采集站在选定日期的调用次数。
type SourceCallRow struct {
	Id        string `json:"id"`
	Name      string `json:"name"`
	Enabled   bool   `json:"enabled"`
	IsPrimary bool   `json:"isPrimary"`
	Count     int64  `json:"count"`
}

func validCollectSourceID(id string) bool {
	if id == "" || len(id) > 32 {
		return false
	}
	for _, r := range id {
		if r <= 0x20 || r == 0x7f {
			return false
		}
	}
	return true
}

func provideCollectSource(path string, query url.Values) string {
	if !strings.HasPrefix(path, "/api/provide/vod") || query == nil {
		return ""
	}
	raw := strings.TrimSpace(query.Get("source"))
	if raw != "" {
		if !validCollectSourceID(raw) {
			return ""
		}
		return raw
	}
	ac := strings.TrimSpace(query.Get("ac"))
	ids := strings.TrimSpace(query.Get("ids"))
	if (ac == "detail" || ac == "videolist") && ids != "" {
		return ""
	}
	return activeCollectSourceID()
}

func pageCollectSource(action, collectSource, resource string) string {
	switch action {
	case ActionBrowse, ActionClassify, ActionSearch:
		return activeCollectSourceID()
	case ActionPlay:
		id := strings.TrimSpace(collectSource)
		if validCollectSourceID(id) && lookupCollectSourceExists(id) {
			return id
		}
		liveID := liveCollectSourceID(resource)
		if validCollectSourceID(liveID) && lookupCollectSourceExists(liveID) {
			return liveID
		}
		return ""
	default:
		return ""
	}
}

func activeCollectSourceID() string {
	id := strings.TrimSpace(lookupActiveCollectSourceID())
	if !validCollectSourceID(id) {
		return ""
	}
	return id
}

func liveCollectSourceID(resource string) string {
	resource = strings.TrimSpace(resource)
	index := strings.LastIndex(resource, ":")
	if index <= 0 || index >= len(resource)-1 {
		return ""
	}
	return strings.TrimSpace(resource[:index])
}

func countCollectSource(pipe redis.Pipeliner, ctx context.Context, day, id string) {
	if pipe == nil || id == "" {
		return
	}
	key := collectSourceKey(day)
	pipe.HIncrBy(ctx, key, id, 1)
	pipe.ExpireNX(ctx, key, ttlDay)
}

// QuerySourceCalls 返回选定日期各采集站的调用次数。当前首选站只用于标记，不改归属。
func QuerySourceCalls(day string) ([]SourceCallRow, error) {
	target, err := parseDay(day, time.Now().In(time.Local))
	if err != nil {
		return nil, err
	}
	counts, err := loadCollectSourceCounts(target.Format("20060102"))
	if err != nil {
		return nil, err
	}
	primary := activeCollectSourceID()
	rows := make([]SourceCallRow, 0)
	seen := map[string]struct{}{}
	for _, source := range listCollectSources() {
		id := strings.TrimSpace(source.Id)
		if id == "" {
			continue
		}
		seen[id] = struct{}{}
		rows = append(rows, SourceCallRow{
			Id:        id,
			Name:      source.Name,
			Enabled:   source.State,
			IsPrimary: id == primary,
			Count:     counts[id],
		})
	}
	unknown := make([]SourceCallRow, 0)
	for id, count := range counts {
		if _, ok := seen[id]; ok || count <= 0 {
			continue
		}
		unknown = append(unknown, SourceCallRow{
			Id:        id,
			Name:      id,
			Enabled:   false,
			IsPrimary: false,
			Count:     count,
		})
	}
	sort.Slice(unknown, func(i, j int) bool {
		if unknown[i].Count != unknown[j].Count {
			return unknown[i].Count > unknown[j].Count
		}
		return unknown[i].Id < unknown[j].Id
	})
	return append(rows, unknown...), nil
}

func loadCollectSourceCounts(dayKey string) (map[string]int64, error) {
	counts := map[string]int64{}
	if db.Rdb == nil {
		return counts, nil
	}
	raw, err := db.Rdb.HGetAll(db.Cxt, collectSourceKey(dayKey)).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	for id, text := range raw {
		if !validCollectSourceID(id) {
			continue
		}
		n, convErr := strconv.ParseInt(text, 10, 64)
		if convErr != nil || n <= 0 {
			continue
		}
		counts[id] = n
	}
	return counts, nil
}
