package access

import (
	"encoding/json"
	"sort"
	"strings"
	"sync"
)

const (
	// maxBoundedHashFields 单日 app_version / device_model Redis Hash 最多保留的不同键值数，超限折叠到 "other"。
	maxBoundedHashFields  = 256
	maxBoundedHashDayKeys = 32
)

var (
	hashFieldGateMu sync.Mutex
	hashFieldGate   = map[string]map[string]struct{}{}
)

func admitBoundedHashField(redisKey, field string) string {
	field = strings.TrimSpace(field)
	if field == "" {
		return ""
	}
	hashFieldGateMu.Lock()
	defer hashFieldGateMu.Unlock()
	seen := hashFieldGate[redisKey]
	if seen == nil {
		if len(hashFieldGate) >= maxBoundedHashDayKeys {
			hashFieldGate = map[string]map[string]struct{}{}
		}
		seen = make(map[string]struct{}, 32)
		hashFieldGate[redisKey] = seen
	}
	if _, ok := seen[field]; ok {
		return field
	}
	if len(seen) >= maxBoundedHashFields {
		return "other"
	}
	seen[field] = struct{}{}
	return field
}

// compactTopIntMap 截断并保留计数值最高的 topK 项，其余合并至 "other"，同时确保 JSON 不超过 MySQL TEXT 上限。
func compactTopIntMap(src map[string]int64, topK, maxBytes int) map[string]int64 {
	if len(src) == 0 {
		return map[string]int64{}
	}
	type kv struct {
		k string
		v int64
	}
	items := make([]kv, 0, len(src))
	var otherTotal int64
	for k, v := range src {
		if k == "other" {
			otherTotal += v
			continue
		}
		items = append(items, kv{k: k, v: v})
	}
	sort.Slice(items, func(i, j int) bool {
		if items[i].v != items[j].v {
			return items[i].v > items[j].v
		}
		return items[i].k < items[j].k
	})
	limit := topK
	if limit <= 0 || limit > len(items) {
		limit = len(items)
	}
	for i := limit; i < len(items); i++ {
		otherTotal += items[i].v
	}
	items = items[:limit]

	build := func(n int, extraOther int64) map[string]int64 {
		out := make(map[string]int64, n+1)
		for i := 0; i < n; i++ {
			out[items[i].k] = items[i].v
		}
		if extraOther > 0 {
			out["other"] += extraOther
		}
		return out
	}
	n := len(items)
	out := build(n, otherTotal)
	for maxBytes > 0 && n > 0 {
		raw, err := json.Marshal(out)
		if err == nil && len(raw) <= maxBytes {
			break
		}
		n--
		otherTotal += items[n].v
		out = build(n, otherTotal)
	}
	return out
}
