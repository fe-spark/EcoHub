package snapshot

import (
	"context"
	"log"
	"math"
	"strings"

	"server/internal/film/shared"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/taxonomy"
	"server/internal/utils"
)

// HotChartCandidate 外部榜单中的单条候选影片。
type HotChartCandidate struct {
	DoubanID  int64
	Title     string
	Year      int64
	MediaKind string // "movie" | "tv" | "variety" | "anime"
}

type dbDoubanMatchRow struct {
	Mid             int64
	DbId            int64
	Name            string
	Pid             int64
	Cid             int64
	CategoryKey     string
	RootCategoryKey string
	Hits            int64
	Year            int64
}

func categoryKindMatches(mediaKind string, pid, cid int64) (matched bool, conflict bool) {
	rootName := ""
	if pid > 0 {
		rootName = strings.TrimSpace(taxonomy.GetCategoryNameById(pid))
	}
	if rootName == "" && cid > 0 {
		rootName = strings.TrimSpace(taxonomy.GetCategoryNameById(cid))
	}
	if rootName == "" {
		return false, false
	}

	isMovieCat := strings.Contains(rootName, "电影")
	isTVOrAnimeOrVariety := strings.Contains(rootName, "剧") ||
		strings.Contains(rootName, "动漫") ||
		strings.Contains(rootName, "动画") ||
		strings.Contains(rootName, "综艺")

	switch mediaKind {
	case "movie":
		if isMovieCat {
			return true, false
		}
		if isTVOrAnimeOrVariety {
			return false, true
		}
	case "tv":
		if strings.Contains(rootName, "剧") {
			return true, false
		}
		if isMovieCat {
			return false, true
		}
	case "variety":
		if strings.Contains(rootName, "综艺") {
			return true, false
		}
		if isMovieCat {
			return false, true
		}
	case "anime":
		if strings.Contains(rootName, "动漫") || strings.Contains(rootName, "动画") || strings.Contains(rootName, "番") {
			return true, false
		}
		if isMovieCat {
			return false, true
		}
	}
	return false, false
}

func scoreMatchedCandidate(cand HotChartCandidate, pid, cid, year, hits int64) (int64, bool) {
	matchedKind, conflict := categoryKindMatches(cand.MediaKind, pid, cid)
	if conflict {
		return 0, false
	}
	if cand.Year > 0 && year > 0 {
		diff := int64(math.Abs(float64(cand.Year - year)))
		if diff > 3 {
			return 0, false
		}
	}
	var score int64 = 100
	if matchedKind {
		score += 500
	}
	if cand.Year > 0 && year > 0 {
		diff := int64(math.Abs(float64(cand.Year - year)))
		if diff == 0 {
			score += 300
		} else if diff == 1 {
			score += 150
		}
	}
	if hits > 0 {
		if hits > 200 {
			hits = 200
		}
		score += hits
	}
	return score, true
}

func collectHotChartMatchKeys(candidates []HotChartCandidate) []string {
	keySet := make(map[string]struct{}, len(candidates)*2)
	keys := make([]string, 0, len(candidates)*2)
	addKey := func(raw string) {
		if raw == "" {
			return
		}
		hashed := utils.GenerateHashKey(raw)
		if hashed == "" {
			return
		}
		if _, ok := keySet[hashed]; ok {
			return
		}
		keySet[hashed] = struct{}{}
		keys = append(keys, hashed)
	}
	for _, c := range candidates {
		if c.DoubanID > 0 {
			addKey(utils.BuildCollectionDbIdentity(c.DoubanID, c.Title))
			addKey(utils.BuildCollectionDbIdentity(c.DoubanID, ""))
		}
		if norm := utils.NormalizeIdentityTitle(c.Title); norm != "" {
			addKey(norm)
		}
	}
	return keys
}

// MatchHotChartCandidates 将外部榜单条目反查为指定采集站可播放的本地影片 Mid 列表。
// 优先通过 movie_match_key / db_id 精确命中，未命中者主站走内存索引、非主站走带索引的片名查询兜底，全程避免 film_index 全表扫。
func MatchHotChartCandidates(version, sourceID string, candidates []HotChartCandidate, limit int, exclude ...int64) ([]int64, map[int64]int, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || len(candidates) == 0 || limit <= 0 {
		return nil, map[int64]int{}, nil
	}

	vis := SourceCategoryVisibility(sourceID)
	dbHitByDoubanID := make(map[int64]int64)
	excludeSet := make(map[int64]struct{}, len(exclude))
	for _, ex := range exclude {
		if ex > 0 {
			excludeSet[ex] = struct{}{}
		}
	}

	type candEntry struct {
		mid  int64
		pid  int64
		cid  int64
		year int64
		hits int64
	}
	entriesByNorm := make(map[string][]candEntry)
	seenCandMid := make(map[int64]struct{})

	appendRow := func(r dbDoubanMatchRow) {
		if r.Mid <= 0 {
			return
		}
		if _, excluded := excludeSet[r.Mid]; excluded {
			return
		}
		if !vis(r.CategoryKey, r.RootCategoryKey, r.Cid, r.Pid) {
			return
		}
		if r.DbId > 0 {
			if _, exists := dbHitByDoubanID[r.DbId]; !exists {
				dbHitByDoubanID[r.DbId] = r.Mid
			}
		}
		if _, seen := seenCandMid[r.Mid]; seen {
			return
		}
		seenCandMid[r.Mid] = struct{}{}
		norm := utils.NormalizeIdentityTitle(r.Name)
		if norm == "" {
			return
		}
		entriesByNorm[norm] = append(entriesByNorm[norm], candEntry{
			mid:  r.Mid,
			pid:  r.Pid,
			cid:  r.Cid,
			year: r.Year,
			hits: r.Hits,
		})
	}

	matchKeys := collectHotChartMatchKeys(candidates)
	if db.Mdb != nil && len(matchKeys) > 0 {
		var matchedMids []int64
		if err := db.Mdb.Model(&model.MovieMatchKey{}).
			Distinct("mid").
			Where("match_key IN ?", matchKeys).
			Pluck("mid", &matchedMids).Error; err != nil {
			log.Printf("[HotChart] 查询 movie_match_key 失败: %v", err)
		} else if len(matchedMids) > 0 {
			var rows []dbDoubanMatchRow
			if err := db.Mdb.Table(model.TableFilmIndex).
				Select("mid, db_id, name, pid, cid, category_key, root_category_key, hits, year").
				Where("deleted_at IS NULL AND mid IN ?", matchedMids).
				Where(model.FilmHasPlaySourceSQL(), sourceID, model.LineKindPlay).
				Order("hits DESC, mid DESC").
				Find(&rows).Error; err != nil {
				log.Printf("[HotChart] 按 match_key 回查本地片库失败: %v", err)
			} else {
				for _, r := range rows {
					appendRow(r)
				}
			}
		}
	}

	// 收集尚未命中的候选归一化片名
	neededTitles := make(map[string]struct{})
	unmatchedRawTitles := make([]string, 0)
	for _, c := range candidates {
		if c.DoubanID > 0 && dbHitByDoubanID[c.DoubanID] > 0 {
			continue
		}
		norm := utils.NormalizeIdentityTitle(c.Title)
		if norm == "" {
			continue
		}
		if _, already := entriesByNorm[norm]; already {
			continue
		}
		neededTitles[norm] = struct{}{}
		if t := strings.TrimSpace(c.Title); t != "" {
			unmatchedRawTitles = append(unmatchedRawTitles, t)
		}
	}

	if len(neededTitles) > 0 {
		idx := activeFilmSearchMetas.Load()
		if idx != nil && idx.PrimaryID == sourceID {
			visitMeta := func(meta *FilmSearchMeta) {
				if meta == nil || meta.Mid <= 0 || !meta.HasPlaySource(sourceID) {
					return
				}
				if _, excluded := excludeSet[meta.Mid]; excluded {
					return
				}
				if _, seen := seenCandMid[meta.Mid]; seen {
					return
				}
				norm := utils.NormalizeIdentityTitle(meta.Item.Name)
				if norm == "" {
					return
				}
				if _, needed := neededTitles[norm]; !needed {
					return
				}
				if !vis(meta.CategoryKey, meta.RootCategoryKey, meta.Cid, meta.Pid) {
					return
				}
				seenCandMid[meta.Mid] = struct{}{}
				entriesByNorm[norm] = append(entriesByNorm[norm], candEntry{
					mid:  meta.Mid,
					pid:  meta.Pid,
					cid:  meta.Cid,
					year: meta.Item.Year,
					hits: meta.Item.Hits,
				})
			}

			// 优先通过倒排索引按候选标题召回小集合，避免对百万级内存影片全量执行多正则 NormalizeIdentityTitle。
			fallbackCleanKeys := make(map[string]struct{})
			checkedPos := make(map[int]struct{})
			lookupTitle := func(title string) bool {
				q := utils.BuildQueryContext(title)
				if q.CleanKey == "" {
					return true
				}
				if idx.SearchPosts != nil {
					if positions, narrowed := idx.SearchPosts.lookup(q); narrowed {
						for _, pos := range positions {
							p := int(pos)
							if p < 0 || p >= len(idx.Items) {
								continue
							}
							if _, done := checkedPos[p]; done {
								continue
							}
							checkedPos[p] = struct{}{}
							visitMeta(idx.Items[p])
						}
						return true
					}
				}
				fallbackCleanKeys[q.CleanKey] = struct{}{}
				return false
			}
			for _, rawTitle := range unmatchedRawTitles {
				lookupTitle(rawTitle)
			}
			for norm := range neededTitles {
				lookupTitle(norm)
			}
			if len(fallbackCleanKeys) > 0 {
				// 极短标题（如单字片名）倒排无法收窄时，仅对 CleanName 精确相等的条目调用 NormalizeIdentityTitle。
				for pos, meta := range idx.Items {
					if meta == nil || meta.Mid <= 0 {
						continue
					}
					if _, done := checkedPos[pos]; done {
						continue
					}
					if _, ok := fallbackCleanKeys[meta.Item.CleanName]; !ok {
						continue
					}
					visitMeta(meta)
				}
			}
		} else if db.Mdb != nil && len(unmatchedRawTitles) > 0 {
			// 非主站或内存索引未覆盖该站时，使用 idx_film_index_name + 线路主键做精准回退
			var rows []dbDoubanMatchRow
			if err := db.Mdb.Table(model.TableFilmIndex).
				Select("mid, db_id, name, pid, cid, category_key, root_category_key, hits, year").
				Where("deleted_at IS NULL AND name IN ?", unmatchedRawTitles).
				Where(model.FilmHasPlaySourceSQL(), sourceID, model.LineKindPlay).
				Order("hits DESC, mid DESC").
				Find(&rows).Error; err == nil {
				for _, r := range rows {
					appendRow(r)
				}
			}
		}
	}

	mids := make([]int64, 0, limit)
	midToCandIdx := make(map[int64]int, limit)
	usedMids := make(map[int64]struct{}, limit)

	for idx, c := range candidates {
		if len(mids) >= limit {
			break
		}
		var chosenMid int64
		if c.DoubanID > 0 {
			chosenMid = dbHitByDoubanID[c.DoubanID]
		}
		if chosenMid == 0 {
			norm := utils.NormalizeIdentityTitle(c.Title)
			if norm != "" {
				var bestScore int64
				for _, entry := range entriesByNorm[norm] {
					sc, ok := scoreMatchedCandidate(c, entry.pid, entry.cid, entry.year, entry.hits)
					if !ok {
						continue
					}
					if sc > bestScore {
						bestScore = sc
						chosenMid = entry.mid
					}
				}
			}
		}
		if chosenMid <= 0 {
			continue
		}
		if _, dup := usedMids[chosenMid]; dup {
			continue
		}
		usedMids[chosenMid] = struct{}{}
		mids = append(mids, chosenMid)
		midToCandIdx[chosenMid] = idx
	}

	return mids, midToCandIdx, nil
}

// ListSnapshotsByMIDsForHotCharts 按给定 Mid 顺序批量加载影片快照。
func ListSnapshotsByMIDsForHotCharts(ctx context.Context, mids []int64) ([]model.FilmListSnapshot, error) {
	if db.Mdb == nil || len(mids) == 0 {
		return []model.FilmListSnapshot{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return listSnapshotsByMIDs(db.Mdb.WithContext(ctx), mids)
}

// MovieBasicInfosByMIDs 按给定 Mid 列表批量组装前台展示用的影片基础信息映射。
func MovieBasicInfosByMIDs(version string, mids []int64) (map[int64]model.MovieBasicInfo, error) {
	return MovieBasicInfosByMIDsCtx(context.Background(), version, mids)
}

// MovieBasicInfosByMIDsCtx 按给定 Mid 列表批量组装前台展示用的影片基础信息映射，受 ctx 超时控制。
func MovieBasicInfosByMIDsCtx(ctx context.Context, version string, mids []int64) (map[int64]model.MovieBasicInfo, error) {
	if len(mids) == 0 {
		return map[int64]model.MovieBasicInfo{}, nil
	}
	if ctx != nil && ctx.Err() != nil {
		return nil, ctx.Err()
	}
	snaps, err := ListSnapshotsByMIDsForHotCharts(ctx, mids)
	if err != nil {
		return nil, err
	}
	infos := shared.BuildMovieBasicInfosFromSnapshots(snaps...)
	byID := make(map[int64]model.MovieBasicInfo, len(infos))
	for _, info := range infos {
		if info.Id > 0 {
			byID[info.Id] = info
		}
	}
	return byID, nil
}

// VisiblePlayableMIDs 批量判断指定影片在该采集站既有可播线路，且所属分类未在分类管理中隐藏。
func VisiblePlayableMIDs(ctx context.Context, sourceID string, mids []int64) (map[int64]bool, error) {
	out := make(map[int64]bool, len(mids))
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || len(mids) == 0 || db.Mdb == nil {
		return out, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	type visRow struct {
		Mid             int64
		Pid             int64
		Cid             int64
		CategoryKey     string
		RootCategoryKey string
	}
	vis := SourceCategoryVisibility(sourceID)
	for start := 0; start < len(mids); start += 500 {
		end := start + 500
		if end > len(mids) {
			end = len(mids)
		}
		var rows []visRow
		if err := db.Mdb.WithContext(ctx).Model(&model.FilmIndex{}).
			Select("mid, pid, cid, category_key, root_category_key").
			Where("mid IN ?", mids[start:end]).
			Where(model.FilmHasPlaySourceSQL(), sourceID, model.LineKindPlay).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			if vis(r.CategoryKey, r.RootCategoryKey, r.Cid, r.Pid) {
				out[r.Mid] = true
			}
		}
	}
	return out, nil
}
