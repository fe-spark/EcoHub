package query

import (
	"errors"
	"sort"
	"strconv"
	"strings"
	"time"

	"server/internal/analytics/ingest"
	"server/internal/analytics/rollup"
	"server/internal/analytics/store"
	"server/internal/analytics/types"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/sourcecatalog"

	"github.com/redis/go-redis/v9"
)

var (
	lookupActiveSourceID = func() string {
		active := sourcecatalog.CurrentPrimarySource()
		if active == nil {
			return ""
		}
		return strings.TrimSpace(active.Id)
	}
	listCollectSources = func() ([]model.FilmSource, error) {
		return sourcecatalog.GetCollectSourceListDB(db.Mdb)
	}
)

// QuerySourceCalls 查询选定日期各片源站的调用/点播分布（今日读 Redis，历史日支持 MySQL 回退）
func QuerySourceCalls(day string) ([]types.SourceCallRow, error) {
	now := time.Now().In(time.Local)
	target, err := store.ParseDay(day, now)
	if err != nil {
		return nil, err
	}

	var counts map[string]int64
	if !store.IsLocalToday(target, now) {
		if row, ok, loadErr := rollup.LoadDailyStats(target.Format("2006-01-02")); loadErr != nil {
			return nil, loadErr
		} else if ok {
			counts = store.UnmarshalIntMap(row.SourceCallsJSON)
		}
	}
	if len(counts) == 0 {
		counts, err = loadCollectSourceCounts(target.Format("20060102"))
		if err != nil {
			return nil, err
		}
	}
	if len(counts) == 0 {
		return []types.SourceCallRow{}, nil
	}

	primary := strings.TrimSpace(lookupActiveSourceID())
	sourceMap := map[string]model.FilmSource{}
	sources, err := listCollectSources()
	if err != nil {
		return nil, err
	}
	for _, s := range sources {
		if id := strings.TrimSpace(s.Id); id != "" {
			sourceMap[id] = s
		}
	}

	rows := make([]types.SourceCallRow, 0, len(counts))
	for id, count := range counts {
		if count <= 0 || !ingest.ValidCollectSourceID(id) {
			continue
		}
		if s, ok := sourceMap[id]; ok {
			rows = append(rows, types.SourceCallRow{
				Id:        id,
				Name:      s.Name,
				Enabled:   s.State,
				IsPrimary: id == primary,
				Count:     count,
			})
		} else {
			rows = append(rows, types.SourceCallRow{
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
	raw, err := db.Rdb.HGetAll(db.Cxt, store.CollectSourceKey(dayKey)).Result()
	if err != nil && !errors.Is(err, redis.Nil) {
		return nil, err
	}
	for id, text := range raw {
		if !ingest.ValidCollectSourceID(id) {
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
