package notify

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"

	"gorm.io/gorm"
)

// ChangeMidItem 包含 mid 与触发更新的源名称。
type ChangeMidItem struct {
	Mid        int64
	SourceName string
}

// ChangeBatch 一次采集的变更批次（纯内存追踪，无需 DB 临时表与全表轮询清理）。
type ChangeBatch struct {
	mu   sync.Mutex
	id   string
	mids []int64
	seen map[int64]struct{}
}

// StartChangeBatch 开启新批次。
func StartChangeBatch() *ChangeBatch {
	return &ChangeBatch{
		id:   newChangeBatchID(),
		seen: make(map[int64]struct{}),
	}
}

// ID 批次标识。
func (b *ChangeBatch) ID() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.id
}

// AppendMids 将 mid 写入批次（内存去重，可安全并发调用）。
func (b *ChangeBatch) AppendMids(sourceName string, mids ...int64) {
	if b == nil || len(mids) == 0 {
		return
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := b.seen[mid]; ok {
			continue
		}
		b.seen[mid] = struct{}{}
		b.mids = append(b.mids, mid)
	}
}

// Count 批次内去重 mid 数。
func (b *ChangeBatch) Count() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return len(b.mids)
}

// Mids 复制批次内去重 mid 列表。
func (b *ChangeBatch) Mids() []int64 {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]int64, len(b.mids))
	copy(out, b.mids)
	return out
}

func newChangeBatchID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano()%1e15)
	}
	return hex.EncodeToString(b[:])
}

// CountChangeMids 兼容接口。
func CountChangeMids(batchID string) int {
	return 0
}

// clampPageSize 限制翻页 size 到合法范围。
func clampPageSize(pageSize int) int {
	if pageSize <= 0 {
		return model.DefaultMaxFilmsInMessage
	}
	if pageSize > model.MaxFilmsInMessageCap {
		return model.MaxFilmsInMessageCap
	}
	return pageSize
}

// CategoryCountItem 按首页顶栏导航大类统计项。
type CategoryCountItem struct {
	CategoryID   int64  // 顶级分类 ID；「其他」为 0
	CategoryName string // 与首页顶栏 Name 一致
	Count        int
}

// navTopCategories 与 IndexService.GetNavCategory / 前端首页顶栏同源：
// GetCategoryTree().Children 中 Show=true 的顶级大类。
func navTopCategories() []model.Category {
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

func navTopCategoryIDs(nav []model.Category) []int64 {
	ids := make([]int64, 0, len(nav))
	for _, c := range nav {
		if c.Id > 0 {
			ids = append(ids, c.Id)
		}
	}
	return ids
}

// applyNavCategoryFilter 按顶级大类 pid 筛选。
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

// notifyCST 通知侧自然日边界。
func notifyCST() *time.Location {
	return time.FixedZone("CST", 8*3600)
}

// Rolling24hWindow 滚动近 24 小时：now-24h（含）到 now（含）。
func Rolling24hWindow(now time.Time) (from, to time.Time) {
	return now.Add(-24 * time.Hour), now
}

// LoadChangeMidsBetween 汇总时间窗内的变更 mid，直接走 film_index 的 update_stamp 索引。
func LoadChangeMidsBetween(from, to time.Time, limit int) ([]ChangeMidItem, error) {
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
	out := make([]ChangeMidItem, 0, len(rows))
	for _, r := range rows {
		if r.Mid > 0 {
			out = append(out, ChangeMidItem{Mid: r.Mid})
		}
	}
	return out, nil
}
