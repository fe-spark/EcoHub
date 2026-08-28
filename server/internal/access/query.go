package access

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"

	"github.com/redis/go-redis/v9"
)

type Overview struct {
	Day     string           `json:"day"`
	PV      int64            `json:"pv"`
	UV      int64            `json:"uv"`
	Err4    int64            `json:"err4"`
	Err5    int64            `json:"err5"`
	P95Ms   int64            `json:"p95Ms"`
	Dropped int64            `json:"dropped"`
	Provide ProvideStats     `json:"provide"`
	Client  map[string]int64 `json:"client"`
	Action  map[string]int64 `json:"action"`
	Hist    map[string]int64 `json:"hist"`
	Series  []SeriesPoint    `json:"series"`
}

type ProvideStats struct {
	PV   int64 `json:"pv"`
	Err4 int64 `json:"err4"`
	Err5 int64 `json:"err5"`
}

type SeriesPoint struct {
	T         string `json:"t"`
	PV        int64  `json:"pv"`
	Err4      int64  `json:"err4"`
	Err5      int64  `json:"err5"`
	ProvidePV int64  `json:"providePv"`
}

type TopItem struct {
	Key      string `json:"key"`
	Count    int64  `json:"count"`
	Title    string `json:"title,omitempty"`
	Category string `json:"category,omitempty"`
	Poster   string `json:"poster,omitempty"`
	Year     int64  `json:"year,omitempty"`
}

func QueryOverview(day string) (*Overview, error) {
	if db.Rdb == nil {
		return nil, fmt.Errorf("redis unavailable")
	}
	loc := time.Local
	now := time.Now().In(loc)
	target, err := parseDay(day, now)
	if err != nil {
		return nil, err
	}
	dayKey := target.Format("20060102")
	out := &Overview{
		Day:     target.Format("2006-01-02"),
		Client:  map[string]int64{},
		Action:  map[string]int64{},
		Hist:    map[string]int64{},
		Series:  []SeriesPoint{},
		Dropped: Dropped(),
	}

	ctx := db.Cxt
	pipe := db.Rdb.Pipeline()
	uvCmd := pipe.PFCount(ctx, uvKey(dayKey))
	dayCmd := pipe.HGetAll(ctx, dayAggKey(dayKey))
	clientCmd := pipe.HGetAll(ctx, clientKey(dayKey))
	actionCmd := pipe.HGetAll(ctx, actionKey(dayKey))
	histCmd := pipe.HGetAll(ctx, histKey(dayKey))
	droppedCmd := pipe.Get(ctx, droppedKey())

	type minSlot struct {
		t   time.Time
		cmd *redis.MapStringStringCmd
	}
	start := time.Date(target.Year(), target.Month(), target.Day(), 0, 0, 0, 0, loc)
	nMin := 0
	if now.Sub(start) < ttlMinute {
		nMin = 1440
		if start.Year() == now.Year() && start.YearDay() == now.YearDay() {
			nMin = now.Hour()*60 + now.Minute() + 1
			if nMin > 1440 {
				nMin = 1440
			}
		}
	}
	slots := make([]minSlot, 0, nMin)
	for i := 0; i < nMin; i++ {
		t := start.Add(time.Duration(i) * time.Minute)
		slots = append(slots, minSlot{t: t, cmd: pipe.HGetAll(ctx, minKey(t))})
	}
	if _, err := pipe.Exec(ctx); err != nil && err != redis.Nil {
		return nil, err
	}

	out.UV = uvCmd.Val()
	out.Client = parseIntMap(clientCmd.Val())
	out.Action = parseIntMap(actionCmd.Val())
	histVals := parseIntMap(histCmd.Val())
	out.Hist = histVals
	out.P95Ms = EstimateP95(histVals)
	if n, err := droppedCmd.Int64(); err == nil && n > out.Dropped {
		out.Dropped = n
	}

	dayVals := parseIntMap(dayCmd.Val())
	out.PV = dayVals["pv"]
	out.Err4 = dayVals["err4"]
	out.Err5 = dayVals["err5"]
	out.Provide.PV = dayVals["provide_pv"]
	out.Provide.Err4 = dayVals["provide_err4"]
	out.Provide.Err5 = dayVals["provide_err5"]

	var fold *SeriesPoint
	var minPV, minErr4, minErr5, minPPV, minPE4, minPE5 int64
	flush := func() {
		if fold != nil {
			out.Series = append(out.Series, *fold)
			fold = nil
		}
	}
	for _, slot := range slots {
		vals := parseIntMap(slot.cmd.Val())
		pv := vals["pv"]
		err4 := vals["err4"]
		err5 := vals["err5"]
		ppv := vals["provide_pv"]
		minPV += pv
		minErr4 += err4
		minErr5 += err5
		minPPV += ppv
		minPE4 += vals["provide_err4"]
		minPE5 += vals["provide_err5"]

		label := slot.t.Truncate(15 * time.Minute).Format(time.RFC3339)
		if fold == nil || fold.T != label {
			flush()
			fold = &SeriesPoint{T: label}
		}
		fold.PV += pv
		fold.Err4 += err4
		fold.Err5 += err5
		fold.ProvidePV += ppv
	}
	flush()
	if minPV > out.PV {
		out.PV = minPV
	}
	if minErr4 > out.Err4 {
		out.Err4 = minErr4
	}
	if minErr5 > out.Err5 {
		out.Err5 = minErr5
	}
	if minPPV > out.Provide.PV {
		out.Provide.PV = minPPV
	}
	if minPE4 > out.Provide.Err4 {
		out.Provide.Err4 = minPE4
	}
	if minPE5 > out.Provide.Err5 {
		out.Provide.Err5 = minPE5
	}
	return out, nil
}

func QueryTops(day, kind string, limit int) ([]TopItem, error) {
	if db.Rdb == nil {
		return nil, fmt.Errorf("redis unavailable")
	}
	if limit <= 0 {
		limit = 10
	}
	if limit > 50 {
		limit = 50
	}
	now := time.Now()
	target, err := parseDay(day, now)
	if err != nil {
		return nil, err
	}
	dayKey := target.Format("20060102")
	var key string
	switch kind {
	case "search":
		key = topSearchKey(dayKey)
	case "play":
		key = topPlayKey(dayKey)
	default:
		key = topPathKey(dayKey)
	}
	pairs, err := db.Rdb.ZRevRangeWithScores(db.Cxt, key, 0, int64(limit-1)).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	items := make([]TopItem, 0, len(pairs))
	for _, p := range pairs {
		member, _ := p.Member.(string)
		items = append(items, TopItem{Key: member, Count: int64(p.Score)})
	}
	if kind == "play" {
		items = enrichPlayTopItems(items)
	}
	return items, nil
}

func QueryLogs(source, status, client, q string, limit int) ([]AccessEvent, error) {
	if db.Rdb == nil {
		return nil, fmt.Errorf("redis unavailable")
	}
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
	key := recentKey()
	fetch := int64(config.AccessRecentLimit)
	switch source {
	case "slow":
		key = slowKey()
		fetch = slowKeep
	case "error":
		key = errorKey()
		fetch = errorKeep
	}
	raw, err := db.Rdb.LRange(db.Cxt, key, 0, fetch-1).Result()
	if err != nil && err != redis.Nil {
		return nil, err
	}
	q = strings.ToLower(strings.TrimSpace(q))
	client = strings.TrimSpace(client)
	out := make([]AccessEvent, 0, limit)
	for _, line := range raw {
		var evt AccessEvent
		if json.Unmarshal([]byte(line), &evt) != nil {
			continue
		}
		if !matchStatus(status, evt.Status) {
			continue
		}
		if client != "" && evt.ClientType != client {
			continue
		}
		if q != "" && !strings.Contains(strings.ToLower(evt.Path), q) {
			continue
		}
		out = append(out, evt)
		if len(out) >= limit {
			break
		}
	}
	return out, nil
}

func matchStatus(filter string, status int) bool {
	switch filter {
	case "2xx":
		return status >= 200 && status < 300
	case "4xx":
		return status >= 400 && status < 500
	case "5xx":
		return status >= 500 && status < 600
	default:
		return true
	}
}

func parseDay(day string, now time.Time) (time.Time, error) {
	day = strings.TrimSpace(day)
	if day == "" {
		return time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, now.Location()), nil
	}
	t, err := time.ParseInLocation("2006-01-02", day, now.Location())
	if err != nil {
		return time.Time{}, fmt.Errorf("invalid day")
	}
	return t, nil
}

func parseIntMap(m map[string]string) map[string]int64 {
	out := make(map[string]int64, len(m))
	for k, v := range m {
		n, _ := strconv.ParseInt(v, 10, 64)
		out[k] = n
	}
	return out
}

func EstimateP95(hist map[string]int64) int64 {
	order := []struct {
		key   string
		upper int64
	}{
		{"bInf", 1000},
		{"b1000", 1000},
		{"b500", 500},
		{"b200", 200},
		{"b100", 100},
		{"b50", 50},
	}
	var total int64
	for _, b := range order {
		total += hist[b.key]
	}
	if total <= 0 {
		return 0
	}
	need := (total*5 + 99) / 100
	if need < 1 {
		need = 1
	}
	var acc int64
	for _, b := range order {
		acc += hist[b.key]
		if acc >= need {
			return b.upper
		}
	}
	return 50
}
