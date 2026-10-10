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
	if raw != "" && validCollectSourceID(raw) {
		return raw
	}
	return ""
}

func pageCollectSource(action, collectSource, resource string) string {
	if action != ActionPlay {
		return ""
	}
	id := strings.TrimSpace(collectSource)
	if id != "" {
		if validCollectSourceID(id) && lookupCollectSourceExists(id) {
			return id
		}
		return ""
	}
	liveID := liveCollectSourceID(resource)
	if liveID != "" {
		if validCollectSourceID(liveID) && lookupCollectSourceExists(liveID) {
			return liveID
		}
		return ""
	}
	return activeCollectSourceID()
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

// QuerySourceCalls 返回选定日期有调用的各片源站计数，按调用量降序排列。
func QuerySourceCalls(day string) ([]SourceCallRow, error) {
	target, err := parseDay(day, time.Now().In(time.Local))
	if err != nil {
		return nil, err
	}
	counts, err := loadCollectSourceCounts(target.Format("20060102"))
	if err != nil {
		return nil, err
	}
	if len(counts) == 0 {
		return []SourceCallRow{}, nil
	}

	primary := activeCollectSourceID()
	sourceMap := map[string]model.FilmSource{}
	for _, source := range listCollectSources() {
		id := strings.TrimSpace(source.Id)
		if id != "" {
			sourceMap[id] = source
		}
	}

	rows := make([]SourceCallRow, 0, len(counts))
	for id, count := range counts {
		if count <= 0 {
			continue
		}
		if s, ok := sourceMap[id]; ok {
			rows = append(rows, SourceCallRow{
				Id:        id,
				Name:      s.Name,
				Enabled:   s.State,
				IsPrimary: id == primary,
				Count:     count,
			})
		} else {
			rows = append(rows, SourceCallRow{
				Id:        id,
				Name:      id,
				Enabled:   false,
				IsPrimary: id == primary,
				Count:     count,
			})
		}
	}

	sort.Slice(rows, func(i, j int) bool {
		if rows[i].Count != rows[j].Count {
			return rows[i].Count > rows[j].Count
		}
		return rows[i].Id < rows[j].Id
	})
	return rows, nil
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
