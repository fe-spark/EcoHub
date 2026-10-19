package service

import (
	"regexp"
	"strconv"
	"strings"
	"time"

	"server/internal/model"
)

var isoDateRegex = regexp.MustCompile(`\d{4}-\d{2}-\d{2}`)

// filmScrapeUpdates 把选中的刮削字段写入影片。空字段列表表示除片名、分类外全部写入。写过的列再采集不覆盖。
func filmScrapeUpdates(existing model.FilmIndex, data model.TMDBDetail, fields []string) map[string]any {
	enabled := func(names ...string) bool {
		return scrapeFieldsOn(fields, names...)
	}
	updates := map[string]any{}
	locks := model.ParseLockedFields(existing.LockedFields)
	add := func(key string) {
		locks[key] = struct{}{}
	}

	if enabled("poster", "picture") {
		if poster := strings.TrimSpace(data.Poster); poster != "" {
			updates["custom_picture"] = poster
			updates["is_custom_picture"] = true
			add(model.LockPoster)
		}
	}
	if enabled("backdrop", "pictureslide") {
		if slide := strings.TrimSpace(data.Backdrop); slide != "" {
			updates["custom_picture_slide"] = slide
			updates["is_custom_picture"] = true
			add(model.LockBackdrop)
		}
	}
	if enabled("overview", "content") {
		if text := strings.TrimSpace(data.Overview); text != "" {
			updates["blurb"] = text
			updates["content"] = text
			add(model.LockOverview)
		}
	}
	if enabled("subtitle", "originaltitle") {
		if text := strings.TrimSpace(data.OriginalTitle); text != "" {
			updates["sub_title"] = text
			add(model.LockSubtitle)
		}
	}
	if enabled("actor", "credits") {
		if text := joinSlash(data.Actors); text != "" {
			updates["actor"] = text
			add(model.LockActor)
		}
	}
	if enabled("director", "credits") {
		if text := joinSlash(data.Directors); text != "" {
			updates["director"] = text
			add(model.LockDirector)
		}
	}
	if enabled("year", "releasedate") {
		year, release := scrapeYear(data)
		if year > 0 {
			updates["year"] = year
			add(model.LockYear)
		}
		if release != "" {
			updates["release_date"] = release
			add(model.LockYear)
		}
	}
	if enabled("score", "voteaverage") {
		if score := scrapeScore(data); score > 0 {
			updates["score"] = score
			add(model.LockScore)
		}
	}
	if enabled("tag", "genres") {
		if text := strings.Join(data.Genres, ","); strings.Trim(text, ",") != "" {
			updates["class_tag"] = text
			add(model.LockTag)
		}
	}

	updates["locked_fields"] = model.FormatLockedFields(locks)
	updates["updated_at"] = time.Now()
	return updates
}

func scrapeFieldsOn(fields []string, names ...string) bool {
	if len(fields) == 0 {
		return true
	}
	set := make(map[string]struct{}, len(fields))
	for _, field := range fields {
		field = strings.ToLower(strings.TrimSpace(field))
		if field != "" {
			set[field] = struct{}{}
		}
	}
	for _, name := range names {
		if _, ok := set[strings.ToLower(name)]; ok {
			return true
		}
	}
	return false
}

func joinSlash(items []string) string {
	parts := make([]string, 0, len(items))
	for _, item := range items {
		item = strings.TrimSpace(item)
		if item != "" {
			parts = append(parts, item)
		}
	}
	return strings.Join(parts, "/")
}

func scrapeYear(data model.TMDBDetail) (int64, string) {
	var year int64
	if y, err := strconv.ParseInt(strings.TrimSpace(data.Year), 10, 64); err == nil && y > 0 {
		year = y
	}
	release := strings.TrimSpace(data.ReleaseDate)
	if match := isoDateRegex.FindString(release); match != "" {
		release = match
		if year <= 0 {
			if y, err := strconv.ParseInt(match[:4], 10, 64); err == nil && y > 0 {
				year = y
			}
		}
	}
	return year, release
}

func scrapeScore(data model.TMDBDetail) float64 {
	if data.VoteAverage > 0 {
		return data.VoteAverage
	}
	score, err := strconv.ParseFloat(strings.TrimSpace(data.VoteScore), 64)
	if err != nil || score <= 0 {
		return 0
	}
	return score
}
