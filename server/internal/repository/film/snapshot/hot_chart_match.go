package snapshot

import (
	"context"
	"log"
	"math"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
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
		rootName = strings.TrimSpace(support.GetCategoryNameById(pid))
	}
	if rootName == "" && cid > 0 {
		rootName = strings.TrimSpace(support.GetCategoryNameById(cid))
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

// MatchHotChartCandidates 将外部榜单条目反查为指定采集站可播放的本地影片 Mid 列表。
// 优先通过 db_id 精确命中，未命中者主站走内存索引、非主站走数据库片名兜底。
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

	doubanIDs := make([]int64, 0, len(candidates))
	seenDouban := make(map[int64]struct{}, len(candidates))
	for _, c := range candidates {
		if c.DoubanID > 0 {
			if _, ok := seenDouban[c.DoubanID]; !ok {
				seenDouban[c.DoubanID] = struct{}{}
				doubanIDs = append(doubanIDs, c.DoubanID)
			}
		}
	}

	if len(doubanIDs) > 0 && db.Mdb != nil {
		var rows []dbDoubanMatchRow
		err := db.Mdb.Table(model.TableFilmIndex).
			Select("mid, db_id, name, pid, cid, category_key, root_category_key, hits, year").
			Where("deleted_at IS NULL AND first_source_id = ? AND db_id IN ?", sourceID, doubanIDs).
			Order("hits DESC, mid DESC").
			Find(&rows).Error
		if err != nil {
			log.Printf("[HotChart] 按 db_id 查询本地片库失败: %v", err)
		} else {
			for _, r := range rows {
				if r.Mid <= 0 || r.DbId <= 0 {
					continue
				}
				if _, excluded := excludeSet[r.Mid]; excluded {
					continue
				}
				if !vis(r.CategoryKey, r.RootCategoryKey, r.Cid, r.Pid) {
					continue
				}
				if _, exists := dbHitByDoubanID[r.DbId]; !exists {
					dbHitByDoubanID[r.DbId] = r.Mid
				}
			}
		}
	}

	// 收集未通过 db_id 命中的候选片名
	neededTitles := make(map[string]struct{})
	rawTitles := make([]string, 0)
	for _, c := range candidates {
		if c.DoubanID > 0 && dbHitByDoubanID[c.DoubanID] > 0 {
			continue
		}
		if t := strings.TrimSpace(c.Title); t != "" {
			rawTitles = append(rawTitles, t)
		}
		norm := utils.NormalizeIdentityTitle(c.Title)
		if norm != "" {
			neededTitles[norm] = struct{}{}
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
	if len(neededTitles) > 0 {
		idx := activeFilmSearchMetas.Load()
		if idx != nil && idx.PrimaryID == sourceID {
			for _, meta := range idx.Items {
				if meta == nil || meta.Mid <= 0 {
					continue
				}
				if _, excluded := excludeSet[meta.Mid]; excluded {
					continue
				}
				norm := utils.NormalizeIdentityTitle(meta.Item.Name)
				if norm == "" {
					continue
				}
				if _, needed := neededTitles[norm]; !needed {
					continue
				}
				if !vis(meta.CategoryKey, meta.RootCategoryKey, meta.Cid, meta.Pid) {
					continue
				}
				entriesByNorm[norm] = append(entriesByNorm[norm], candEntry{
					mid:  meta.Mid,
					pid:  meta.Pid,
					cid:  meta.Cid,
					year: meta.Item.Year,
					hits: meta.Item.Hits,
				})
			}
		} else if db.Mdb != nil && len(rawTitles) > 0 {
			// 非主站或内存索引未覆盖该站时，通过 MySQL 精确片名兜底
			var rows []dbDoubanMatchRow
			if err := db.Mdb.Table(model.TableFilmIndex).
				Select("mid, db_id, name, pid, cid, category_key, root_category_key, hits, year").
				Where("deleted_at IS NULL AND first_source_id = ? AND name IN ?", sourceID, rawTitles).
				Order("hits DESC, mid DESC").
				Find(&rows).Error; err == nil {
				for _, r := range rows {
					if r.Mid <= 0 {
						continue
					}
					if _, excluded := excludeSet[r.Mid]; excluded {
						continue
					}
					if !vis(r.CategoryKey, r.RootCategoryKey, r.Cid, r.Pid) {
						continue
					}
					norm := utils.NormalizeIdentityTitle(r.Name)
					entriesByNorm[norm] = append(entriesByNorm[norm], candEntry{
						mid:  r.Mid,
						pid:  r.Pid,
						cid:  r.Cid,
						year: r.Year,
						hits: r.Hits,
					})
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
