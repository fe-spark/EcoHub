package repository

import (
	"database/sql"
	"encoding/json"
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

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
	return loadCachedConfig(
		config.NotifyConfigKey,
		decodeNotifyConfig,
		func() (model.NotifyConfig, string, bool, error) {
			if db.Mdb == nil {
				return model.NotifyConfig{}, "", false, errors.New("数据库未连接")
			}
			var rec model.NotifyConfigRecord
			if err := db.Mdb.Order("id DESC").First(&rec).Error; err != nil {
				if recordNotFound(err) {
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
	return writeCache(config.NotifyConfigKey, string(data))
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
			_ = db.Rdb.Set(db.Cxt, key, raw, notifyBatchSessionTTL).Err()
		}
	}
	return nil
}

// LoadNotifyBatchSession 加载批次翻页会话（内存优先，Redis 兜底）
func LoadNotifyBatchSession(batchID string) (model.NotifyBatchSession, error) {
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
		if data, err := db.Rdb.Get(db.Cxt, key).Result(); err == nil && data != "" {
			var sess model.NotifyBatchSession
			if json.Unmarshal([]byte(data), &sess) == nil {
				ttl := notifyBatchSessionTTL
				if rem, err := db.Rdb.TTL(db.Cxt, key).Result(); err == nil && rem > 0 {
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

// NotifyFilmMeta 影片通知归类与排序元数据
type NotifyFilmMeta struct {
	Mid             int64
	FirstSourceId   string
	Name            string
	Pid             int64
	Cid             int64
	CategoryKey     string
	RootCategoryKey string
	UpdateStamp     int64
}

// LoadNotifyFilmMetas 查询变更影片的标题、分类与更新时间戳，并按与前台每日更新一致的顺序（update_stamp DESC, mid DESC）返回
func LoadNotifyFilmMetas(mids []int64) ([]NotifyFilmMeta, error) {
	if len(mids) == 0 || db.Mdb == nil {
		return nil, nil
	}
	seen := make(map[int64]struct{}, len(mids))
	uniq := make([]int64, 0, len(mids))
	for _, m := range mids {
		if m > 0 {
			if _, ok := seen[m]; !ok {
				seen[m] = struct{}{}
				uniq = append(uniq, m)
			}
		}
	}
	if len(uniq) == 0 {
		return nil, nil
	}

	out := make([]NotifyFilmMeta, 0, len(uniq))
	const chunk = 500
	for start := 0; start < len(uniq); start += chunk {
		end := start + chunk
		if end > len(uniq) {
			end = len(uniq)
		}
		var rows []struct {
			Mid             int64          `gorm:"column:mid"`
			FirstSourceId   sql.NullString `gorm:"column:first_source_id"`
			Name            string         `gorm:"column:name"`
			Pid             sql.NullInt64  `gorm:"column:pid"`
			Cid             sql.NullInt64  `gorm:"column:cid"`
			CategoryKey     sql.NullString `gorm:"column:category_key"`
			RootCategoryKey sql.NullString `gorm:"column:root_category_key"`
			UpdateStamp     int64          `gorm:"column:update_stamp"`
		}
		if err := db.Mdb.Table(model.TableFilmIndex).
			Select("mid, first_source_id, name, pid, cid, category_key, root_category_key, update_stamp").
			Where("mid IN ? AND deleted_at IS NULL", uniq[start:end]).
			Scan(&rows).Error; err != nil {
			return nil, err
		}
		for _, r := range rows {
			var pid, cid int64
			if r.Pid.Valid {
				pid = r.Pid.Int64
			}
			if r.Cid.Valid {
				cid = r.Cid.Int64
			}
			out = append(out, NotifyFilmMeta{
				Mid:             r.Mid,
				FirstSourceId:   strings.TrimSpace(r.FirstSourceId.String),
				Name:            strings.TrimSpace(r.Name),
				Pid:             pid,
				Cid:             cid,
				CategoryKey:     strings.TrimSpace(r.CategoryKey.String),
				RootCategoryKey: strings.TrimSpace(r.RootCategoryKey.String),
				UpdateStamp:     r.UpdateStamp,
			})
		}
	}

	// 与前台「每日更新」完全一致的排序：update_stamp DESC, mid DESC
	sort.SliceStable(out, func(i, j int) bool {
		if out[i].UpdateStamp != out[j].UpdateStamp {
			return out[i].UpdateStamp > out[j].UpdateStamp
		}
		return out[i].Mid > out[j].Mid
	})
	return out, nil
}
