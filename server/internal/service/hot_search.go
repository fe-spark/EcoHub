package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"server/internal/config"
	"server/internal/infra/db"
	filmsnapshot "server/internal/repository/film/snapshot"
)

const (
	hotSearchDefaultLimit = 8
	hotSearchMaxLimit     = 20
)

var hotKeywordsSfGroup singleflight.Group

func clampHotSearchLimit(limit int) int {
	if limit <= 0 {
		return hotSearchDefaultLimit
	}
	if limit > hotSearchMaxLimit {
		return hotSearchMaxLimit
	}
	return limit
}

func sliceHotKeywords(keywords []string, limit int) []string {
	if len(keywords) == 0 {
		return []string{}
	}
	if len(keywords) > limit {
		res := make([]string, limit)
		copy(res, keywords[:limit])
		return res
	}
	res := make([]string, len(keywords))
	copy(res, keywords)
	return res
}

// GetHotSearchKeywords 全站热门推荐：按当前首选站播放热度取片名，与个人搜索历史无关。
func (i *IndexService) GetHotSearchKeywords(limit int, sourceIdOpt ...string) ([]string, error) {
	limit = clampHotSearchLimit(limit)

	var sourceID string
	if len(sourceIdOpt) > 0 {
		sourceID = strings.TrimSpace(sourceIdOpt[0])
	}
	if sourceID == "" {
		sourceID = i.BaselineSourceID()
	}

	version := filmsnapshot.GetActiveReadModelVersion()
	if version == "" {
		version = filmsnapshot.GetActiveSnapshotVersion()
	}
	if version == "" {
		return nil, filmsnapshot.ErrSearchIndexMissing
	}

	var cacheKey string
	if sourceID != "" {
		cacheKey = fmt.Sprintf("%s:v%s:src_%s", config.FilmHotKeywordsKey, version, sourceID)
	} else {
		cacheKey = fmt.Sprintf("%s:v%s", config.FilmHotKeywordsKey, version)
	}
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []string
			if json.Unmarshal([]byte(data), &cached) == nil {
				return sliceHotKeywords(cached, limit), nil
			}
		}
	}

	sfKey := cacheKey
	val, err, _ := hotKeywordsSfGroup.Do(sfKey, func() (any, error) {
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				var cached []string
				if json.Unmarshal([]byte(data), &cached) == nil {
					return cached, nil
				}
			}
		}

		names, err := filmsnapshot.TopSourceFilmNames(sourceID, hotSearchMaxLimit*3)
		if err != nil {
			return nil, err
		}

		topKeywords := make([]string, 0, hotSearchMaxLimit)
		seen := make(map[string]struct{}, hotSearchMaxLimit)
		for _, rawName := range names {
			name := strings.TrimSpace(rawName)
			if name == "" {
				continue
			}
			if _, ok := seen[name]; ok {
				continue
			}
			seen[name] = struct{}{}
			topKeywords = append(topKeywords, name)
			if len(topKeywords) >= hotSearchMaxLimit {
				break
			}
		}

		if db.Rdb != nil {
			ttl := 30 * time.Minute
			if len(topKeywords) == 0 {
				ttl = 60 * time.Second
			}
			if raw, err := json.Marshal(topKeywords); err == nil {
				_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), ttl).Err()
			}
		}
		return topKeywords, nil
	})

	if err != nil {
		return nil, err
	}
	if val == nil {
		return []string{}, nil
	}
	keywords, ok := val.([]string)
	if !ok {
		return nil, errors.New("热词结果无效")
	}
	return sliceHotKeywords(keywords, limit), nil
}
