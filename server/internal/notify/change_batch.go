package notify

import (
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"fmt"
	"sort"
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
	mu    sync.Mutex
	id    string
	mids  []int64
	items []ChangeMidItem
	seen  map[int64]struct{}
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
		b.items = append(b.items, ChangeMidItem{Mid: mid, SourceName: sourceName})
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

// Items 复制批次内去重变更项（含 mid 与触发更新的源名称）。
func (b *ChangeBatch) Items() []ChangeMidItem {
	if b == nil {
		return nil
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	out := make([]ChangeMidItem, len(b.items))
	copy(out, b.items)
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

// FilmBatchSession 变更批次会话（保存于 Redis，TTL 48h；亦支持内存 fallback）
type FilmBatchSession struct {
	BatchID      string              `json:"batchId"`
	SiteName     string              `json:"siteName"`
	PageSize     int                 `json:"pageSize"`
	OverviewText string              `json:"overviewText"`
	Total        int                 `json:"total"`
	AllItems     []ChangeMidItem     `json:"allItems"`
	Cats         []CategoryCountItem `json:"cats"`
	CatMids      [][]int64           `json:"catMids"`
}

const (
	batchSessionTTL     = 48 * time.Hour
	maxMemBatchSessions = 50
)

type memSessionEntry struct {
	sess      FilmBatchSession
	expiresAt time.Time
}

var (
	memSessionMu sync.RWMutex
	memSessions  = make(map[string]memSessionEntry)
	memOrder     []string
)

func batchRedisKey(id string) string {
	return "EcoHub:NotifyBatch:" + id
}

// SaveChangeBatchSession 保存变更批次会话（Redis + 本地内存备份）
func SaveChangeBatchSession(sess FilmBatchSession) error {
	if sess.BatchID == "" {
		return fmt.Errorf("empty batch id")
	}
	now := time.Now()
	memSessionMu.Lock()
	// 清理已过期或已失效条目
	for len(memOrder) > 0 {
		oldestID := memOrder[0]
		entry, ok := memSessions[oldestID]
		if !ok || now.After(entry.expiresAt) {
			delete(memSessions, oldestID)
			memOrder = memOrder[1:]
			continue
		}
		break
	}
	// 超出容量上限淘汰最早会话
	for len(memOrder) >= maxMemBatchSessions {
		oldestID := memOrder[0]
		delete(memSessions, oldestID)
		memOrder = memOrder[1:]
	}
	if _, exists := memSessions[sess.BatchID]; !exists {
		memOrder = append(memOrder, sess.BatchID)
	}
	memSessions[sess.BatchID] = memSessionEntry{
		sess:      sess,
		expiresAt: now.Add(batchSessionTTL),
	}
	memSessionMu.Unlock()

	if db.Rdb != nil {
		raw, err := json.Marshal(sess)
		if err == nil {
			_ = db.Rdb.Set(db.Cxt, batchRedisKey(sess.BatchID), raw, batchSessionTTL).Err()
		}
	}
	return nil
}

func loadFilmBatchSession(batchID string) (FilmBatchSession, error) {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return FilmBatchSession{}, fmt.Errorf("empty batch id")
	}
	if db.Rdb != nil {
		data, err := db.Rdb.Get(db.Cxt, batchRedisKey(batchID)).Result()
		if err == nil {
			var sess FilmBatchSession
			if json.Unmarshal([]byte(data), &sess) == nil {
				return sess, nil
			}
		}
	}
	memSessionMu.RLock()
	entry, ok := memSessions[batchID]
	memSessionMu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.sess, nil
	}
	return FilmBatchSession{}, fmt.Errorf("session not found")
}

type filmSortMeta struct {
	pid         int64
	updateStamp int64
}

func loadFilmSortMeta(mids []int64) (map[int64]filmSortMeta, error) {
	out := make(map[int64]filmSortMeta, len(mids))
	if len(mids) == 0 || db.Mdb == nil {
		return out, nil
	}
	const chunk = 500
	for start := 0; start < len(mids); start += chunk {
		end := start + chunk
		if end > len(mids) {
			end = len(mids)
		}
		var rows []struct {
			Mid         int64         `gorm:"column:mid"`
			Pid         sql.NullInt64 `gorm:"column:pid"`
			UpdateStamp int64         `gorm:"column:update_stamp"`
		}
		err := db.Mdb.Table(model.TableFilmIndex).
			Select("mid, pid, update_stamp").
			Where("mid IN ?", mids[start:end]).
			Scan(&rows).Error
		if err != nil {
			return nil, err
		}
		for _, r := range rows {
			var pid int64
			if r.Pid.Valid {
				pid = r.Pid.Int64
			}
			out[r.Mid] = filmSortMeta{
				pid:         pid,
				updateStamp: r.UpdateStamp,
			}
		}
	}
	return out, nil
}

// BuildCategoryPlanForMids 针对变更项按首页大类聚合分类（支持其他），并按最新更新顺序（update_stamp DESC, mid DESC）对齐每日更新列表。
func BuildCategoryPlanForMids(all []ChangeMidItem) (cats []CategoryCountItem, catMids [][]int64, err error) {
	if len(all) == 0 {
		return nil, nil, nil
	}
	mids := make([]int64, 0, len(all))
	for _, it := range all {
		mids = append(mids, it.Mid)
	}
	metaByMid, err := loadFilmSortMeta(mids)
	if err != nil {
		return nil, nil, err
	}

	// 统一按最新更新顺序排序：update_stamp 降序；相同时 mid 降序（与 /api/dailyUpdates 排序规则完全一致）
	sort.SliceStable(all, func(i, j int) bool {
		metaI := metaByMid[all[i].Mid]
		metaJ := metaByMid[all[j].Mid]
		if metaI.updateStamp != metaJ.updateStamp {
			return metaI.updateStamp > metaJ.updateStamp
		}
		return all[i].Mid > all[j].Mid
	})

	nav := navTopCategories()
	navIDs := navTopCategoryIDs(nav)
	navSet := make(map[int64]struct{}, len(navIDs))
	for _, id := range navIDs {
		navSet[id] = struct{}{}
	}

	buckets := make(map[int64][]ChangeMidItem, len(nav)+1)
	var other []ChangeMidItem
	for _, it := range all {
		pid := metaByMid[it.Mid].pid
		if pid > 0 {
			if _, ok := navSet[pid]; ok {
				buckets[pid] = append(buckets[pid], it)
				continue
			}
		}
		other = append(other, it)
	}

	countByPid := make(map[int64]int, len(buckets))
	for pid, list := range buckets {
		countByPid[pid] = len(list)
	}
	cats = categoryCountsFromPidMap(countByPid, len(other))
	catMids = make([][]int64, len(cats))
	for i, c := range cats {
		var items []ChangeMidItem
		if c.CategoryName == "其他" {
			items = other
		} else {
			items = buckets[c.CategoryID]
		}
		ids := make([]int64, 0, len(items))
		for _, it := range items {
			ids = append(ids, it.Mid)
		}
		catMids[i] = ids
	}
	return cats, catMids, nil
}

func categoryCountsFromPidMap(countByPid map[int64]int, otherCount int) []CategoryCountItem {
	nav := navTopCategories()
	out := make([]CategoryCountItem, 0, len(nav)+1)
	for _, c := range nav {
		cnt := countByPid[c.Id]
		if cnt > 0 {
			out = append(out, CategoryCountItem{
				CategoryID:   c.Id,
				CategoryName: c.Name,
				Count:        cnt,
			})
		}
	}
	if otherCount > 0 {
		out = append(out, CategoryCountItem{
			CategoryID:   0,
			CategoryName: "其他",
			Count:        otherCount,
		})
	}
	return out
}

