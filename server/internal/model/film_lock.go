package model

import "strings"

const (
	LockPoster   = "poster"
	LockBackdrop = "backdrop"
	LockOverview = "overview"
	LockSubtitle = "subtitle"
	LockActor    = "actor"
	LockDirector = "director"
	LockYear     = "year"
	LockScore    = "score"
	LockTag      = "tag"
)

var lockOrder = []string{
	LockPoster, LockBackdrop, LockOverview, LockSubtitle,
	LockActor, LockDirector, LockYear, LockScore, LockTag,
}

// ParseLockedFields 解析刮削已经写入、再采集不覆盖的列。
func ParseLockedFields(raw string) map[string]struct{} {
	set := make(map[string]struct{})
	for _, part := range strings.Split(raw, ",") {
		part = strings.ToLower(strings.TrimSpace(part))
		if part != "" {
			set[part] = struct{}{}
		}
	}
	return set
}

// FormatLockedFields 按固定顺序写回已写入的列。
func FormatLockedFields(set map[string]struct{}) string {
	if len(set) == 0 {
		return ""
	}
	parts := make([]string, 0, len(set))
	for _, key := range lockOrder {
		if _, ok := set[key]; ok {
			parts = append(parts, key)
		}
	}
	return strings.Join(parts, ",")
}

// MergeLocks 追加再采集不覆盖的列。
func MergeLocks(raw string, keys ...string) string {
	set := ParseLockedFields(raw)
	for _, key := range keys {
		key = strings.ToLower(strings.TrimSpace(key))
		if key != "" {
			set[key] = struct{}{}
		}
	}
	return FormatLockedFields(set)
}

// DropLocks 去掉不再跳过的列。
func DropLocks(raw string, keys ...string) string {
	set := ParseLockedFields(raw)
	for _, key := range keys {
		delete(set, strings.ToLower(strings.TrimSpace(key)))
	}
	return FormatLockedFields(set)
}

// HasLock 该列是否已由刮削写入。
func HasLock(raw, key string) bool {
	_, ok := ParseLockedFields(raw)[strings.ToLower(strings.TrimSpace(key))]
	return ok
}

// LockedColumns 再采集不覆盖的 film_index 列。
func LockedColumns(raw string) map[string]struct{} {
	set := ParseLockedFields(raw)
	out := make(map[string]struct{})
	add := func(cols ...string) {
		for _, col := range cols {
			out[col] = struct{}{}
		}
	}
	if _, ok := set[LockPoster]; ok {
		add("picture", "picture_slide")
	}
	if _, ok := set[LockBackdrop]; ok {
		add("picture", "picture_slide")
	}
	if _, ok := set[LockOverview]; ok {
		add("blurb", "content")
	}
	if _, ok := set[LockSubtitle]; ok {
		add("sub_title")
	}
	if _, ok := set[LockActor]; ok {
		add("actor")
	}
	if _, ok := set[LockDirector]; ok {
		add("director")
	}
	if _, ok := set[LockYear]; ok {
		add("year", "release_date")
	}
	if _, ok := set[LockScore]; ok {
		add("score")
	}
	if _, ok := set[LockTag]; ok {
		add("class_tag")
	}
	return out
}
