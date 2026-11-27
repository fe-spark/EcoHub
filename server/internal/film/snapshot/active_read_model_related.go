package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"

	"server/internal/config"
	"server/internal/film/shared"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"

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

func ListRelatedSnapshotsReadModel(version string, snapshot model.FilmListSnapshot, page *dto.Page, sourceIdOpt ...string) ([]model.FilmListSnapshot, error) {
	startedAt := time.Now()
	page = shared.EnsurePage(page)
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" || snapshot.Mid <= 0 {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}, nil
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

	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			if json.Unmarshal([]byte(data), &candidates) == nil {
				hitCache = true
				log.Printf("[FilmRelate] 相关推荐候选集命中缓存 mid=%d name=%q source=%q cache=HIT candidates=%d cost=%s",
					snapshot.Mid, snapshot.Name, sourceID, len(candidates), time.Since(startedAt))
			}
		}
	}

	if !hitCache {
		var sfKey string
		if sourceID != "" {
			sfKey = fmt.Sprintf("v%s:src_%s:%d", version, sourceID, snapshot.Mid)
		} else {
			sfKey = fmt.Sprintf("v%s:%d", version, snapshot.Mid)
		}
		val, err, _ := relatedSnapshotsSf.Do(sfKey, func() (any, error) {
			if db.Rdb != nil {
				if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
					var cached []model.FilmListSnapshot
					if json.Unmarshal([]byte(data), &cached) == nil {
						return cached, nil
					}
				}
			}

			listGen := GetSearchCacheVersion()
			cands, loadErr := loadRelatedSnapshotCandidates(version, snapshot, maxRelatedRecommendCount, sourceID)
			if loadErr != nil {
				return nil, loadErr
			}
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
		if err != nil {
			return nil, err
		}
		cands, ok := val.([]model.FilmListSnapshot)
		if !ok {
			return nil, errors.New("相关推荐结果异常")
		}
		candidates = cands
	}

	curTotal := len(candidates)
	curPageCount := (curTotal + page.PageSize - 1) / page.PageSize
	if curPageCount <= 0 {
		curPageCount = 1
	}
	page.Total = curTotal
	page.PageCount = curPageCount

	result := pageSnapshots(candidates, page)
	return cloneFilmListSnapshots(result), nil
}

const candidatePoolLimit = 60

func loadRelatedSnapshotCandidates(version string, current model.FilmListSnapshot, maxCandidates int, sourceID string) ([]model.FilmListSnapshot, error) {
	seen := make(map[int64]struct{}, candidatePoolLimit+1)
	seen[current.Mid] = struct{}{}
	list := make([]model.FilmListSnapshot, 0, candidatePoolLimit)
	effectiveSourceID := strings.TrimSpace(sourceID)
	if effectiveSourceID == "" {
		effectiveSourceID = strings.TrimSpace(current.SourceId)
	}
	if effectiveSourceID == "" {
		effectiveSourceID = currentPrimarySourceID()
	}
	categoryVisible := SourceCategoryVisibility(effectiveSourceID)

	appendUnique := func(src []model.FilmListSnapshot) {
		for _, item := range src {
			if _, ok := seen[item.Mid]; ok {
				continue
			}
			if !categoryVisible(item.CategoryKey, item.RootCategoryKey, item.Cid, item.Pid) {
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

	recall := func(query *gorm.DB) error {
		rows, err := scanListSnapshots(query)
		if err != nil {
			return err
		}
		appendUnique(rows)
		return nil
	}

	if current.SeriesKey != "" && db.Mdb != nil {
		if err := recall(baseQuery().
			Where("series_key = ? AND mid <> ?", current.SeriesKey, current.Mid).
			Order("hits DESC, mid DESC").Limit(candidatePoolLimit)); err != nil {
			return nil, err
		}
	}

	if len(list) < candidatePoolLimit {
		if idx := activeFilmSearchMetas.Load(); canRecallRelatedFromIndex(idx, current, sourceID) {
			if mids := recallRelatedMidsFromIndex(idx, current, sourceID, seen, candidatePoolLimit-len(list)); len(mids) > 0 {
				rows, err := GetSnapshotsByMidsOrdered(version, mids)
				if err != nil {
					return nil, err
				}
				appendUnique(rows)
			}
			return rankRelatedCandidates(current, list, maxCandidates), nil
		}
	}

	coreToken := extractCoreSearchToken(current.Name)
	if len(list) < candidatePoolLimit && len([]rune(coreToken)) >= 2 && db.Mdb != nil {
		if err := recall(baseQuery().
			Where("name LIKE ? AND mid <> ?", coreToken+"%", current.Mid).
			Order("hits DESC, mid DESC").Limit(candidatePoolLimit - len(list))); err != nil {
			return nil, err
		}
	}

	if len(list) < candidatePoolLimit && db.Mdb != nil {
		catKey := strings.TrimSpace(current.CategoryKey)
		if catKey != "" {
			if err := recall(baseQuery().
				Where("category_key = ? AND mid <> ?", catKey, current.Mid).
				Order("hits DESC, mid DESC").Limit(candidatePoolLimit - len(list))); err != nil {
				return nil, err
			}
		} else if current.Cid > 0 {
			if err := recall(baseQuery().
				Where("cid = ? AND mid <> ?", current.Cid, current.Mid).
				Order("hits DESC, mid DESC").Limit(candidatePoolLimit - len(list))); err != nil {
				return nil, err
			}
		}
	}

	rootKey, pid := resolveCurrentRootCategory(current)
	if len(list) < candidatePoolLimit && db.Mdb != nil {
		if rootKey != "" {
			if err := recall(baseQuery().
				Where("root_category_key = ? AND mid <> ?", rootKey, current.Mid).
				Order("hits DESC, mid DESC").Limit(candidatePoolLimit - len(list))); err != nil {
				return nil, err
			}
		}
		if len(list) < candidatePoolLimit && pid > 0 {
			if err := recall(baseQuery().
				Where("pid = ? AND mid <> ?", pid, current.Mid).
				Order("hits DESC, mid DESC").Limit(candidatePoolLimit - len(list))); err != nil {
				return nil, err
			}
		}
	}

	if len(list) < candidatePoolLimit && rootKey == "" && pid <= 0 && db.Mdb != nil {
		if err := recall(baseQuery().
			Where("mid <> ?", current.Mid).
			Order("hits DESC, mid DESC").Limit(candidatePoolLimit - len(list))); err != nil {
			return nil, err
		}
	}

	if len(list) < candidatePoolLimit && sourceID == "" && rootKey == "" && pid <= 0 && db.Mdb != nil {
		if err := recall(liveFilmQuery().
			Select(relatedSnapshotSelectFields).
			Where("mid <> ?", current.Mid).
			Order("hits DESC, mid DESC").Limit(candidatePoolLimit - len(list))); err != nil {
			return nil, err
		}
	}

	return rankRelatedCandidates(current, list, maxCandidates), nil
}

func canRecallRelatedFromIndex(idx *filmSearchMetaIndex, current model.FilmListSnapshot, sourceID string) bool {
	if idx == nil || len(idx.Items) == 0 {
		return false
	}
	if idx.PrimaryID == "" || idx.PrimaryID != currentPrimarySourceID() {
		return false
	}
	if sourceID != "" {
		return sourceID == idx.PrimaryID
	}
	curSrc := strings.TrimSpace(current.SourceId)
	return curSrc == "" || curSrc == idx.PrimaryID
}

func hasTitlePrefixFold(name, lowerPrefix string) bool {
	if lowerPrefix == "" {
		return true
	}
	trimmed := strings.TrimSpace(name)
	if trimmed == "" {
		return false
	}
	return strings.HasPrefix(strings.ToLower(trimmed), lowerPrefix)
}

func recallRelatedMidsFromIndex(idx *filmSearchMetaIndex, current model.FilmListSnapshot, sourceID string, seen map[int64]struct{}, need int) []int64 {
	if need <= 0 {
		return nil
	}
	recalled := make([]int64, 0, need)
	picked := make(map[int64]struct{}, len(seen)+need)
	for mid := range seen {
		picked[mid] = struct{}{}
	}
	effectiveSourceID := strings.TrimSpace(sourceID)
	if effectiveSourceID == "" {
		effectiveSourceID = strings.TrimSpace(current.SourceId)
	}
	if effectiveSourceID == "" {
		effectiveSourceID = idx.PrimaryID
	}
	categoryVisible := SourceCategoryVisibility(effectiveSourceID)
	accept := func(meta *FilmSearchMeta) bool {
		if meta == nil || meta.Mid <= 0 {
			return false
		}
		if _, exists := picked[meta.Mid]; exists {
			return false
		}
		if !categoryVisible(meta.CategoryKey, meta.RootCategoryKey, meta.Cid, meta.Pid) {
			return false
		}
		if sourceID != "" && !meta.HasPlaySource(sourceID) {
			return false
		}
		return true
	}
	addMids := func(mids []int64) int {
		added := 0
		for _, mid := range mids {
			if len(recalled) >= need {
				return added
			}
			pos, ok := idx.Pos[mid]
			if !ok || pos < 0 || pos >= len(idx.Items) {
				continue
			}
			if !accept(idx.Items[pos]) {
				continue
			}
			picked[mid] = struct{}{}
			recalled = append(recalled, mid)
			added++
		}
		return added
	}
	takeTopByHits := func(cands []*FilmSearchMeta) {
		remain := need - len(recalled)
		if len(cands) == 0 || remain <= 0 {
			return
		}
		top := topKSearchMetas(cands, remain, func(a, b *FilmSearchMeta) bool {
			if a.Item.Hits != b.Item.Hits {
				return a.Item.Hits > b.Item.Hits
			}
			return a.Mid > b.Mid
		})
		for _, meta := range top {
			picked[meta.Mid] = struct{}{}
			recalled = append(recalled, meta.Mid)
		}
	}

	// 1. 核心片名前缀召回（非汉字/非 ASCII 字符经 CleanCompactText 置空时跳过倒排窄化，改走全量大小写不敏感前缀匹配）
	coreToken := extractCoreSearchToken(current.Name)
	if len([]rune(coreToken)) >= 2 {
		lowerPrefix := strings.ToLower(strings.TrimSpace(coreToken))
		var prefixHits []*FilmSearchMeta
		q := utils.BuildQueryContext(coreToken)
		positions, narrowed := ([]int32)(nil), false
		if q.CleanKey != "" {
			positions, narrowed = idx.SearchPosts.lookup(q)
		}
		if narrowed {
			for _, pos := range positions {
				if pos < 0 || int(pos) >= len(idx.Items) {
					continue
				}
				meta := idx.Items[pos]
				if accept(meta) && hasTitlePrefixFold(meta.Item.Name, lowerPrefix) {
					prefixHits = append(prefixHits, meta)
				}
			}
		} else {
			for _, meta := range idx.Items {
				if accept(meta) && hasTitlePrefixFold(meta.Item.Name, lowerPrefix) {
					prefixHits = append(prefixHits, meta)
				}
			}
		}
		takeTopByHits(prefixHits)
	}

	// 2. 同子分类召回（HotPools 已含当前主站该分类按热度 Top 50，命中非空池时无需全表重扫）
	if len(recalled) < need {
		catKey := strings.TrimSpace(current.CategoryKey)
		if catKey != "" {
			hotPool := idx.HotPools[hotPoolMapKey(idx.PrimaryID, catKey)]
			addMids(hotPool)
			if len(recalled) < need && len(hotPool) == 0 {
				var catCands []*FilmSearchMeta
				for _, pos := range idx.ByCategory[catKey] {
					if pos < 0 || pos >= len(idx.Items) {
						continue
					}
					if meta := idx.Items[pos]; accept(meta) {
						catCands = append(catCands, meta)
					}
				}
				takeTopByHits(catCands)
			}
		} else if current.Cid > 0 {
			var cidCands []*FilmSearchMeta
			for _, meta := range idx.Items {
				if accept(meta) && meta.Cid == current.Cid {
					cidCands = append(cidCands, meta)
				}
			}
			takeTopByHits(cidCands)
		}
	}

	// 3. 同根分类召回（优先消费 HotPools，命中非空池时跳过全量扫描）
	rootKey, pid := resolveCurrentRootCategory(current)
	if len(recalled) < need {
		if rootKey != "" {
			hotPool := idx.HotPools[hotPoolMapKey(idx.PrimaryID, rootKey)]
			addMids(hotPool)
			if len(recalled) < need && len(hotPool) == 0 {
				var rootCands []*FilmSearchMeta
				for _, meta := range idx.Items {
					if accept(meta) && meta.RootCategoryKey == rootKey {
						rootCands = append(rootCands, meta)
					}
				}
				takeTopByHits(rootCands)
			}
		}
		if len(recalled) < need && pid > 0 && rootKey == "" {
			var pidCands []*FilmSearchMeta
			for _, meta := range idx.Items {
				if accept(meta) && meta.Pid == pid {
					pidCands = append(pidCands, meta)
				}
			}
			takeTopByHits(pidCands)
		}
	}

	// 4. 无分类归属时的热播兜底
	if len(recalled) < need && rootKey == "" && pid <= 0 {
		srcPoolKey := idx.PrimaryID
		if sourceID != "" {
			srcPoolKey = sourceID
		}
		hotPool := idx.SourceHot[srcPoolKey]
		addMids(hotPool)
		if len(recalled) < need && len(hotPool) == 0 {
			var hotCands []*FilmSearchMeta
			for _, meta := range idx.Items {
				if accept(meta) {
					hotCands = append(hotCands, meta)
				}
			}
			takeTopByHits(hotCands)
		}
	}

	return recalled
}
