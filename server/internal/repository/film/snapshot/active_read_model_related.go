package snapshot

import (
	"encoding/json"
	"fmt"
	"golang.org/x/sync/singleflight"
	"log"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/film/shared"

	"gorm.io/gorm"
)

const (
	relatedCacheTTL             = 1 * time.Hour
	maxRelatedRecommendCount    = 28 // 相关推荐全局最大保留数量
	relatedSnapshotSelectFields = "mid, pid, cid, root_category_key, category_key, c_name, name, sub_title, series_key, director, actor, score, hits, update_stamp, remarks, state, picture, picture_slide, custom_picture, custom_picture_slide, is_custom_picture, blurb, year, class_tag, area, language, play_from_summary"
)

var relatedSnapshotsSf singleflight.Group

func pageSnapshots(snapshots []model.FilmListSnapshot, page *dto.Page) []model.FilmListSnapshot {
	offset := shared.PageOffset(page)
	if offset >= len(snapshots) {
		return []model.FilmListSnapshot{}
	}
	end := offset + page.PageSize
	if end > len(snapshots) {
		end = len(snapshots)
	}
	return snapshots[offset:end]
}

func ListRelatedSnapshotsReadModel(version string, snapshot model.FilmListSnapshot, page *dto.Page, sourceIdOpt ...string) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = shared.EnsurePage(page)
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" || snapshot.Mid <= 0 {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}
	}

	var sourceID string
	if len(sourceIdOpt) > 0 {
		sourceID = strings.TrimSpace(sourceIdOpt[0])
	}

	var cacheKey string
	if sourceID != "" {
		cacheKey = fmt.Sprintf("%s:v%s:src_%s:%d", config.FilmRelateCandCachePrefix, version, sourceID, snapshot.Mid)
	} else {
		cacheKey = fmt.Sprintf("%s:v%s:%d", config.FilmRelateCandCachePrefix, version, snapshot.Mid)
	}
	var candidates []model.FilmListSnapshot
	hitCache := false

	// 1. 尝试从 Redis 缓存获取候选集
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			if json.Unmarshal([]byte(data), &candidates) == nil {
				hitCache = true
				log.Printf("[FilmRelate] 相关推荐候选集命中缓存 mid=%d name=%q source=%q cache=HIT candidates=%d cost=%s",
					snapshot.Mid, snapshot.Name, sourceID, len(candidates), time.Since(startedAt))
			}
		}
	}

	// 2. 并发防击穿：同 mid 详情页并发打开时合并计算候选集
	if !hitCache {
		var sfKey string
		if sourceID != "" {
			sfKey = fmt.Sprintf("v%s:src_%s:%d", version, sourceID, snapshot.Mid)
		} else {
			sfKey = fmt.Sprintf("v%s:%d", version, snapshot.Mid)
		}
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

			listGen := GetSearchCacheVersion()
			cands := loadRelatedSnapshotCandidates(version, snapshot, maxRelatedRecommendCount, sourceID)
			ttl := relatedCacheTTL
			if len(cands) == 0 {
				ttl = emptyListCacheTTL
			}
			if raw, err := json.Marshal(cands); err == nil {
				writeListCache(cacheKey, raw, ttl, listGen)
			}

			log.Printf("[FilmRelate] 相关推荐候选集计算完成 mid=%d name=%q source=%q cache=MISS candidates=%d cost=%s",
				snapshot.Mid, snapshot.Name, sourceID, len(cands), time.Since(startedAt))

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

const candidatePoolLimit = 60

func loadRelatedSnapshotCandidates(version string, current model.FilmListSnapshot, maxCandidates int, sourceID string) []model.FilmListSnapshot {
	seen := make(map[int64]struct{}, candidatePoolLimit+1)
	seen[current.Mid] = struct{}{}
	list := make([]model.FilmListSnapshot, 0, candidatePoolLimit)

	appendUnique := func(src []model.FilmListSnapshot) {
		for _, item := range src {
			if _, ok := seen[item.Mid]; ok {
				continue
			}
			seen[item.Mid] = struct{}{}
			list = append(list, item)
			if len(list) >= candidatePoolLimit {
				break
			}
		}
	}

	baseQuery := func() *gorm.DB {
		q := liveFilmQuery().Select(relatedSnapshotSelectFields)
		if sourceID != "" {
			q = applySourceMembership(q, sourceID)
		}
		return q
	}

	// 1. 同系列极速召回（命中 series_key 索引）
	if current.SeriesKey != "" && db.Mdb != nil {
		seriesRows, _ := scanListSnapshots(baseQuery().
			Where("series_key = ? AND mid <> ?", current.SeriesKey, current.Mid).
			Order("hits DESC, mid DESC").Limit(candidatePoolLimit))
		appendUnique(seriesRows)
	}

	// 2. 同核心片名/同IP系列极速召回（命中 idx_film_index_name 索引，召回同IP剧场版、年番、衍生番）
	coreToken := extractCoreSearchToken(current.Name)
	if len(list) < candidatePoolLimit && len([]rune(coreToken)) >= 2 && db.Mdb != nil {
		titleRows, _ := scanListSnapshots(baseQuery().
			Where("name LIKE ? AND mid <> ?", coreToken+"%", current.Mid).
			Order("hits DESC, mid DESC").Limit(candidatePoolLimit - len(list)))
		appendUnique(titleRows)
	}

	// 3. 同细分类极速召回（命中 category_key 或 cid 复合索引）
	if len(list) < candidatePoolLimit && db.Mdb != nil {
		catKey := strings.TrimSpace(current.CategoryKey)
		if catKey != "" {
			catRows, _ := scanListSnapshots(baseQuery().
				Where("category_key = ? AND mid <> ?", catKey, current.Mid).
				Order("hits DESC, mid DESC").Limit(candidatePoolLimit - len(list)))
			appendUnique(catRows)
		} else if current.Cid > 0 {
			cidRows, _ := scanListSnapshots(baseQuery().
				Where("cid = ? AND mid <> ?", current.Cid, current.Mid).
				Order("hits DESC, mid DESC").Limit(candidatePoolLimit - len(list)))
			appendUnique(cidRows)
		}
	}

	// 4. 同大分类热播极速召回（命中 root_category_key 或 pid 复合索引）
	rootKey, pid := resolveCurrentRootCategory(current)
	if len(list) < candidatePoolLimit && db.Mdb != nil {
		if rootKey != "" {
			rootRows, _ := scanListSnapshots(baseQuery().
				Where("root_category_key = ? AND mid <> ?", rootKey, current.Mid).
				Order("hits DESC, mid DESC").Limit(candidatePoolLimit - len(list)))
			appendUnique(rootRows)
		}
		if len(list) < candidatePoolLimit && pid > 0 {
			pidRows, _ := scanListSnapshots(baseQuery().
				Where("pid = ? AND mid <> ?", pid, current.Mid).
				Order("hits DESC, mid DESC").Limit(candidatePoolLimit - len(list)))
			appendUnique(pidRows)
		}
	}

	// 5. 首选站热播兜底（仅在影片自身无任何分类归属时作为孤立片保底，有大分类时绝对禁止跨类污染）
	if len(list) < candidatePoolLimit && rootKey == "" && pid <= 0 && db.Mdb != nil {
		fallbackRows, _ := scanListSnapshots(baseQuery().
			Where("mid <> ?", current.Mid).
			Order("hits DESC, mid DESC").Limit(candidatePoolLimit - len(list)))
		appendUnique(fallbackRows)
	}

	// 6. 全库热播兜底（仅在未指定首选站且影片无分类归属时）
	if len(list) < candidatePoolLimit && sourceID == "" && rootKey == "" && pid <= 0 && db.Mdb != nil {
		globalRows, _ := scanListSnapshots(liveFilmQuery().
			Select(relatedSnapshotSelectFields).
			Where("mid <> ?", current.Mid).
			Order("hits DESC, mid DESC").Limit(candidatePoolLimit - len(list)))
		appendUnique(globalRows)
	}

	// 7. 内存多维特征打分与重排（微秒级 CPU 计算，彻底避免 SQL 字符串与全表扫描）
	return rankRelatedCandidates(current, list, maxCandidates)
}
