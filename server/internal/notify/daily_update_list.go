package notify

import (
	"database/sql"
	"fmt"
	"math/rand"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

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

// NavTopCategories 首页可见顶级大类（Show=true），与 /navCategory 同源。
func NavTopCategories() []model.Category {
	return navTopCategories()
}

// NavTopCategoryIDs 由 NavTopCategories 结果提取 ID 列表，供查询复用避免重复全表扫描分类树。
func NavTopCategoryIDs(nav []model.Category) []int64 {
	return navTopCategoryIDs(nav)
}

// DailyUpdateListQuery 近 24h 更新列表查询。
// Pid: 0 全部；-1 其他（非导航大类）；>0 导航大类 pid。
// NavIDs 由调用方预取的导航大类 ID（pid=0 时可不传）。
type DailyUpdateListQuery struct {
	From     time.Time
	To       time.Time
	Pid      int64
	Current  int
	PageSize int
	Random   bool
	Exclude  []int64
	NavIDs   []int64
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

func dailyUpdateBaseQuery(from, to time.Time, pid int64, navIDs []int64) *gorm.DB {
	q := db.Mdb.Table(model.TableNotifyChangeMid+" AS c").
		Where("c.created_at >= ? AND c.created_at <= ?", from, to)
	if pid != DailyPidAll {
		q = q.Joins("LEFT JOIN " + model.TableFilmIndex + " AS f ON f.mid = c.mid")
	}
	return applyNavCategoryFilter(q, dailyPidFilter(pid), navIDs)
}

func applyDailyUpdateExclude(q *gorm.DB, random bool, exclude []int64) *gorm.DB {
	if !random || len(exclude) == 0 {
		return q
	}
	return q.Where("c.mid NOT IN ?", exclude)
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
// 随机时忽略 offset，按排除列表抽一页；total 为筛选后（随机时再扣 exclude）可抽数量。
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
		navIDs = navTopCategoryIDs(navTopCategories())
	}

	base := applyDailyUpdateExclude(dailyUpdateBaseQuery(q.From, q.To, q.Pid, navIDs), q.Random, q.Exclude)
	var n int64
	if err = base.Select("COUNT(DISTINCT c.mid)").Scan(&n).Error; err != nil {
		return nil, 0, err
	}
	total = int(n)
	if total == 0 {
		return []int64{}, 0, nil
	}

	listQ := applyDailyUpdateExclude(dailyUpdateBaseQuery(q.From, q.To, q.Pid, navIDs), q.Random, q.Exclude)
	listQ = listQ.Select("c.mid AS mid, MAX(c.created_at) AS latest_time").Group("c.mid")

	var rows []dailyUpdateMidRow
	if q.Random {
		var pool []dailyUpdateMidRow
		if err = listQ.Order("latest_time DESC, c.mid DESC").Limit(dailyRandomPoolCap).Scan(&pool).Error; err != nil {
			return nil, total, err
		}
		rows = pickRandomDailyUpdateRows(pool, pageSize)
	} else {
		offset := (current - 1) * pageSize
		if err = listQ.Order("latest_time DESC, c.mid DESC").Offset(offset).Limit(pageSize).Scan(&rows).Error; err != nil {
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

// DailyUpdatePidCounts 近 24h 按导航大类聚合数量。navIDs 由调用方预取，避免重复全表扫描分类树。
// 孤儿 mid（film_index 缺失 / f.pid NULL）计入 total 与 otherCount，与「其他」列表口径一致。
func DailyUpdatePidCounts(from, to time.Time, navIDs []int64) (countByPid map[int64]int, otherCount int, total int, err error) {
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
	q := db.Mdb.Table(model.TableNotifyChangeMid+" AS c").
		Select("f.pid AS pid, COUNT(DISTINCT c.mid) AS cnt").
		Joins("LEFT JOIN "+model.TableFilmIndex+" AS f ON f.mid = c.mid").
		Where("c.created_at >= ? AND c.created_at <= ?", from, to).
		Group("f.pid")
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
