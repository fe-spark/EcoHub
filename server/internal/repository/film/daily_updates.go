package film

import (
	"errors"
	"math/rand"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/support"
)

const (
	DailyPidAll   int64 = 0
	DailyPidOther int64 = -1
	// 随机池上限：近 24h 去重 mid 按时间取前 N 再洗牌，避免 ORDER BY RAND() filesort。
	dailyRandomPoolCap = 500
)

type dailyUpdateMidRow struct {
	Mid int64
}

// NavTopCategories 首页可见顶级大类（Show=true），与 /navCategory 同源（按当前生效采集站隔离）。
func NavTopCategories(sourceIdOpt ...string) ([]model.Category, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	var sourceID string
	if len(sourceIdOpt) > 0 {
		sourceID = strings.TrimSpace(sourceIdOpt[0])
	}
	if sourceID == "" {
		if active := repository.GetActiveCollectSource(); active != nil {
			sourceID = strings.TrimSpace(active.Id)
		}
	}
	var tree model.CategoryTree
	if sourceID != "" {
		var err error
		tree, err = repository.GetActiveCategoryTree(sourceID)
		if err != nil {
			return nil, err
		}
	}
	if len(tree.Children) == 0 {
		tree = repository.GetCategoryTree()
	}
	out := make([]model.Category, 0, len(tree.Children))
	for _, c := range tree.Children {
		if c == nil || !c.Show {
			continue
		}
		out = append(out, model.Category{
			Id:   c.Id,
			Pid:  c.Pid,
			Name: c.Name,
			Sort: c.Sort,
		})
	}
	return out, nil
}

// NavTopCategoryIDs 由 NavTopCategories 结果提取 ID 列表，供查询复用避免重复全表扫描分类树。
func NavTopCategoryIDs(nav []model.Category) []int64 {
	ids := make([]int64, 0, len(nav))
	for _, c := range nav {
		if c.Id > 0 {
			ids = append(ids, c.Id)
		}
	}
	return ids
}

// Rolling24hWindow 滚动近 24 小时：now-24h（含）到 now（含）。
func Rolling24hWindow(now time.Time) (from, to time.Time) {
	return now.Add(-24 * time.Hour), now
}

// DailyUpdateListQuery 近 24h 更新列表查询。
type DailyUpdateListQuery struct {
	From     time.Time
	To       time.Time
	Pid      int64
	Current  int
	PageSize int
	Random   bool
	Exclude  []int64
	NavIDs   []int64
	SourceId string
}

func sourceTopCategoryKeyGroups(sourceID string, navIDs []int64) (keyToRoot map[string]int64, allVisibleKeys []string, ok bool) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || db.Mdb == nil || len(repository.SourceRootTypeIDs(sourceID)) == 0 {
		return nil, nil, false
	}
	if len(navIDs) == 0 {
		navIDs = repository.SourceRootTypeIDs(sourceID)
	}
	keyToRoot = make(map[string]int64, len(navIDs)*4)
	allVisibleKeys = make([]string, 0, len(navIDs)*4)
	for _, rootID := range navIDs {
		if rootID <= 0 {
			continue
		}
		for _, typeID := range repository.PublicSourceTypeIDs(sourceID, "pid", rootID) {
			key := support.BuildSourceCategoryKey(sourceID, typeID)
			if key == "" {
				continue
			}
			if _, exists := keyToRoot[key]; !exists {
				keyToRoot[key] = rootID
				allVisibleKeys = append(allVisibleKeys, key)
			}
		}
	}
	return keyToRoot, allVisibleKeys, len(keyToRoot) > 0
}

type dailyNavIndex struct {
	useSourceKeys bool
	keyToRoot     map[string]int64
	navSet        map[int64]struct{}
}

func buildDailyNavIndex(sourceID string, navIDs []int64) dailyNavIndex {
	keyToRoot, _, ok := sourceTopCategoryKeyGroups(sourceID, navIDs)
	navSet := make(map[int64]struct{}, len(navIDs))
	for _, id := range navIDs {
		if id > 0 {
			navSet[id] = struct{}{}
		}
	}
	return dailyNavIndex{useSourceKeys: ok, keyToRoot: keyToRoot, navSet: navSet}
}

func (n dailyNavIndex) knownRoot(meta *filmsnapshot.FilmSearchMeta) (int64, bool) {
	if meta == nil {
		return 0, false
	}
	if !n.useSourceKeys {
		if meta.Pid > 0 {
			if _, ok := n.navSet[meta.Pid]; ok {
				return meta.Pid, true
			}
		}
		return 0, false
	}
	if r, ok := n.keyToRoot[strings.TrimSpace(meta.CategoryKey)]; ok && r > 0 {
		return r, true
	}
	if r, ok := n.keyToRoot[strings.TrimSpace(meta.RootCategoryKey)]; ok && r > 0 {
		return r, true
	}
	return 0, false
}

func (n dailyNavIndex) matches(meta *filmsnapshot.FilmSearchMeta, pid int64) bool {
	if n.useSourceKeys {
		switch pid {
		case DailyPidAll:
			return true
		case DailyPidOther:
			_, known := n.knownRoot(meta)
			return !known
		default:
			if r, ok := n.keyToRoot[strings.TrimSpace(meta.CategoryKey)]; ok && r == pid {
				return true
			}
			if r, ok := n.keyToRoot[strings.TrimSpace(meta.RootCategoryKey)]; ok && r == pid {
				return true
			}
			return false
		}
	}
	switch pid {
	case DailyPidAll:
		return true
	case DailyPidOther:
		_, known := n.knownRoot(meta)
		return !known
	default:
		return meta != nil && meta.Pid == pid
	}
}

func dailyMetaStop(meta *filmsnapshot.FilmSearchMeta, sourceID string, fromUnix, toUnix int64) (skip bool, stop bool) {
	stamp := meta.Item.UpdateStamp
	if stamp > toUnix {
		return true, false
	}
	if stamp < fromUnix {
		return false, true
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID != "" && !meta.HasPlaySource(sourceID) {
		return true, false
	}
	return false, false
}

func clampDailyUpdatePage(current, pageSize int) (int, int) {
	if current <= 0 {
		current = 1
	}
	if pageSize <= 0 {
		pageSize = 21
	}
	if pageSize > 100 {
		pageSize = 100
	}
	return current, pageSize
}

// ListDailyUpdateMids 近 24h 变更 mid：标准分页或随机抽样。
func ListDailyUpdateMids(q DailyUpdateListQuery) (mids []int64, total int, err error) {
	if q.To.Before(q.From) {
		return []int64{}, 0, nil
	}
	current, pageSize := clampDailyUpdatePage(q.Current, q.PageSize)
	navIDs := q.NavIDs
	if q.Pid != DailyPidAll && len(navIDs) == 0 {
		nav, navErr := NavTopCategories(q.SourceId)
		if navErr != nil {
			return nil, 0, navErr
		}
		navIDs = NavTopCategoryIDs(nav)
	}
	nav := buildDailyNavIndex(q.SourceId, navIDs)
	exclude := map[int64]struct{}{}
	if q.Random {
		for _, id := range q.Exclude {
			if id > 0 {
				exclude[id] = struct{}{}
			}
		}
	}
	fromUnix, toUnix := q.From.Unix(), q.To.Unix()
	start := (current - 1) * pageSize
	pool := make([]int64, 0)
	err = filmsnapshot.VisitSearchMetasByUpdate(func(meta *filmsnapshot.FilmSearchMeta) bool {
		skip, stop := dailyMetaStop(meta, q.SourceId, fromUnix, toUnix)
		if stop {
			return false
		}
		if skip || meta.Mid <= 0 || !nav.matches(meta, q.Pid) {
			return true
		}
		if _, drop := exclude[meta.Mid]; drop {
			return true
		}
		total++
		if q.Random {
			if len(pool) < dailyRandomPoolCap {
				pool = append(pool, meta.Mid)
			}
			return true
		}
		if total > start && len(pool) < pageSize {
			pool = append(pool, meta.Mid)
		}
		return true
	})
	if err != nil {
		return nil, 0, err
	}
	if total == 0 {
		return []int64{}, 0, nil
	}
	if q.Random {
		rows := make([]dailyUpdateMidRow, len(pool))
		for i, mid := range pool {
			rows[i] = dailyUpdateMidRow{Mid: mid}
		}
		picked := pickRandomDailyUpdateRows(rows, pageSize)
		mids = make([]int64, 0, len(picked))
		for _, row := range picked {
			if row.Mid > 0 {
				mids = append(mids, row.Mid)
			}
		}
		return mids, total, nil
	}
	return pool, total, nil
}

func pickRandomDailyUpdateRows(rows []dailyUpdateMidRow, pageSize int) []dailyUpdateMidRow {
	if len(rows) == 0 {
		return []dailyUpdateMidRow{}
	}
	shuffled := append([]dailyUpdateMidRow(nil), rows...)
	rand.Shuffle(len(shuffled), func(i, j int) {
		shuffled[i], shuffled[j] = shuffled[j], shuffled[i]
	})
	if pageSize > 0 && len(shuffled) > pageSize {
		shuffled = shuffled[:pageSize]
	}
	return shuffled
}

// DailyUpdatePidCounts 近 24h 按导航大类聚合数量。
func DailyUpdatePidCounts(from, to time.Time, navIDs []int64, sourceID string) (countByPid map[int64]int, otherCount int, total int, err error) {
	countByPid = map[int64]int{}
	if to.Before(from) {
		return countByPid, 0, 0, nil
	}
	nav := buildDailyNavIndex(sourceID, navIDs)
	fromUnix, toUnix := from.Unix(), to.Unix()
	err = filmsnapshot.VisitSearchMetasByUpdate(func(meta *filmsnapshot.FilmSearchMeta) bool {
		skip, stop := dailyMetaStop(meta, sourceID, fromUnix, toUnix)
		if stop {
			return false
		}
		if skip || meta.Mid <= 0 {
			return true
		}
		total++
		if root, known := nav.knownRoot(meta); known {
			countByPid[root]++
		} else {
			otherCount++
		}
		return true
	})
	if err != nil {
		return map[int64]int{}, 0, 0, err
	}
	return countByPid, otherCount, total, nil
}

// ClampDailyUpdateExclude 限制随机排除列表长度，避免 NOT IN 占位符/包过大。
func ClampDailyUpdateExclude(exclude []int64, max int) []int64 {
	if max <= 0 || len(exclude) <= max {
		return exclude
	}
	return exclude[:max]
}
