package film

import (
	"database/sql"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"

	"gorm.io/gorm"
)

const (
	DailyPidAll   int64 = 0
	DailyPidOther int64 = -1
	// 随机池上限：近 24h 去重 mid 按时间取前 N 再洗牌，避免 ORDER BY RAND() filesort。
	dailyRandomPoolCap = 500
)

type dailyUpdateMidRow struct {
	Mid int64 `gorm:"column:mid"`
}

type dailyPidCountRow struct {
	Pid   sql.NullInt64 `gorm:"column:pid"`
	Count int           `gorm:"column:cnt"`
}

// CategoryCountItem 顶级分类计数项。
type CategoryCountItem struct {
	CategoryID   int64
	CategoryName string
	Count        int
}

// NavTopCategories 首页可见顶级大类（Show=true），与 /navCategory 同源。
func NavTopCategories() []model.Category {
	if db.Mdb == nil {
		return nil
	}
	tree := repository.GetCategoryTree()
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
	return out
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

// LoadChangeMidsBetween 汇总时间窗内的变更 mid，直接走 film_index 的 update_stamp 索引。
func LoadChangeMidsBetween(from, to time.Time, limit int) ([]int64, error) {
	if db.Mdb == nil {
		return nil, fmt.Errorf("数据库未就绪")
	}
	if to.Before(from) {
		return nil, nil
	}

	type row struct {
		Mid int64 `gorm:"column:mid"`
	}
	var rows []row
	q := db.Mdb.Table(model.TableFilmIndex).
		Select("mid").
		Where("update_stamp >= ? AND update_stamp <= ?", from.Unix(), to.Unix()).
		Order("update_stamp DESC, mid DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]int64, 0, len(rows))
	for _, r := range rows {
		if r.Mid > 0 {
			out = append(out, r.Mid)
		}
	}
	return out, nil
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

func dailyPidFilter(pid int64) *CategoryCountItem {
	if pid == DailyPidAll {
		return nil
	}
	if pid == DailyPidOther {
		return &CategoryCountItem{CategoryID: 0, CategoryName: "其他"}
	}
	return &CategoryCountItem{CategoryID: pid, CategoryName: "_"}
}

func applyNavCategoryFilter(q *gorm.DB, cat *CategoryCountItem, navIDs []int64) *gorm.DB {
	if cat == nil {
		return q
	}
	name := strings.TrimSpace(cat.CategoryName)
	if name == "" || name == "全部" {
		return q
	}
	if name == "其他" {
		if len(navIDs) == 0 {
			return q
		}
		return q.Where("pid IS NULL OR pid = 0 OR pid NOT IN ?", navIDs)
	}
	if cat.CategoryID > 0 {
		return q.Where("pid = ?", cat.CategoryID)
	}
	return q.Where("1 = 0")
}

func dailyUpdateBaseQuery(from, to time.Time, pid int64, navIDs []int64, sourceID string) *gorm.DB {
	q := db.Mdb.Table(model.TableFilmIndex).
		Where("update_stamp >= ? AND update_stamp <= ?", from.Unix(), to.Unix())
	q = applyDailySourceMembership(q, sourceID)
	return applyNavCategoryFilter(q, dailyPidFilter(pid), navIDs)
}

func applyDailySourceMembership(q *gorm.DB, sourceID string) *gorm.DB {
	if strings.TrimSpace(sourceID) == "" {
		return q
	}
	return q.Where(model.FilmHasPlaySourceSQL(), strings.TrimSpace(sourceID), "play")
}

func applyDailyUpdateExclude(q *gorm.DB, random bool, exclude []int64) *gorm.DB {
	if !random || len(exclude) == 0 {
		return q
	}
	return q.Where("mid NOT IN ?", exclude)
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
	if db.Mdb == nil {
		return nil, 0, fmt.Errorf("数据库未就绪")
	}
	if q.To.Before(q.From) {
		return []int64{}, 0, nil
	}
	current, pageSize := clampDailyUpdatePage(q.Current, q.PageSize)
	navIDs := q.NavIDs
	if q.Pid != DailyPidAll && len(navIDs) == 0 {
		navIDs = NavTopCategoryIDs(NavTopCategories())
	}

	base := applyDailyUpdateExclude(dailyUpdateBaseQuery(q.From, q.To, q.Pid, navIDs, q.SourceId), q.Random, q.Exclude)
	var n int64
	if err = base.Select("COUNT(mid)").Scan(&n).Error; err != nil {
		return nil, 0, err
	}
	total = int(n)
	if total == 0 {
		return []int64{}, 0, nil
	}

	listQ := applyDailyUpdateExclude(dailyUpdateBaseQuery(q.From, q.To, q.Pid, navIDs, q.SourceId), q.Random, q.Exclude)
	listQ = listQ.Select("mid, update_stamp")

	var rows []dailyUpdateMidRow
	if q.Random {
		var pool []dailyUpdateMidRow
		if err = listQ.Order("update_stamp DESC, mid DESC").Limit(dailyRandomPoolCap).Scan(&pool).Error; err != nil {
			return nil, total, err
		}
		rows = pickRandomDailyUpdateRows(pool, pageSize)
	} else {
		offset := (current - 1) * pageSize
		if err = listQ.Order("update_stamp DESC, mid DESC").Offset(offset).Limit(pageSize).Scan(&rows).Error; err != nil {
			return nil, total, err
		}
	}
	mids = make([]int64, 0, len(rows))
	for _, r := range rows {
		if r.Mid > 0 {
			mids = append(mids, r.Mid)
		}
	}
	return mids, total, nil
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
	if db.Mdb == nil {
		return countByPid, 0, 0, fmt.Errorf("数据库未就绪")
	}
	if to.Before(from) {
		return countByPid, 0, 0, nil
	}

	navSet := make(map[int64]struct{}, len(navIDs))
	for _, id := range navIDs {
		navSet[id] = struct{}{}
	}

	var rows []dailyPidCountRow
	q := applyDailySourceMembership(db.Mdb.Table(model.TableFilmIndex).
		Select("pid AS pid, COUNT(mid) AS cnt").
		Where("update_stamp >= ? AND update_stamp <= ?", from.Unix(), to.Unix()), sourceID).
		Group("pid")
	if err = q.Scan(&rows).Error; err != nil {
		return countByPid, 0, 0, err
	}
	countByPid, otherCount, total = accumulateDailyPidCounts(rows, navSet)
	return countByPid, otherCount, total, nil
}

func accumulateDailyPidCounts(rows []dailyPidCountRow, navSet map[int64]struct{}) (countByPid map[int64]int, otherCount, total int) {
	countByPid = map[int64]int{}
	for _, r := range rows {
		if r.Count <= 0 {
			continue
		}
		total += r.Count
		if r.Pid.Valid && r.Pid.Int64 > 0 {
			if _, ok := navSet[r.Pid.Int64]; ok {
				countByPid[r.Pid.Int64] += r.Count
				continue
			}
		}
		otherCount += r.Count
	}
	return countByPid, otherCount, total
}

// ClampDailyUpdateExclude 限制随机排除列表长度，避免 NOT IN 占位符/包过大。
func ClampDailyUpdateExclude(exclude []int64, max int) []int64 {
	if max <= 0 || len(exclude) <= max {
		return exclude
	}
	return exclude[:max]
}
