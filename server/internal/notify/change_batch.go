package notify

import (
	"crypto/rand"
	"encoding/hex"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const changeBatchTTL = 48 * time.Hour

// 批次加载错误分类：回调侧据此给出准确提示，避免一律「列表已过期」掩盖真实原因
// （批次不存在往往是多实例共用 Bot Token、回调被另一实例消费所致）。
var (
	ErrChangeBatchEmpty    = errors.New("变更批次为空")
	ErrChangeBatchNotFound = errors.New("变更批次不存在")
	ErrChangeBatchExpired  = errors.New("变更批次已过期")
)

var changeBatchMu sync.Mutex

// ChangeBatch 一次采集的变更批次（MySQL）。显式持有，随采集入口创建并沿调用链传递，
// 不依赖进程级全局状态，避免并发采集串批。
type ChangeBatch struct {
	mu sync.Mutex
	id string
}

// StartChangeBatch 开启新批次。DB 不可用时返回 nil，调用方按「无批次」降级。
// 落库与「是否推送采集摘要」解耦：首页/TG 每日更新依赖 mid 表，关通知时仍记录变更。
func StartChangeBatch() *ChangeBatch {
	if db.Mdb == nil {
		return nil
	}
	changeBatchMu.Lock()
	defer changeBatchMu.Unlock()
	purgeExpiredChangeBatches()
	id := newChangeBatchID()
	now := time.Now()
	rec := model.NotifyChangeBatch{
		ID:        id,
		PageSize:  15,
		CreatedAt: now,
		ExpireAt:  now.Add(changeBatchTTL),
	}
	if err := db.Mdb.Create(&rec).Error; err != nil {
		syslog.Errorf("[Notify] 创建变更批次失败: %v", err)
		return nil
	}
	return &ChangeBatch{id: id}
}

// ID 批次标识（未开启时为空）。
func (b *ChangeBatch) ID() string {
	if b == nil {
		return ""
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return b.id
}

// ChangeMidItem 包含 mid 与触发更新的源名称。
type ChangeMidItem struct {
	Mid        int64
	SourceName string
}

// AppendMids 将 mid 及其源名称写入批次（主键冲突时拼接源名称，可安全并发调用）。
func (b *ChangeBatch) AppendMids(sourceName string, mids ...int64) {
	if b == nil || len(mids) == 0 || db.Mdb == nil {
		return
	}
	sourceName = strings.TrimSpace(sourceName)
	b.mu.Lock()
	defer b.mu.Unlock()
	rows := make([]model.NotifyChangeMid, 0, len(mids))
	seen := make(map[int64]struct{}, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		rows = append(rows, model.NotifyChangeMid{BatchID: b.id, Mid: mid, SourceName: sourceName, CreatedAt: time.Now()})
	}
	if len(rows) == 0 {
		return
	}
	onConflict := clause.OnConflict{DoNothing: true}
	if sourceName != "" {
		onConflict = clause.OnConflict{
			Columns: []clause.Column{{Name: "batch_id"}, {Name: "mid"}},
			DoUpdates: clause.Assignments(map[string]any{
				// 用分隔串匹配，避免 FIND_IN_SET 对含逗号源名误判
				"source_name": gorm.Expr(
					"CASE WHEN source_name IS NULL OR source_name = '' THEN ? "+
						"WHEN CONCAT(', ', source_name, ', ') LIKE CONCAT('%, ', ?, ', %') THEN source_name "+
						"ELSE CONCAT(source_name, ', ', ?) END",
					sourceName, sourceName, sourceName,
				),
			}),
		}
	}
	if err := db.Mdb.Clauses(onConflict).CreateInBatches(rows, 200).Error; err != nil {
		syslog.Errorf("[Notify] 写入变更 mid 失败 batch=%s: %v", b.id, err)
	}
}

// Count 批次内去重 mid 数。
func (b *ChangeBatch) Count() int {
	if b == nil {
		return 0
	}
	b.mu.Lock()
	defer b.mu.Unlock()
	return CountChangeMids(b.id)
}

func newChangeBatchID() string {
	var b [8]byte
	if _, err := rand.Read(b[:]); err != nil {
		return fmt.Sprintf("%d", time.Now().UnixNano()%1e15)
	}
	return hex.EncodeToString(b[:])
}

// CountChangeMids 批次内 mid 数。
func CountChangeMids(batchID string) int {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" || db.Mdb == nil {
		return 0
	}
	var n int64
	_ = db.Mdb.Model(&model.NotifyChangeMid{}).Where("batch_id = ?", batchID).Count(&n).Error
	return int(n)
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

// CategoryCountItem 按首页顶栏导航大类统计项（与 /navCategory 一致）。
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

// applyNavCategoryFilter 按顶级大类 pid 筛选（与首页按 pid 归类一致）。
// cat 为空/全部：不筛选；「其他」：pid 为空或不属于任一导航大类。
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
			// 无导航大类时全部视为其他，不额外收窄
			return q
		}
		return q.Where("f.pid IS NULL OR f.pid = 0 OR f.pid NOT IN ?", navIDs)
	}
	if cat.CategoryID > 0 {
		return q.Where("f.pid = ?", cat.CategoryID)
	}
	// 未知分类名且无 ID：不匹配任何影片
	return q.Where("1 = 0")
}

// resolveCategoryFilter 将分类名解析为筛选项（优先统计列表 ID；兼容旧 callback 仅名称）。
func resolveCategoryFilter(batchID, category string) *CategoryCountItem {
	category = strings.TrimSpace(category)
	if category == "" || category == "全部" {
		return nil
	}
	if category == "其他" {
		return &CategoryCountItem{CategoryID: 0, CategoryName: "其他"}
	}
	// 先走本批次统计项（含 ID）
	for _, c := range GetChangeBatchCategoryCounts(batchID) {
		if c.CategoryName == category {
			item := c
			return &item
		}
	}
	// 再按导航名解析
	for _, n := range navTopCategories() {
		if n.Name == category {
			return &CategoryCountItem{CategoryID: n.Id, CategoryName: n.Name}
		}
	}
	return &CategoryCountItem{CategoryID: 0, CategoryName: category}
}

// GetChangeBatchCategoryCounts 按首页顶栏大类统计批次内影片数。
// 顺序与导航一致（非仅按数量倒序）；仅输出本批有片的大类，末尾可附加「其他」。
func GetChangeBatchCategoryCounts(batchID string) []CategoryCountItem {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" || db.Mdb == nil {
		return nil
	}
	navIDs := navTopCategoryIDs(navTopCategories())

	// 按 film_index.pid（写入时即为顶级大类 ID）聚合
	type pidRow struct {
		Pid   int64 `gorm:"column:pid"`
		Count int   `gorm:"column:cnt"`
	}
	var rows []pidRow
	q := db.Mdb.Table(model.TableNotifyChangeMid+" AS c").
		Select("f.pid AS pid, COUNT(DISTINCT c.mid) AS cnt").
		Joins("LEFT JOIN "+model.TableFilmIndex+" AS f ON f.mid = c.mid").
		Where("c.batch_id = ?", batchID).
		Group("f.pid")
	if err := q.Scan(&rows).Error; err != nil {
		syslog.Errorf("[Notify] 获取变更批次分类统计失败 batch=%s: %v", batchID, err)
		return nil
	}

	countByPid := make(map[int64]int, len(rows))
	otherCount := 0
	navSet := make(map[int64]struct{}, len(navIDs))
	for _, id := range navIDs {
		navSet[id] = struct{}{}
	}
	for _, r := range rows {
		if r.Count <= 0 {
			continue
		}
		if r.Pid > 0 {
			if _, ok := navSet[r.Pid]; ok {
				countByPid[r.Pid] += r.Count
				continue
			}
		}
		otherCount += r.Count
	}
	return categoryCountsFromPidMap(countByPid, otherCount)
}

// ResolveCategoryByIndex 按统计列表下标解析分类名；越界返回空（视为全部）。
func ResolveCategoryByIndex(batchID string, idx int) string {
	item := ResolveCategoryItemByIndex(batchID, idx)
	if item == nil {
		return ""
	}
	return item.CategoryName
}

// ResolveCategoryItemByIndex 按统计列表下标解析完整分类项（含顶级 ID）。
func ResolveCategoryItemByIndex(batchID string, idx int) *CategoryCountItem {
	if idx < 0 {
		return nil
	}
	cats := GetChangeBatchCategoryCounts(batchID)
	if idx >= len(cats) {
		return nil
	}
	item := cats[idx]
	return &item
}

// LoadChangeMidPageCategory 按导航大类筛选并分页取 mid 及其源名称。
// category 为空/"全部" 表示全部；名称须与首页顶栏一致，或为「其他」。
func LoadChangeMidPageCategory(batchID, category string, page, pageSize int) (chunk []ChangeMidItem, total, start, end, pageOut int, err error) {
	batchID = strings.TrimSpace(batchID)
	category = strings.TrimSpace(category)
	if batchID == "" {
		return nil, 0, 0, 0, 1, fmt.Errorf("empty batch")
	}
	pageSize = clampPageSize(pageSize)
	navIDs := navTopCategoryIDs(navTopCategories())
	filter := resolveCategoryFilter(batchID, category)

	if filter == nil {
		total = CountChangeMids(batchID)
	} else {
		var n int64
		countQuery := db.Mdb.Table(model.TableNotifyChangeMid+" AS c").
			Joins("LEFT JOIN "+model.TableFilmIndex+" AS f ON f.mid = c.mid").
			Where("c.batch_id = ?", batchID)
		countQuery = applyNavCategoryFilter(countQuery, filter, navIDs)
		_ = countQuery.Count(&n).Error
		total = int(n)
	}

	if total == 0 {
		return nil, 0, 0, 0, 1, nil
	}
	totalPages := (total + pageSize - 1) / pageSize
	if page < 1 {
		page = 1
	}
	if page > totalPages {
		page = totalPages
	}
	offset := (page - 1) * pageSize

	type row struct {
		Mid        int64
		SourceName string
	}
	var rows []row
	q := db.Mdb.Table(model.TableNotifyChangeMid+" AS c").
		Select("c.mid, c.source_name").
		Joins("LEFT JOIN "+model.TableFilmIndex+" AS f ON f.mid = c.mid").
		Where("c.batch_id = ?", batchID)
	q = applyNavCategoryFilter(q, filter, navIDs)

	q = q.Order("f.update_stamp DESC, c.mid DESC").Offset(offset).Limit(pageSize)
	if err = q.Scan(&rows).Error; err != nil {
		return nil, 0, 0, 0, page, err
	}
	chunk = make([]ChangeMidItem, 0, len(rows))
	for _, r := range rows {
		chunk = append(chunk, ChangeMidItem{Mid: r.Mid, SourceName: r.SourceName})
	}
	start = offset
	end = offset + len(chunk)
	return chunk, total, start, end, page, nil
}

// LoadChangeMidPage 按 update_stamp 新→旧分页取 mid 及其源名称（1-based page）。
func LoadChangeMidPage(batchID string, page, pageSize int) (chunk []ChangeMidItem, total, start, end, pageOut int, err error) {
	return LoadChangeMidPageCategory(batchID, "", page, pageSize)
}

// SaveChangeBatchMeta 写入概要文案与分页参数。
func SaveChangeBatchMeta(batchID, siteName, overview string, pageSize, total int) error {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return fmt.Errorf("empty batch")
	}
	pageSize = clampPageSize(pageSize)
	return db.Mdb.Model(&model.NotifyChangeBatch{}).Where("id = ?", batchID).Updates(map[string]any{
		"site_name": siteName,
		"overview":  overview,
		"page_size": pageSize,
		"total":     total,
	}).Error
}

// notifyCST 通知侧自然日边界（与消息时间展示一致）。
func notifyCST() *time.Location {
	return time.FixedZone("CST", 8*3600)
}

// Rolling24hWindow 滚动近 24 小时：now-24h（含）到 now（含）。
func Rolling24hWindow(now time.Time) (from, to time.Time) {
	return now.Add(-24 * time.Hour), now
}

// LoadChangeMidsBetween 汇总时间窗内各采集批次的变更 mid（与采集概要进列表逻辑同源）。
// 数据来自 notify_change_mid：采集写库时经 filterPlayStructureNotifyMIDs / 附属站「全库最大集数」判定后写入。
// 直接按 c.created_at 索引进行范围筛选，同 mid 跨批次去重，源名合并；按最新变更时间倒序。
// limit>0 时只取前 N 条（首页卡片）；limit<=0 不截断（TG 每日更新全窗）。
func LoadChangeMidsBetween(from, to time.Time, limit int) ([]ChangeMidItem, error) {
	if db.Mdb == nil {
		return nil, fmt.Errorf("数据库未就绪")
	}
	if to.Before(from) {
		return nil, nil
	}

	// 首页或小条数场景：直接走 (created_at, mid) 联合索引倒序流式提取，耗时 < 1ms，消除 MySQL 临时表与全量排序
	if limit > 0 && limit <= 500 {
		var rows []struct {
			Mid        int64
			SourceName string
		}
		scanLimit := limit * 3
		if scanLimit < 300 {
			scanLimit = 300
		}
		err := db.Mdb.Table(model.TableNotifyChangeMid).
			Select("mid, source_name").
			Where("created_at >= ? AND created_at <= ?", from, to).
			Order("created_at DESC, mid DESC").
			Limit(scanLimit).
			Scan(&rows).Error
		if err != nil {
			return nil, err
		}

		seen := make(map[int64]struct{}, limit)
		out := make([]ChangeMidItem, 0, limit)
		for _, r := range rows {
			if r.Mid <= 0 {
				continue
			}
			if _, ok := seen[r.Mid]; ok {
				continue
			}
			seen[r.Mid] = struct{}{}
			out = append(out, ChangeMidItem{Mid: r.Mid, SourceName: strings.TrimSpace(r.SourceName)})
			if len(out) >= limit {
				break
			}
		}
		return out, nil
	}

	type row struct {
		Mid        int64
		SourceName string
	}
	var rows []row
	q := db.Mdb.Table(model.TableNotifyChangeMid).
		Select("mid, GROUP_CONCAT(DISTINCT NULLIF(TRIM(source_name), '') ORDER BY source_name SEPARATOR ', ') AS source_name, MAX(created_at) AS latest_time").
		Where("created_at >= ? AND created_at <= ?", from, to).
		Group("mid").
		Order("latest_time DESC, mid DESC")
	if limit > 0 {
		q = q.Limit(limit)
	}
	if err := q.Scan(&rows).Error; err != nil {
		return nil, err
	}
	out := make([]ChangeMidItem, 0, len(rows))
	for _, r := range rows {
		if r.Mid <= 0 {
			continue
		}
		out = append(out, ChangeMidItem{Mid: r.Mid, SourceName: strings.TrimSpace(r.SourceName)})
	}
	return out, nil
}

// categoryCountsFromPidMap 导航大类顺序聚合 pid→count（与 GetChangeBatchCategoryCounts 一致）。
func categoryCountsFromPidMap(countByPid map[int64]int, otherCount int) []CategoryCountItem {
	nav := navTopCategories()
	res := make([]CategoryCountItem, 0, len(nav)+1)
	for _, n := range nav {
		if cnt := countByPid[n.Id]; cnt > 0 {
			res = append(res, CategoryCountItem{
				CategoryID:   n.Id,
				CategoryName: n.Name,
				Count:        cnt,
			})
		}
	}
	if otherCount > 0 {
		res = append(res, CategoryCountItem{
			CategoryID:   0,
			CategoryName: "其他",
			Count:        otherCount,
		})
	}
	return res
}

// loadFilmPids 批量 mid → 顶级分类 pid。查询失败返回 error，避免把残缺结果打进「其他」。
func loadFilmPids(mids []int64) (map[int64]int64, error) {
	out := make(map[int64]int64, len(mids))
	if len(mids) == 0 || db.Mdb == nil {
		return out, nil
	}
	uniq := make([]int64, 0, len(mids))
	seen := make(map[int64]struct{}, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		uniq = append(uniq, mid)
	}
	const chunk = 200
	for start := 0; start < len(uniq); start += chunk {
		end := start + chunk
		if end > len(uniq) {
			end = len(uniq)
		}
		var rows []struct {
			Mid int64
			Pid int64
		}
		if err := db.Mdb.Table(model.TableFilmIndex).
			Select("mid, pid").
			Where("mid IN ?", uniq[start:end]).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			out[r.Mid] = r.Pid
		}
	}
	return out, nil
}

// BuildCategoryPlanForMids 按首页导航大类划分 mid（顺序/「其他」规则与采集概要分类一致）。
// 返回有片的分类列表，以及与之对齐的各分类 mid 子集（仍保持 all 中的相对顺序）。
func BuildCategoryPlanForMids(all []ChangeMidItem) (cats []CategoryCountItem, catMids [][]int64, err error) {
	if len(all) == 0 {
		return nil, nil, nil
	}
	mids := make([]int64, 0, len(all))
	for _, it := range all {
		mids = append(mids, it.Mid)
	}
	pidByMid, err := loadFilmPids(mids)
	if err != nil {
		return nil, nil, err
	}
	nav := navTopCategories()
	navIDs := navTopCategoryIDs(nav)
	navSet := make(map[int64]struct{}, len(navIDs))
	for _, id := range navIDs {
		navSet[id] = struct{}{}
	}

	// 先按导航大类分桶，保持 all 的排序
	buckets := make(map[int64][]ChangeMidItem, len(nav)+1)
	var other []ChangeMidItem
	for _, it := range all {
		pid := pidByMid[it.Mid]
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

// LoadChangeBatch 加载批次元数据。
// 先按 id 查行，再在 Go 端判过期（避免 SQL 端 time.Time 参数与 datetime 列的时区/精度比较差异），
// 错误信息携带 expire_at 便于回调侧日志定位「刚收到就过期」类问题。
func LoadChangeBatch(batchID string) (model.NotifyChangeBatch, error) {
	var rec model.NotifyChangeBatch
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return rec, ErrChangeBatchEmpty
	}
	err := db.Mdb.Where("id = ?", batchID).First(&rec).Error
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return rec, ErrChangeBatchNotFound
		}
		return rec, err
	}
	if rec.ExpireAt.Before(time.Now()) {
		return rec, fmt.Errorf("%w id=%s expire_at=%s", ErrChangeBatchExpired, batchID,
			rec.ExpireAt.In(time.FixedZone("CST", 8*3600)).Format(time.DateTime))
	}
	if rec.PageSize <= 0 {
		rec.PageSize = 15
	}
	return rec, nil
}

func purgeExpiredChangeBatches() {
	if db.Mdb == nil {
		return
	}
	now := time.Now()
	const batchLimit = 100
	// 多轮清理，避免过期批次堆积；单次 Start 最多 20 轮
	for round := 0; round < 20; round++ {
		var expired []model.NotifyChangeBatch
		if err := db.Mdb.Where("expire_at < ?", now).Limit(batchLimit).Find(&expired).Error; err != nil || len(expired) == 0 {
			return
		}
		ids := make([]string, 0, len(expired))
		oldest, newest := expired[0].ExpireAt, expired[0].ExpireAt
		for _, e := range expired {
			ids = append(ids, e.ID)
			if e.ExpireAt.Before(oldest) {
				oldest = e.ExpireAt
			}
			if e.ExpireAt.After(newest) {
				newest = e.ExpireAt
			}
		}
		log.Printf("[Notify] 清理过期变更批次 count=%d expire_range=[%s ~ %s]",
			len(expired),
			oldest.In(time.FixedZone("CST", 8*3600)).Format(time.DateTime),
			newest.In(time.FixedZone("CST", 8*3600)).Format(time.DateTime))
		_ = db.Mdb.Where("batch_id IN ?", ids).Delete(&model.NotifyChangeMid{}).Error
		_ = db.Mdb.Where("id IN ?", ids).Delete(&model.NotifyChangeBatch{}).Error
		if len(expired) < batchLimit {
			return
		}
	}
}
