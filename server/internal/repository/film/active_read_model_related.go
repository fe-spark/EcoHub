package film

import (
	"encoding/json"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"

	"golang.org/x/sync/singleflight"
)

const (
	relatedSnapshotMinScore    = 24
	relatedCacheTTL            = 1 * time.Hour
	maxRelatedRecommendCount   = 28 // 相关推荐全局最大保留数量（前端展示通常为 14 条）
	relatedFunnelMinCandidates = 35 // 候选集召回最低打底阈值
	relatedSnapshotSelectFields = "id, snapshot_version, mid, pid, cid, c_name, name, sub_title, series_key, director, actor, score, hits, update_stamp, remarks, state, picture, picture_slide, custom_picture, custom_picture_slide, is_custom_picture, blurb, year, class_tag, area, language, play_from_summary"
)

type relatedCacheItem struct {
	Total     int                     `json:"total"`
	PageCount int                     `json:"page_count"`
	Snapshots []model.FilmListSnapshot `json:"snapshots"`
}

var relatedSnapshotsSf singleflight.Group

func ListRelatedSnapshotsReadModel(version string, snapshot model.FilmListSnapshot, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = ensurePage(page)
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" || snapshot.Mid <= 0 {
		return []model.FilmListSnapshot{}
	}

	// 1. 尝试从 Redis 缓存获取相关推荐
	cacheKey := fmt.Sprintf("EcoHub:relate:v%s:%d:p%d:s%d", version, snapshot.Mid, page.Current, page.PageSize)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var item relatedCacheItem
			if json.Unmarshal([]byte(data), &item) == nil {
				page.Total = item.Total
				page.PageCount = item.PageCount
				log.Printf("[FilmRelate] 相关推荐命中缓存 mid=%d name=%q cache=HIT total=%d page=%d size=%d cost=%s",
					snapshot.Mid, snapshot.Name, item.Total, page.Current, len(item.Snapshots), time.Since(startedAt))
				return item.Snapshots
			}
		}
	}

	// 2. 并发防击穿：同 mid 详情页并发打开时合并计算
	sfKey := fmt.Sprintf("v%s:%d:p%d:s%d", version, snapshot.Mid, page.Current, page.PageSize)
	val, err, _ := relatedSnapshotsSf.Do(sfKey, func() (any, error) {
		// 二次双检 Redis 缓存
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				var item relatedCacheItem
				if json.Unmarshal([]byte(data), &item) == nil {
					return item, nil
				}
			}
		}

		// 漏斗召回最相关的候选集（最多 50 个候选）
		candidates := loadRelatedSnapshotCandidates(version, snapshot, 50)
		context := buildRelatedSnapshotContext(snapshot)

		scoredList := make([]relatedSnapshotScore, 0, len(candidates))
		for _, candidate := range candidates {
			if candidate.Mid == snapshot.Mid {
				continue
			}
			score := scoreRelatedSnapshot(context, candidate)
			if score < relatedSnapshotMinScore {
				continue
			}
			scoredList = append(scoredList, relatedSnapshotScore{snapshot: candidate, score: score})
		}
		sortRelatedSnapshots(scoredList)
		snapshots := relatedScoresToSnapshots(scoredList)

		// 限制最高相关度结果数量
		if len(snapshots) > maxRelatedRecommendCount {
			snapshots = snapshots[:maxRelatedRecommendCount]
		}

		// 3. 兜底补齐（若相关推荐不足 maxRelatedRecommendCount 条，补齐到 maxRelatedRecommendCount）
		if len(snapshots) < maxRelatedRecommendCount {
			snapshots = appendCategoryFallbacks(version, snapshot, snapshots, maxRelatedRecommendCount)
		}

		curTotal := len(snapshots)
		curPageCount := (curTotal + page.PageSize - 1) / page.PageSize
		if curPageCount <= 0 {
			curPageCount = 1
		}

		result := pageSnapshots(snapshots, page)

		// 4. 写入 Redis 缓存
		if db.Rdb != nil && len(result) > 0 {
			cachePayload := relatedCacheItem{
				Total:     curTotal,
				PageCount: curPageCount,
				Snapshots: result,
			}
			if raw, err := json.Marshal(cachePayload); err == nil {
				_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), relatedCacheTTL).Err()
			}
		}

		log.Printf("[FilmRelate] 相关推荐计算完成 mid=%d name=%q cache=MISS candidates=%d scored=%d total=%d page=%d size=%d cost=%s",
			snapshot.Mid, snapshot.Name, len(candidates), len(scoredList), curTotal, page.Current, len(result), time.Since(startedAt))

		return relatedCacheItem{
			Total:     curTotal,
			PageCount: curPageCount,
			Snapshots: result,
		}, nil
	})

	if err != nil || val == nil {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}
	}
	cachedItem, ok := val.(relatedCacheItem)
	if !ok {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}
	}
	page.Total = cachedItem.Total
	page.PageCount = cachedItem.PageCount
	return cloneFilmListSnapshots(cachedItem.Snapshots)
}

func loadRelatedSnapshotCandidates(version string, current model.FilmListSnapshot, maxCandidates int) []model.FilmListSnapshot {
	seen := make(map[int64]struct{}, maxCandidates)
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

	// 候选 1：同系列优先（精确匹配，应用层 seen 去重）
	if current.SeriesKey != "" {
		var seriesRows []model.FilmListSnapshot
		db.Mdb.Select(relatedSnapshotSelectFields).Where("snapshot_version = ? AND series_key = ?", version, current.SeriesKey).
			Order("update_stamp DESC, id DESC").Limit(15).Find(&seriesRows)
		appendUnique(seriesRows)
	}

	// 候选 2：核心片名匹配（优先内存倒排索引纳秒级快速召回）
	coreToken := extractCoreSearchToken(current.Name)
	if coreToken != "" && len(list) < maxCandidates {
		idx := getOrLoadFilmSearchMemoryIndex(version)
		if idx != nil && len(idx.Items) > 0 {
			var matchedMids []int64
			token := utils.CleanCompactText(coreToken)
			if token == "" {
				token = strings.ToLower(strings.TrimSpace(coreToken))
			}
			candidateIndexes := idx.tokenNameCandidates(token)
			for _, itemIdx := range candidateIndexes {
				if int(itemIdx) >= len(idx.Items) {
					continue
				}
				item := &idx.Items[itemIdx]
				if item.Mid == current.Mid {
					continue
				}
				if _, ok := seen[item.Mid]; ok {
					continue
				}
				matchedMids = append(matchedMids, item.Mid)
				if len(matchedMids) >= 20 {
					break
				}
			}
			if len(matchedMids) > 0 {
				titleRows := GetProjectedSnapshotsByMidsOrdered(version, matchedMids)
				appendUnique(titleRows)
			}
		} else {
			var titleRows []model.FilmListSnapshot
			like := "%" + escapeLikePattern(coreToken) + "%"
			q := db.Mdb.Select(relatedSnapshotSelectFields).Where("snapshot_version = ? AND mid <> ? AND name LIKE ?", version, current.Mid, like)
			if current.Pid > 0 {
				q = q.Where("pid = ?", current.Pid)
			}
			q.Order("update_stamp DESC, id DESC").Limit(20).Find(&titleRows)
			appendUnique(titleRows)
		}
	}

	// 候选 3：同细分类 (Cid) 候选（走 idx_snap_cid_update 复合索引直出）
	funnelThreshold := min(maxCandidates, relatedFunnelMinCandidates)
	if current.Cid > 0 && len(list) < funnelThreshold {
		var cidRows []model.FilmListSnapshot
		db.Mdb.Select(relatedSnapshotSelectFields).Where("snapshot_version = ? AND cid = ?", version, current.Cid).
			Order("update_stamp DESC, id DESC").Limit(20).Find(&cidRows)
		appendUnique(cidRows)
	}

	// 候选 4：同大分类高热度候选（走 idx_snap_pid_hits 复合索引直出，杜绝 class_tag LIKE 长文本全表扫描）
	if current.Pid > 0 && len(list) < funnelThreshold {
		var hotRows []model.FilmListSnapshot
		db.Mdb.Select(relatedSnapshotSelectFields).Where("snapshot_version = ? AND pid = ?", version, current.Pid).
			Order("hits DESC, id DESC").Limit(15).Find(&hotRows)
		appendUnique(hotRows)
	}

	return list
}

func appendCategoryFallbacks(version string, current model.FilmListSnapshot, snapshots []model.FilmListSnapshot, targetCount int) []model.FilmListSnapshot {
	seen := make(map[int64]struct{}, len(snapshots)+1)
	seen[current.Mid] = struct{}{}
	for _, snap := range snapshots {
		seen[snap.Mid] = struct{}{}
	}

	needed := targetCount - len(snapshots)
	if needed <= 0 {
		return snapshots
	}

	query := db.Mdb.Select(relatedSnapshotSelectFields).Where("snapshot_version = ?", version)
	if current.Cid > 0 {
		query = query.Where("cid = ?", current.Cid)
	} else if current.Pid > 0 {
		query = query.Where("pid = ?", current.Pid)
	}

	var fallbackRows []model.FilmListSnapshot
	query.Order("hits DESC, id DESC").Limit(needed * 2).Find(&fallbackRows)
	for _, row := range fallbackRows {
		if _, ok := seen[row.Mid]; ok {
			continue
		}
		seen[row.Mid] = struct{}{}
		snapshots = append(snapshots, row)
		if len(snapshots) >= targetCount {
			break
		}
	}
	return snapshots
}

type relatedSnapshotScore struct {
	snapshot model.FilmListSnapshot
	score    int
}

type relatedSnapshotContext struct {
	snapshot  model.FilmListSnapshot
	coreToken string
	tagSet    map[string]struct{}
	directors map[string]struct{}
	actors    map[string]struct{}
}

func buildRelatedSnapshotContext(snapshot model.FilmListSnapshot) relatedSnapshotContext {
	return relatedSnapshotContext{
		snapshot:  snapshot,
		coreToken: extractCoreSearchToken(snapshot.Name),
		tagSet:    buildTagSet(splitClassTags(snapshot.ClassTag)),
		directors: splitPeopleSet(snapshot.Director),
		actors:    splitPeopleSet(snapshot.Actor),
	}
}

func scoreRelatedSnapshot(context relatedSnapshotContext, candidate model.FilmListSnapshot) int {
	current := context.snapshot
	relationScore := 0
	if current.SeriesKey != "" && current.SeriesKey == candidate.SeriesKey {
		relationScore += 100
	}
	relationScore += titleRelatedScore(context.coreToken, candidate)
	relationScore += tagRelatedScore(context.tagSet, splitClassTags(candidate.ClassTag))
	relationScore += peopleRelatedScore(context.directors, candidate.Director, 24)
	relationScore += peopleRelatedScore(context.actors, candidate.Actor, 18)
	if relationScore == 0 {
		return 0
	}

	score := relationScore
	if current.Cid > 0 && current.Cid == candidate.Cid {
		score += 18
	}
	score += snapshotMetaRelatedScore(current, candidate)
	return score
}

func titleRelatedScore(coreToken string, candidate model.FilmListSnapshot) int {
	if coreToken == "" {
		return 0
	}
	candidateCoreToken := extractCoreSearchToken(candidate.Name)
	name := strings.TrimSpace(candidate.Name)
	subTitle := strings.TrimSpace(candidate.SubTitle)
	switch {
	case candidateCoreToken != "" && candidateCoreToken == coreToken:
		return 45
	case name == coreToken:
		return 35
	case strings.HasPrefix(name, coreToken):
		return 25
	case strings.Contains(name, coreToken):
		return 18
	case subTitle != "" && strings.Contains(subTitle, coreToken):
		return 10
	default:
		return 0
	}
}

func tagRelatedScore(currentSet map[string]struct{}, candidateTags []string) int {
	if len(currentSet) == 0 || len(candidateTags) == 0 {
		return 0
	}
	score := 0
	for _, tag := range candidateTags {
		if _, ok := currentSet[tag]; ok {
			score += 12
			if score >= 36 {
				return 36
			}
		}
	}
	return score
}

func peopleRelatedScore(currentSet map[string]struct{}, candidate string, maxScore int) int {
	if len(currentSet) == 0 {
		return 0
	}
	score := 0
	for _, name := range splitPeople(candidate) {
		if _, ok := currentSet[name]; ok {
			score += 8
			if score >= maxScore {
				return maxScore
			}
		}
	}
	return score
}

func splitPeopleSet(raw string) map[string]struct{} {
	people := splitPeople(raw)
	set := make(map[string]struct{}, len(people))
	for _, name := range people {
		set[name] = struct{}{}
	}
	return set
}

func splitPeople(raw string) []string {
	return strings.FieldsFunc(strings.TrimSpace(raw), func(r rune) bool {
		switch r {
		case ',', '，', '/', '|', '、', ' ':
			return true
		}
		return false
	})
}

func snapshotMetaRelatedScore(current model.FilmListSnapshot, candidate model.FilmListSnapshot) int {
	score := 0
	if current.Year > 0 && candidate.Year > 0 {
		diff := current.Year - candidate.Year
		if diff < 0 {
			diff = -diff
		}
		if diff == 0 {
			score += 8
		} else if diff == 1 {
			score += 4
		}
	}
	if current.Area != "" && current.Area == candidate.Area {
		score += 5
	}
	if current.Language != "" && current.Language == candidate.Language {
		score += 3
	}
	return score
}

func sortRelatedSnapshots(scores []relatedSnapshotScore) {
	slices.SortFunc(scores, func(left, right relatedSnapshotScore) int {
		if left.score != right.score {
			if left.score > right.score {
				return -1
			}
			return 1
		}
		if left.snapshot.UpdateStamp != right.snapshot.UpdateStamp {
			if left.snapshot.UpdateStamp > right.snapshot.UpdateStamp {
				return -1
			}
			return 1
		}
		if left.snapshot.Mid > right.snapshot.Mid {
			return -1
		}
		return 1
	})
}

func relatedScoresToSnapshots(scores []relatedSnapshotScore) []model.FilmListSnapshot {
	snapshots := make([]model.FilmListSnapshot, 0, len(scores))
	for _, item := range scores {
		snapshots = append(snapshots, item.snapshot)
	}
	return snapshots
}

