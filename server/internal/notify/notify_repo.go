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

	"gorm.io/gorm"
)

const (
	notifyBatchSessionTTL = 24 * time.Hour
	maxMemNotifySessions  = 64
	notifyPageCacheTTL    = 30 * time.Minute
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
	return repobase.DropConfigCache(config.NotifyConfigKey)
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

// BatchSourceCategoryStatRow 表示主站按分类字段聚合后的计数行
type BatchSourceCategoryStatRow struct {
	SourceID        string
	FirstSourceId   string
	Pid             int64
	Cid             int64
	CategoryKey     string
	RootCategoryKey string
	Count           int
}

// BatchSourceCategoryPageQuery 定义基于 film_index 联合索引的主站分类分页查询条件
type BatchSourceCategoryPageQuery struct {
	RecordID        uint
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

func notifySourcePageCacheKey(q BatchSourceCategoryPageQuery) string {
	raw := fmt.Sprintf(
		"%d|%s|%t|%t|%t|%s|%s|%v|%v|%d|%d",
		q.RecordID,
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

var loadBatchPrimaryCategoryStatsFn = LoadBatchPrimaryCategoryStats

// LoadBatchPrimaryCategoryStats 直接基于 film_index 联合索引 (first_source_id, last_record_id, mid) 聚合主站本次更新的分类计数。
func LoadBatchPrimaryCategoryStats(ctx context.Context, recordID uint, sourceID string) ([]BatchSourceCategoryStatRow, error) {
	sourceID = strings.TrimSpace(sourceID)
	if recordID == 0 || sourceID == "" || db.Mdb == nil {
		return nil, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return nil, err
	}
	type statRaw struct {
		Pid             sql.NullInt64
		Cid             sql.NullInt64
		CategoryKey     sql.NullString
		RootCategoryKey sql.NullString
		Count           int64
	}
	var rows []statRaw
	err := db.Mdb.WithContext(ctx).Table(model.TableFilmIndex).
		Select("pid, cid, category_key, root_category_key, COUNT(*) AS count").
		Where("first_source_id = ? AND last_record_id = ? AND deleted_at IS NULL", sourceID, recordID).
		Group("pid, cid, category_key, root_category_key").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	out := make([]BatchSourceCategoryStatRow, 0, len(rows))
	for _, r := range rows {
		out = append(out, BatchSourceCategoryStatRow{
			SourceID:        sourceID,
			FirstSourceId:   sourceID,
			Pid:             r.Pid.Int64,
			Cid:             r.Cid.Int64,
			CategoryKey:     strings.TrimSpace(r.CategoryKey.String),
			RootCategoryKey: strings.TrimSpace(r.RootCategoryKey.String),
			Count:           int(r.Count),
		})
	}
	return out, nil
}

// LoadBatchPrimaryCategoryPage 直接基于 film_index 联合索引 (first_source_id, last_record_id, mid) 单表分页查询当前主站更新片单。
func LoadBatchPrimaryCategoryPage(ctx context.Context, q BatchSourceCategoryPageQuery) ([]model.NotifyFilmItem, error) {
	sourceID := strings.TrimSpace(q.SourceID)
	if q.RecordID == 0 || sourceID == "" || db.Mdb == nil || q.Limit <= 0 {
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

	query := db.Mdb.WithContext(ctx).Table(model.TableFilmIndex).
		Select("mid, name").
		Where("first_source_id = ? AND last_record_id = ? AND deleted_at IS NULL", sourceID, q.RecordID)

	if !q.CategoryAll {
		switch {
		case q.IsOther && q.UseCategoryKeys:
			if len(q.KnownKeys) > 0 {
				query = query.Where(
					"(category_key IS NULL OR category_key = '' OR category_key NOT IN ?) AND (root_category_key IS NULL OR root_category_key = '' OR root_category_key NOT IN ?)",
					q.KnownKeys, q.KnownKeys,
				)
			}
		case q.IsOther && !q.UseCategoryKeys:
			if len(q.KnownPids) > 0 {
				query = query.Where("(pid IS NULL OR pid <= 0 OR pid NOT IN ?)", q.KnownPids)
			}
		case !q.IsOther && q.UseCategoryKeys:
			if len(q.IncludeKeys) == 0 {
				return nil, nil
			}
			if len(q.KnownKeys) > 0 {
				query = query.Where(
					"(category_key IN ? OR ((category_key IS NULL OR category_key = '' OR category_key NOT IN ?) AND root_category_key IN ?))",
					q.IncludeKeys, q.KnownKeys, q.IncludeKeys,
				)
			} else {
				query = query.Where("(category_key IN ? OR root_category_key IN ?)", q.IncludeKeys, q.IncludeKeys)
			}
		default:
			if len(q.IncludePids) == 0 {
				return nil, nil
			}
			query = query.Where("pid IN ?", q.IncludePids)
		}
	}

	type nameRow struct {
		Mid  int64
		Name string
	}
	var rows []nameRow
	if err := query.Order("mid ASC").Limit(q.Limit).Offset(offset).Scan(&rows).Error; err != nil {
		return nil, err
	}
	items := make([]model.NotifyFilmItem, 0, len(rows))
	for _, r := range rows {
		items = append(items, model.NotifyFilmItem{
			Mid:  r.Mid,
			Name: strings.TrimSpace(r.Name),
		})
	}
	if db.Rdb != nil && ctx.Err() == nil && !(offset > 0 && len(items) == 0) {
		if raw, err := json.Marshal(items); err == nil {
			if err := db.Rdb.Set(ctx, cacheKey, raw, notifyPageCacheTTL).Err(); err != nil && ctx.Err() == nil {
				syslog.Warnf("[Notify] 写入分页缓存失败 key=%s err=%v", cacheKey, err)
			}
		}
	}
	return items, nil
}
