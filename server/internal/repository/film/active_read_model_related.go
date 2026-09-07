package film

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"

	"golang.org/x/sync/singleflight"
)

const (
	relatedCacheTTL             = 1 * time.Hour
	maxRelatedRecommendCount    = 28 // 相关推荐全局最大保留数量
	relatedSnapshotSelectFields = "id, snapshot_version, mid, pid, cid, c_name, name, sub_title, series_key, director, actor, score, hits, update_stamp, remarks, state, picture, picture_slide, custom_picture, custom_picture_slide, is_custom_picture, blurb, year, class_tag, area, language, play_from_summary"
)

var relatedSnapshotsSf singleflight.Group

func pageSnapshots(snapshots []model.FilmListSnapshot, page *dto.Page) []model.FilmListSnapshot {
	offset := getPageOffset(page)
	if offset >= len(snapshots) {
		return []model.FilmListSnapshot{}
	}
	end := offset + page.PageSize
	if end > len(snapshots) {
		end = len(snapshots)
	}
	return snapshots[offset:end]
}

func ListRelatedSnapshotsReadModel(version string, snapshot model.FilmListSnapshot, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = ensurePage(page)
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" || snapshot.Mid <= 0 {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}
	}

	cacheKey := fmt.Sprintf("EcoHub:relate:cand:v%s:%d", version, snapshot.Mid)
	var candidates []model.FilmListSnapshot
	hitCache := false

	// 1. 尝试从 Redis 缓存获取候选集
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			if json.Unmarshal([]byte(data), &candidates) == nil {
				hitCache = true
				log.Printf("[FilmRelate] 相关推荐候选集命中缓存 mid=%d name=%q cache=HIT candidates=%d cost=%s",
					snapshot.Mid, snapshot.Name, len(candidates), time.Since(startedAt))
			}
		}
	}

	// 2. 并发防击穿：同 mid 详情页并发打开时合并计算候选集
	if !hitCache {
		sfKey := fmt.Sprintf("v%s:%d", version, snapshot.Mid)
		val, err, _ := relatedSnapshotsSf.Do(sfKey, func() (any, error) {
			// 二次双检 Redis 缓存
			if db.Rdb != nil {
				if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
					var cached []model.FilmListSnapshot
					if json.Unmarshal([]byte(data), &cached) == nil {
						return cached, nil
					}
				}
			}

			cands := loadRelatedSnapshotCandidates(version, snapshot, maxRelatedRecommendCount)
			if db.Rdb != nil {
				ttl := relatedCacheTTL
				if len(cands) == 0 {
					ttl = 1 * time.Minute // 空候选集防穿透短缓存
				}
				if raw, err := json.Marshal(cands); err == nil {
					_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), ttl).Err()
				}
			}

			log.Printf("[FilmRelate] 相关推荐候选集计算完成 mid=%d name=%q cache=MISS candidates=%d cost=%s",
				snapshot.Mid, snapshot.Name, len(cands), time.Since(startedAt))

			return cands, nil
		})

		if err == nil && val != nil {
			if cands, ok := val.([]model.FilmListSnapshot); ok {
				candidates = cands
			}
		}
	}

	curTotal := len(candidates)
	curPageCount := (curTotal + page.PageSize - 1) / page.PageSize
	if curPageCount <= 0 {
		curPageCount = 1
	}
	page.Total = curTotal
	page.PageCount = curPageCount

	result := pageSnapshots(candidates, page)
	return cloneFilmListSnapshots(result)
}

func loadRelatedSnapshotCandidates(version string, current model.FilmListSnapshot, maxCandidates int) []model.FilmListSnapshot {
	seen := make(map[int64]struct{}, maxCandidates+1)
	seen[current.Mid] = struct{}{}
	list := make([]model.FilmListSnapshot, 0, maxCandidates)

	appendUnique := func(src []model.FilmListSnapshot) {
		for _, item := range src {
			if _, ok := seen[item.Mid]; ok {
				continue
			}
			seen[item.Mid] = struct{}{}
			list = append(list, item)
			if len(list) >= maxCandidates {
				break
			}
		}
	}

	// 1. 同系列优先（精确匹配，应用层 seen 去重）
	if current.SeriesKey != "" && db.Mdb != nil {
		var seriesRows []model.FilmListSnapshot
		db.Mdb.Select(relatedSnapshotSelectFields).
			Where("snapshot_version = ? AND series_key = ? AND mid <> ?", version, current.SeriesKey, current.Mid).
			Order("hits DESC, id DESC").Limit(maxCandidates).Find(&seriesRows)
		appendUnique(seriesRows)
	}

	// 2. 同细分类 (Cid) 候选兜底
	if len(list) < maxCandidates && current.Cid > 0 && db.Mdb != nil {
		var cidRows []model.FilmListSnapshot
		db.Mdb.Select(relatedSnapshotSelectFields).
			Where("snapshot_version = ? AND cid = ? AND mid <> ?", version, current.Cid, current.Mid).
			Order("hits DESC, id DESC").Limit(maxCandidates - len(list)).Find(&cidRows)
		appendUnique(cidRows)
	}

	// 3. 同大分类 (Pid) 高热度候选兜底
	if len(list) < maxCandidates && current.Pid > 0 && db.Mdb != nil {
		var pidRows []model.FilmListSnapshot
		db.Mdb.Select(relatedSnapshotSelectFields).
			Where("snapshot_version = ? AND pid = ? AND mid <> ?", version, current.Pid, current.Mid).
			Order("hits DESC, id DESC").Limit(maxCandidates - len(list)).Find(&pidRows)
		appendUnique(pidRows)
	}

	return list
}
