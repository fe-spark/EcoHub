package snapshot

import (
	"sort"
	"strings"

	"server/internal/repository"
	"server/internal/repository/support"
)

// 分类页年份、热播、最新共用这一条定长榜。封口时各写入一次，请求只合并榜内 mid。
const categoryRankCap = searchHotPoolCap

type rankCand struct {
	mid   int64
	year  int64
	hits  int64
	stamp int64
}

func rankFromMeta(item *FilmSearchMeta) rankCand {
	return rankCand{
		mid:   item.Mid,
		year:  item.Item.Year,
		hits:  item.Item.Hits,
		stamp: item.Item.UpdateStamp,
	}
}

// rankBetter 三路只换比较字段。0 年份，1 热播，其余按更新时间。末位都用 mid。
func rankBetter(sortType int, a, b rankCand) bool {
	switch sortType {
	case 0:
		if a.year != b.year {
			return a.year > b.year
		}
		if a.stamp != b.stamp {
			return a.stamp > b.stamp
		}
	case 1:
		if a.hits != b.hits {
			return a.hits > b.hits
		}
	default:
		if a.stamp != b.stamp {
			return a.stamp > b.stamp
		}
	}
	return a.mid > b.mid
}

func keepRank(pools map[string][]rankCand, key string, cand rankCand, sortType, cap int) {
	if key == "" || cand.mid <= 0 || cap <= 0 {
		return
	}
	slot := pools[key]
	if len(slot) < cap {
		pools[key] = append(slot, cand)
		return
	}
	cold := 0
	for i := 1; i < len(slot); i++ {
		if rankBetter(sortType, slot[cold], slot[i]) {
			cold = i
		}
	}
	if rankBetter(sortType, cand, slot[cold]) {
		slot[cold] = cand
	}
}

func noteFilmRanks(hits, year, recent map[string][]rankCand, key string, item *FilmSearchMeta) {
	if item == nil {
		return
	}
	cand := rankFromMeta(item)
	keepRank(hits, key, cand, 1, categoryRankCap)
	keepRank(year, key, cand, 0, categoryRankCap)
	keepRank(recent, key, cand, 2, categoryRankCap)
}

func finishRanks(pools map[string][]rankCand, sortType int) map[string][]int64 {
	out := make(map[string][]int64, len(pools))
	for key, cands := range pools {
		out[key] = midsOf(cands, sortType, len(cands))
	}
	return out
}

func midsOf(cands []rankCand, sortType, limit int) []int64 {
	sort.Slice(cands, func(i, j int) bool {
		return rankBetter(sortType, cands[i], cands[j])
	})
	if limit >= 0 && len(cands) > limit {
		cands = cands[:limit]
	}
	mids := make([]int64, len(cands))
	for i := range cands {
		mids[i] = cands[i].mid
	}
	return mids
}

func rankPools(idx *filmSearchMetaIndex, sortType int) map[string][]int64 {
	if idx == nil {
		return nil
	}
	switch sortType {
	case 0:
		return idx.YearPools
	case 1:
		return idx.HotPools
	default:
		return idx.RecentPools
	}
}

// categoryRankMids 年份、热播、最新共用的取榜入口。
func categoryRankMids(version, sourceID, field string, categoryID int64, sortType, limit int) ([]int64, error) {
	if limit <= 0 || categoryID <= 0 {
		return nil, nil
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = strings.TrimSpace(GetActiveSnapshotVersion())
	}
	if version == "" {
		version = LiveReadVersion
	}
	idx, err := loadFilmSearchMetaIndex(version)
	if err != nil {
		return nil, err
	}
	if len(idx.Items) == 0 {
		return nil, nil
	}
	sourceID = strings.TrimSpace(sourceID)
	categoryID = listedCategoryID(sourceID, categoryID)
	if categoryID <= 0 {
		return nil, nil
	}
	if sourceID == "" {
		return scanLocalRanks(idx, field, categoryID, sortType, limit), nil
	}
	if limit > categoryRankCap {
		return scanSourceRanks(idx, sourceID, field, categoryID, sortType, limit), nil
	}
	return mergeRankPools(idx, rankPools(idx, sortType), sourceID, field, categoryID, sortType, limit), nil
}

func mergeRankPools(idx *filmSearchMetaIndex, pools map[string][]int64, sourceID, field string, categoryID int64, sortType, limit int) []int64 {
	typeIDs := repository.PublicSourceTypeIDs(sourceID, field, categoryID)
	seen := make(map[int64]struct{})
	cands := make([]rankCand, 0, limit)
	for _, typeID := range typeIDs {
		key := support.BuildSourceCategoryKey(sourceID, typeID)
		for _, mid := range pools[hotPoolMapKey(sourceID, key)] {
			if mid <= 0 {
				continue
			}
			if _, ok := seen[mid]; ok {
				continue
			}
			seen[mid] = struct{}{}
			pos, ok := idx.Pos[mid]
			if !ok || pos < 0 || pos >= len(idx.Items) {
				continue
			}
			cands = append(cands, rankFromMeta(&idx.Items[pos]))
		}
	}
	return midsOf(cands, sortType, limit)
}

func scanLocalRanks(idx *filmSearchMetaIndex, field string, categoryID int64, sortType, limit int) []int64 {
	pools := map[string][]rankCand{}
	for i := range idx.Items {
		item := &idx.Items[i]
		match := item.Pid == categoryID
		if field == "cid" {
			match = item.Cid == categoryID
		}
		if !match {
			continue
		}
		keepRank(pools, "k", rankFromMeta(item), sortType, limit)
	}
	return midsOf(pools["k"], sortType, limit)
}

func scanSourceRanks(idx *filmSearchMetaIndex, sourceID, field string, categoryID int64, sortType, limit int) []int64 {
	keys := make(map[string]struct{})
	for _, typeID := range repository.PublicSourceTypeIDs(sourceID, field, categoryID) {
		key := support.BuildSourceCategoryKey(sourceID, typeID)
		if key != "" {
			keys[key] = struct{}{}
		}
	}
	pools := map[string][]rankCand{}
	for i := range idx.Items {
		item := &idx.Items[i]
		if _, ok := keys[item.CategoryKey]; !ok || !item.HasPlaySource(sourceID) {
			continue
		}
		keepRank(pools, "k", rankFromMeta(item), sortType, limit)
	}
	return midsOf(pools["k"], sortType, limit)
}
