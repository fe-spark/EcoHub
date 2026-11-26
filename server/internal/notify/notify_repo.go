package notify

import (
	"context"
	"crypto/sha1"
	"database/sql"
	"encoding/hex"
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/sourcecatalog"

	"gorm.io/gorm"
)

const (
	notifyBatchSessionTTL = 24 * time.Hour
	maxMemNotifySessions  = 64
)

type notifyMemSession struct {
	sess      model.NotifyBatchSession
	expiresAt time.Time
}

var (
	notifySessionMu    sync.RWMutex
	notifyMemSessions  = make(map[string]notifyMemSession)
	notifySessionOrder []string
)

// DefaultNotifyConfig 返回默认 Telegram 通知配置
func DefaultNotifyConfig() model.NotifyConfig {
	return model.NotifyConfig{
		Enabled:  false,
		BotToken: "",
		ChatIDs:  []string{},
		Events: model.NotifyEventSwitches{
			CollectBatchSummary: true,
			CollectSourceFailed: true,
			CronTaskFailed:      true,
			SourceOrderChange:   true,
		},
		OnlyNotifyOnUpdate: true,
		MaxFilmsInMessage:  model.DefaultNotifyPageSize,
	}
}

// NormalizeNotifyConfig 规范化 Telegram 通知配置
func NormalizeNotifyConfig(cfg model.NotifyConfig) model.NotifyConfig {
	cfg.BotToken = strings.TrimSpace(cfg.BotToken)
	seen := make(map[string]struct{}, len(cfg.ChatIDs))
	cleaned := make([]string, 0, len(cfg.ChatIDs))
	for _, raw := range cfg.ChatIDs {
		id := strings.TrimSpace(raw)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleaned = append(cleaned, id)
	}
	cfg.ChatIDs = cleaned
	if cfg.MaxFilmsInMessage <= 0 {
		cfg.MaxFilmsInMessage = model.DefaultNotifyPageSize
	} else if cfg.MaxFilmsInMessage > model.MaxNotifyPageSize {
		cfg.MaxFilmsInMessage = model.MaxNotifyPageSize
	}
	return cfg
}

// MaskBotToken 对 BotToken 进行脱敏展示
func MaskBotToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	r := []rune(token)
	n := len(r)
	if n <= 8 {
		return strings.Repeat("*", n)
	}
	return string(r[:4]) + "***" + string(r[n-4:])
}

// IsMaskedBotToken 判断是否为脱敏后的占位 Token
func IsMaskedBotToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if strings.Trim(token, "*") == "" {
		return true
	}
	if len([]rune(token)) > 8 && strings.Contains(token, "***") {
		parts := strings.Split(token, "***")
		if len(parts) == 2 && len([]rune(parts[0])) == 4 && len([]rune(parts[1])) == 4 {
			return true
		}
	}
	return false
}

// PublicNotifyConfig 返回前端展示的脱敏配置
func PublicNotifyConfig(cfg model.NotifyConfig) model.NotifyConfig {
	cfg = NormalizeNotifyConfig(cfg)
	cfg.BotToken = MaskBotToken(cfg.BotToken)
	return cfg
}

// GetNotifyConfig 读取 Telegram 通知配置。没有记录时返回默认值，不写入缓存。
func GetNotifyConfig() (model.NotifyConfig, error) {
	return repobase.LoadCachedConfig(
		config.NotifyConfigKey,
		decodeNotifyConfig,
		func() (model.NotifyConfig, string, bool, error) {
			if db.Mdb == nil {
				return model.NotifyConfig{}, "", false, errors.New("数据库未连接")
			}
			var rec model.NotifyConfigRecord
			if err := db.Mdb.Order("id DESC").First(&rec).Error; err != nil {
				if repobase.RecordNotFound(err) {
					return DefaultNotifyConfig(), "", false, nil
				}
				return model.NotifyConfig{}, "", false, err
			}
			cfg, err := decodeNotifyConfig(rec.Payload)
			if err != nil {
				return model.NotifyConfig{}, "", false, err
			}
			data, err := json.Marshal(cfg)
			if err != nil {
				return model.NotifyConfig{}, "", false, err
			}
			return cfg, string(data), true, nil
		},
	)
}

func decodeNotifyConfig(raw string) (model.NotifyConfig, error) {
	if strings.TrimSpace(raw) == "" {
		return DefaultNotifyConfig(), nil
	}
	var cfg model.NotifyConfig
	if err := json.Unmarshal([]byte(raw), &cfg); err != nil {
		return model.NotifyConfig{}, fmt.Errorf("通知配置无法解析: %w", err)
	}
	return NormalizeNotifyConfig(cfg), nil
}

// SaveNotifyConfig 持久化 Telegram 通知配置并刷新 Redis 缓存
func SaveNotifyConfig(cfg model.NotifyConfig) error {
	cfg = NormalizeNotifyConfig(cfg)
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	raw, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	rec := model.NotifyConfigRecord{Payload: string(raw)}
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.NotifyConfigRecord{}).Error; err != nil {
			return err
		}
		return tx.Create(&rec).Error
	}); err != nil {
		return err
	}
	return cacheNotifyConfig(cfg)
}

func cacheNotifyConfig(cfg model.NotifyConfig) error {
	data, err := json.Marshal(cfg)
	if err != nil {
		return err
	}
	return repobase.WriteConfigCache(config.NotifyConfigKey, string(data))
}

// SaveNotifyBatchSession 保存批次翻页会话（内存 + Redis 双写，TTL 24h）
func SaveNotifyBatchSession(sess model.NotifyBatchSession) error {
	sess.BatchID = strings.TrimSpace(sess.BatchID)
	if sess.BatchID == "" {
		return errors.New("empty batch id")
	}
	rememberNotifyMemSession(sess, time.Now().Add(notifyBatchSessionTTL))
	if db.Rdb != nil {
		if raw, err := json.Marshal(sess); err == nil {
			key := config.NotifyBatchCachePrefix + ":" + sess.BatchID
			if setErr := db.Rdb.Set(db.Cxt, key, raw, notifyBatchSessionTTL).Err(); setErr != nil {
				syslog.Warnf("[Notify] 写入批次会话缓存失败 batch=%s err=%v", sess.BatchID, setErr)
			}
		} else {
			syslog.Warnf("[Notify] 序列化批次会话失败 batch=%s err=%v", sess.BatchID, err)
		}
	}
	return nil
}

// LoadNotifyBatchSession 加载批次翻页会话（内存优先，Redis 兜底）
func LoadNotifyBatchSession(batchID string) (model.NotifyBatchSession, error) {
	return LoadNotifyBatchSessionContext(db.Cxt, batchID)
}

// LoadNotifyBatchSessionContext 加载批次翻页会话，支持上下文取消。
func LoadNotifyBatchSessionContext(ctx context.Context, batchID string) (model.NotifyBatchSession, error) {
	if ctx == nil {
		ctx = db.Cxt
	}
	batchID = strings.TrimSpace(batchID)
	if batchID == "" {
		return model.NotifyBatchSession{}, errors.New("empty batch id")
	}
	notifySessionMu.RLock()
	entry, ok := notifyMemSessions[batchID]
	notifySessionMu.RUnlock()
	if ok && time.Now().Before(entry.expiresAt) {
		return entry.sess, nil
	}
	if db.Rdb != nil {
		key := config.NotifyBatchCachePrefix + ":" + batchID
		if data, err := db.Rdb.Get(ctx, key).Result(); err == nil && data != "" {
			var sess model.NotifyBatchSession
			if json.Unmarshal([]byte(data), &sess) == nil {
				ttl := notifyBatchSessionTTL
				if rem, err := db.Rdb.TTL(ctx, key).Result(); err == nil && rem > 0 {
					ttl = rem
				}
				rememberNotifyMemSession(sess, time.Now().Add(ttl))
				return sess, nil
			}
		}
	}
	return model.NotifyBatchSession{}, fmt.Errorf("session %s not found", batchID)
}

func rememberNotifyMemSession(sess model.NotifyBatchSession, expiresAt time.Time) {
	now := time.Now()
	notifySessionMu.Lock()
	defer notifySessionMu.Unlock()
	// 更新既有会话时先移除旧槽位，保持 notifySessionOrder 与 expiresAt 单调递增一致
	if _, exists := notifyMemSessions[sess.BatchID]; exists {
		for i, id := range notifySessionOrder {
			if id == sess.BatchID {
				notifySessionOrder = append(notifySessionOrder[:i], notifySessionOrder[i+1:]...)
				break
			}
		}
	}
	for len(notifySessionOrder) > 0 {
		oldest := notifySessionOrder[0]
		e, ok := notifyMemSessions[oldest]
		if !ok || now.After(e.expiresAt) {
			delete(notifyMemSessions, oldest)
			notifySessionOrder = notifySessionOrder[1:]
			continue
		}
		break
	}
	for len(notifySessionOrder) >= maxMemNotifySessions {
		oldest := notifySessionOrder[0]
		delete(notifyMemSessions, oldest)
		notifySessionOrder = notifySessionOrder[1:]
	}
	notifySessionOrder = append(notifySessionOrder, sess.BatchID)
	notifyMemSessions[sess.BatchID] = notifyMemSession{sess: sess, expiresAt: expiresAt}
}

// BatchSourceCategoryStatRow 表示按站点与影片分类字段聚合后的计数行
type BatchSourceCategoryStatRow struct {
	SourceID        string
	FirstSourceId   string
	Pid             int64
	Cid             int64
	CategoryKey     string
	RootCategoryKey string
	Count           int
}

// BatchSourceCategoryPageQuery 定义账本批次下站点分类分页 SQL 查询条件
type BatchSourceCategoryPageQuery struct {
	BatchID         string
	SourceID        string
	CategoryAll     bool
	IsOther         bool
	UseCategoryKeys bool
	IncludeKeys     []string
	KnownKeys       []string
	IncludePids     []int64
	KnownPids       []int64
	Limit           int
	Offset          int
}

// HasCollectBatchLedger 检查批次是否已写入 collect_record_films 账本
func HasCollectBatchLedger(batchID string) (bool, error) {
	return sourcecatalog.CollectRecordHasLedger(batchID)
}

func filmIndexJoinClause(conn *gorm.DB) string {
	if conn != nil && conn.Dialector != nil && conn.Dialector.Name() == "mysql" {
		return "STRAIGHT_JOIN " + model.TableFilmIndex + " AS fi ON fi.mid = crf.mid"
	}
	return "JOIN " + model.TableFilmIndex + " AS fi ON fi.mid = crf.mid"
}

const notifyPageCacheTTL = 30 * time.Minute

func notifySourcePageCacheKey(q BatchSourceCategoryPageQuery) string {
	raw := fmt.Sprintf(
		"%s|%s|%t|%t|%t|%s|%s|%v|%v|%d|%d",
		strings.TrimSpace(q.BatchID),
		strings.TrimSpace(q.SourceID),
		q.CategoryAll,
		q.IsOther,
		q.UseCategoryKeys,
		strings.Join(q.IncludeKeys, ","),
		strings.Join(q.KnownKeys, ","),
		q.IncludePids,
		q.KnownPids,
		q.Limit,
		q.Offset,
	)
	sum := sha1.Sum([]byte(raw))
	return config.NotifyBatchCachePrefix + ":pg:" + hex.EncodeToString(sum[:12])
}

var loadBatchSourceCategoryStatsFn = LoadBatchSourceCategoryStatsForSource

// LoadBatchSourceCategoryStatsForSource 基于 collect_record_films 账本按单站条件聚合分类计数，沿聚簇主键 (record_id, source_id, mid) 顺序扫描。
func LoadBatchSourceCategoryStatsForSource(ctx context.Context, batchID, sourceID string) ([]BatchSourceCategoryStatRow, error) {
	batchID = strings.TrimSpace(batchID)
	sourceID = strings.TrimSpace(sourceID)
	if batchID == "" || sourceID == "" || db.Mdb == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	recordID, ready, err := sourcecatalog.ResolveCollectRecordLedgerContext(ctx, batchID)
	if err != nil || !ready || recordID == 0 {
		return nil, err
	}
	type statRaw struct {
		FirstSourceId   sql.NullString
		Pid             sql.NullInt64
		Cid             sql.NullInt64
		CategoryKey     sql.NullString
		RootCategoryKey sql.NullString
		Count           int64
	}
	var rows []statRaw
	err = db.Mdb.WithContext(ctx).Table("collect_record_films AS crf").
		Select("fi.first_source_id, fi.pid, fi.cid, fi.category_key, fi.root_category_key, COUNT(*) AS count").
		Joins(filmIndexJoinClause(db.Mdb)).
		Where("crf.record_id = ? AND crf.source_id = ? AND fi.deleted_at IS NULL", recordID, sourceID).
		Group("fi.first_source_id, fi.pid, fi.cid, fi.category_key, fi.root_category_key").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]BatchSourceCategoryStatRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, BatchSourceCategoryStatRow{
			SourceID:        sourceID,
			FirstSourceId:   strings.TrimSpace(r.FirstSourceId.String),
			Pid:             r.Pid.Int64,
			Cid:             r.Cid.Int64,
			CategoryKey:     strings.TrimSpace(r.CategoryKey.String),
			RootCategoryKey: strings.TrimSpace(r.RootCategoryKey.String),
			Count:           int(r.Count),
		})
	}
	return out, nil
}

// LoadBatchSourceCategoryPage 基于 collect_record_films 账本按需查询单页通知影片（对齐采集记录，按账本 id ASC 索引顺序单页提取后再回表补片名，避免跨表 filesort）。
func LoadBatchSourceCategoryPage(ctx context.Context, q BatchSourceCategoryPageQuery) ([]model.NotifyFilmItem, error) {
	batchID := strings.TrimSpace(q.BatchID)
	sourceID := strings.TrimSpace(q.SourceID)
	if batchID == "" || sourceID == "" || db.Mdb == nil || q.Limit <= 0 {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	offset := q.Offset
	if offset < 0 {
		offset = 0
	}
	q.Offset = offset

	cacheKey := notifySourcePageCacheKey(q)
	if db.Rdb != nil {
		if raw, err := db.Rdb.Get(ctx, cacheKey).Result(); err == nil && raw != "" {
			var cached []model.NotifyFilmItem
			if json.Unmarshal([]byte(raw), &cached) == nil {
				return cached, nil
			}
		} else if err != nil && ctx.Err() != nil {
			return nil, ctx.Err()
		}
	}

	recordID, ready, err := sourcecatalog.ResolveCollectRecordLedgerContext(ctx, batchID)
	if err != nil {
		return nil, err
	}
	if !ready || recordID == 0 {
		return []model.NotifyFilmItem{}, nil
	}

	var mids []int64
	query := db.Mdb.WithContext(ctx).Table("collect_record_films AS crf").
		Joins(filmIndexJoinClause(db.Mdb)).
		Where("crf.record_id = ? AND crf.source_id = ? AND fi.deleted_at IS NULL", recordID, sourceID)

	if !q.CategoryAll {
		switch {
		case q.IsOther && q.UseCategoryKeys:
			if len(q.KnownKeys) > 0 {
				query = query.Where(
					"(fi.category_key IS NULL OR fi.category_key = '' OR fi.category_key NOT IN ?) AND (fi.root_category_key IS NULL OR fi.root_category_key = '' OR fi.root_category_key NOT IN ?)",
					q.KnownKeys, q.KnownKeys,
				)
			}
		case q.IsOther && !q.UseCategoryKeys:
			if len(q.KnownPids) > 0 {
				query = query.Where("(fi.pid IS NULL OR fi.pid <= 0 OR fi.pid NOT IN ?)", q.KnownPids)
			}
		case !q.IsOther && q.UseCategoryKeys:
			if len(q.IncludeKeys) == 0 {
				return nil, nil
			}
			if len(q.KnownKeys) > 0 {
				query = query.Where(
					"(fi.category_key IN ? OR ((fi.category_key IS NULL OR fi.category_key = '' OR fi.category_key NOT IN ?) AND fi.root_category_key IN ?))",
					q.IncludeKeys, q.KnownKeys, q.IncludeKeys,
				)
			} else {
				query = query.Where("(fi.category_key IN ? OR fi.root_category_key IN ?)", q.IncludeKeys, q.IncludeKeys)
			}
		default:
			if len(q.IncludePids) == 0 {
				return nil, nil
			}
			query = query.Where("fi.pid IN ?", q.IncludePids)
		}
	}

	// 按 crf.mid ASC 沿聚簇主键 (record_id, source_id, mid) 顺序扫描，STRAIGHT_JOIN 主键校验 deleted_at 后早停，避免 filesort。
	if err := query.Select("crf.mid").Order("crf.mid ASC").Limit(q.Limit).Offset(offset).Pluck("crf.mid", &mids).Error; err != nil {
		return nil, err
	}
	if len(mids) == 0 {
		return []model.NotifyFilmItem{}, nil
	}

	type nameRow struct {
		Mid  int64
		Name string
	}
	var nameRows []nameRow
	if err := db.Mdb.WithContext(ctx).Unscoped().Table(model.TableFilmIndex).
		Select("mid, name").
		Where("mid IN ?", mids).
		Scan(&nameRows).Error; err != nil {
		return nil, err
	}
	nameByMid := make(map[int64]string, len(nameRows))
	for _, r := range nameRows {
		nameByMid[r.Mid] = strings.TrimSpace(r.Name)
	}
	items := make([]model.NotifyFilmItem, 0, len(mids))
	for _, mid := range mids {
		items = append(items, model.NotifyFilmItem{
			Mid:  mid,
			Name: nameByMid[mid],
		})
	}
	if db.Rdb != nil && ctx.Err() == nil {
		if raw, err := json.Marshal(items); err == nil {
			if err := db.Rdb.Set(ctx, cacheKey, raw, notifyPageCacheTTL).Err(); err != nil && ctx.Err() == nil {
				syslog.Warnf("[Notify] 写入分页缓存失败 key=%s err=%v", cacheKey, err)
			}
		}
	}
	return items, nil
}
