package notify

import (
	"crypto/rand"
	"encoding/hex"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"

	"gorm.io/gorm/clause"
)

const changeBatchTTL = 48 * time.Hour

var changeBatchMu sync.Mutex

// ChangeBatch 一次采集的变更批次（MySQL）。显式持有，随采集入口创建并沿调用链传递，
// 不依赖进程级全局状态，避免并发采集串批。
type ChangeBatch struct {
	mu sync.Mutex
	id string
}

// StartChangeBatch 开启新批次。DB 不可用或通知关闭等异常时返回 nil，调用方按「无批次」降级。
func StartChangeBatch() *ChangeBatch {
	if db.Mdb == nil {
		return nil
	}
	changeBatchMu.Lock()
	defer changeBatchMu.Unlock()
	purgeExpiredChangeBatches()
	// 通知关闭时短路：不创建批次，避免采集热路径向 MySQL 写入无人消费的变更 mid
	if !IsEventEnabled(model.NotifyEventCollectBatchSummary) {
		return nil
	}
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

// AppendMids 将 mid 写入批次（主键冲突忽略，可安全并发调用）。
func (b *ChangeBatch) AppendMids(mids ...int64) {
	if b == nil || len(mids) == 0 || db.Mdb == nil {
		return
	}
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
		rows = append(rows, model.NotifyChangeMid{BatchID: b.id, Mid: mid})
	}
	if len(rows) == 0 {
		return
	}
	if err := db.Mdb.Clauses(clause.OnConflict{DoNothing: true}).CreateInBatches(rows, 200).Error; err != nil {
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

// LoadChangeMidPage 按 update_stamp 新→旧分页取 mid（1-based page）。
func LoadChangeMidPage(batchID string, page, pageSize int) (chunk []int64, total, start, end, pageOut int, err error) {
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return nil, 0, 0, 0, 1, fmt.Errorf("empty batch")
	}
	pageSize = clampPageSize(pageSize)
	total = CountChangeMids(batchID)
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

	// 关联 film_index 按更新时间排序；无索引时 mid 倒序兜底
	type row struct {
		Mid int64
	}
	var rows []row
	q := db.Mdb.Table(model.TableNotifyChangeMid+" AS c").
		Select("c.mid").
		Joins("LEFT JOIN "+model.TableFilmIndex+" AS f ON f.mid = c.mid").
		Where("c.batch_id = ?", batchID).
		Order("f.update_stamp DESC, c.mid DESC").
		Offset(offset).Limit(pageSize)
	if err = q.Scan(&rows).Error; err != nil {
		return nil, 0, 0, 0, page, err
	}
	chunk = make([]int64, 0, len(rows))
	for _, r := range rows {
		chunk = append(chunk, r.Mid)
	}
	start = offset
	end = offset + len(chunk)
	return chunk, total, start, end, page, nil
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

// LoadChangeBatch 加载批次元数据。
// 先按 id 查行，再在 Go 端判过期（避免 SQL 端 time.Time 参数与 datetime 列的时区/精度比较差异），
// 错误信息携带 expire_at 便于回调侧日志定位「刚收到就过期」类问题。
func LoadChangeBatch(batchID string) (model.NotifyChangeBatch, error) {
	var rec model.NotifyChangeBatch
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return rec, fmt.Errorf("empty batch")
	}
	err := db.Mdb.Where("id = ?", batchID).First(&rec).Error
	if err != nil {
		return rec, err
	}
	if rec.ExpireAt.Before(time.Now()) {
		return rec, fmt.Errorf("批次已过期 id=%s expire_at=%s", batchID,
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
