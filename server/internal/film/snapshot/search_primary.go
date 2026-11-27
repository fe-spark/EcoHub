package snapshot

import (
	"sort"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/sourcecatalog"
	"server/internal/utils"
)

// currentPrimarySourceID 当前主站。没有已启用采集站时为空。
func currentPrimarySourceID() string {
	return sourcecatalog.CurrentPrimaryID()
}

func indexMatches(cur *filmSearchMetaIndex, version, primary string) bool {
	return cur != nil && cur.Version != "" && cur.Version == version && cur.PrimaryID == primary
}

// DropPrimarySearchIndex 立刻丢掉内存里的检索索引。切换主站时先调用，避免防抖期间继续占用上一站。
func DropPrimarySearchIndex() {
	activeFilmSearchMetasMu.Lock()
	activeFilmSearchMetas.Store(nil)
	activeFilmSearchMetasMu.Unlock()
}

// EnsurePrimarySearchIndex 索引被丢掉又没有装上新主站时补建。已经有索引则不动。
func EnsurePrimarySearchIndex() error {
	if activeFilmSearchMetas.Load() != nil || currentPrimarySourceID() == "" || db.Mdb == nil {
		return nil
	}
	return RebuildPrimarySearchIndex()
}

// RebuildPrimarySearchIndex 丢掉当前索引，只装当前主站。若当前主站索引已就绪则复用。
func RebuildPrimarySearchIndex() error {
	version := currentSearchIndexVersion()
	if strings.TrimSpace(version) == "" || db.Mdb == nil {
		return ErrSearchIndexMissing
	}
	primary := currentPrimarySourceID()
	if cur := activeFilmSearchMetas.Load(); indexMatches(cur, version, primary) {
		return nil
	}
	activeFilmSearchMetasMu.Lock()
	activeFilmSearchMetas.Store(nil)
	activeFilmSearchMetasMu.Unlock()
	_, err := buildAndStoreFilmSearchMetaIndex(version)
	return err
}

// PrimarySearchMIDs 只留下查到且属于当前主站的影片编号。
// 没有主站、或库里没有这行、或所属站不是主站时都不留下。
// 数据库未连接时无法判断所属站，原样返回，避免测试里的封口队列被清空。
// 查询失败返回 error。写入检索索引前必须再用这次结果过滤；失败则放弃写入，不点查未过滤编号，也不因此全表重建。
func PrimarySearchMIDs(mids []int64) ([]int64, error) {
	_, kept, err := PrimarySearchMIDsWithSource(mids)
	return kept, err
}

// PrimarySearchMIDsWithSource 过滤出属于当前主站的影片编号，并同时返回过滤时使用的主站 ID。
func PrimarySearchMIDsWithSource(mids []int64) (string, []int64, error) {
	if len(mids) == 0 {
		return currentPrimarySourceID(), nil, nil
	}
	if db.Mdb == nil {
		return "", append([]int64(nil), mids...), nil
	}
	primary := currentPrimarySourceID()
	if primary == "" {
		return "", nil, nil
	}
	const batchSize = 2500
	owned := make(map[int64]string, len(mids))
	for i := 0; i < len(mids); i += batchSize {
		end := i + batchSize
		if end > len(mids) {
			end = len(mids)
		}
		var rows []struct {
			Mid           int64
			FirstSourceId string
		}
		if err := db.Mdb.Model(&model.FilmIndex{}).
			Select("mid, first_source_id").
			Where("mid IN ?", mids[i:end]).
			Find(&rows).Error; err != nil {
			return "", nil, err
		}
		for _, row := range rows {
			owned[row.Mid] = strings.TrimSpace(row.FirstSourceId)
		}
	}
	kept := make([]int64, 0, len(mids))
	for _, mid := range mids {
		src, ok := owned[mid]
		if !ok || src != primary {
			continue
		}
		kept = append(kept, mid)
	}
	return primary, kept, nil
}

// SearchPrimarySourceFilmMids 当主站内存索引就绪时，优先用倒排索引按片名检索并分页，避免百万表双次 LIKE 全表扫描。
func SearchPrimarySourceFilmMids(sourceID, keyword, sortField string, offset, limit int) ([]int64, int, bool) {
	sourceID = strings.TrimSpace(sourceID)
	keyword = strings.TrimSpace(keyword)
	if sourceID == "" || keyword == "" || limit <= 0 || offset < 0 {
		return nil, 0, false
	}
	idx := activeFilmSearchMetas.Load()
	if idx == nil || len(idx.Items) == 0 || idx.PrimaryID != sourceID || idx.PrimaryID != currentPrimarySourceID() || idx.SearchPosts == nil {
		return nil, 0, false
	}
	q := utils.BuildQueryContext(keyword)
	if q.CleanKey == "" {
		return nil, 0, false
	}
	positions, narrowed := idx.SearchPosts.lookup(q)
	if !narrowed {
		return nil, 0, false
	}
	hits := make([]*FilmSearchMeta, 0, len(positions))
	for _, pos := range positions {
		p := int(pos)
		if p < 0 || p >= len(idx.Items) {
			continue
		}
		meta := idx.Items[p]
		if meta == nil || meta.Mid <= 0 || !meta.HasPlaySource(sourceID) {
			continue
		}
		if utils.ScoreFilmMatch(meta.Item, q) <= 0 {
			continue
		}
		hits = append(hits, meta)
	}
	if len(hits) == 0 {
		return nil, 0, true
	}
	sortKey := strings.TrimSpace(sortField)
	sort.Slice(hits, func(i, j int) bool {
		a, b := hits[i], hits[j]
		switch sortKey {
		case "update_stamp", "latest":
			if a.Item.UpdateStamp != b.Item.UpdateStamp {
				return a.Item.UpdateStamp > b.Item.UpdateStamp
			}
		case "score":
			if a.Item.Score != b.Item.Score {
				return a.Item.Score > b.Item.Score
			}
			if a.Item.Hits != b.Item.Hits {
				return a.Item.Hits > b.Item.Hits
			}
		case "year":
			if a.Item.Year != b.Item.Year {
				return a.Item.Year > b.Item.Year
			}
			if a.Item.UpdateStamp != b.Item.UpdateStamp {
				return a.Item.UpdateStamp > b.Item.UpdateStamp
			}
		default:
			if a.Item.Hits != b.Item.Hits {
				return a.Item.Hits > b.Item.Hits
			}
			if a.Item.UpdateStamp != b.Item.UpdateStamp {
				return a.Item.UpdateStamp > b.Item.UpdateStamp
			}
		}
		return a.Mid > b.Mid
	})
	total := len(hits)
	if offset >= total {
		return []int64{}, total, true
	}
	end := min(offset+limit, total)
	mids := make([]int64, 0, end-offset)
	for _, m := range hits[offset:end] {
		mids = append(mids, m.Mid)
	}
	return mids, total, true
}
