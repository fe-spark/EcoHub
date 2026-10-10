package repository

import (
	"encoding/json"
	"log"
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// DefaultNotifyConfig 返回默认通知配置（总开关关闭）。
func DefaultNotifyConfig() model.NotifyConfig {
	return model.NotifyConfig{
		Enabled:  false,
		BotToken: "",
		ChatIDs:  []string{},
		Events: model.NotifyEventSwitches{
			CollectBatchSummary:   true,
			CollectSourceFailed:   true,
			CollectFinalizeFailed: true,
			CollectProgressStale:  true,
			CronTaskFailed:        true,
			CronTaskDone:          false,
			SourceConfigChanged:   true,
		},
		IncludeFilmDetails: true,
		OnlyNotifyOnUpdate: true, // 默认无更新无失败时静音
		MaxFilmsInMessage:  model.DefaultMaxFilmsInMessage,
		MinIntervalSec:     60,
		QuietHours: model.NotifyQuietHours{
			Enabled:     false,
			Start:       "23:00",
			End:         "07:00",
			AllowLevels: []model.Severity{model.SeverityError, model.SeverityCritical},
		},
	}
}

// applyMissingOnlyNotifyOnUpdateDefault 历史 JSON 若未写入 onlyNotifyOnUpdate 字段，平滑默认 true。
// 显式 false 必须保留，故不能依赖 bool 零值。
func applyMissingOnlyNotifyOnUpdateDefault(raw []byte, cfg *model.NotifyConfig) {
	var probe map[string]json.RawMessage
	if err := json.Unmarshal(raw, &probe); err != nil {
		return
	}
	if _, ok := probe["onlyNotifyOnUpdate"]; !ok {
		cfg.OnlyNotifyOnUpdate = true
	}
}

// GetNotifyConfig 读取通知配置（Redis 优先，MySQL 兜底）。
func GetNotifyConfig() model.NotifyConfig {
	cfg := DefaultNotifyConfig()
	if db.Rdb != nil {
		if data := db.Rdb.Get(db.Cxt, config.NotifyConfigKey).Val(); data != "" {
			raw := []byte(data)
			if err := json.Unmarshal(raw, &cfg); err == nil {
				applyMissingOnlyNotifyOnUpdateDefault(raw, &cfg)
				return normalizeNotifyConfig(cfg)
			}
			log.Println("GetNotifyConfig Redis Unmarshal Error")
			db.Rdb.Del(db.Cxt, config.NotifyConfigKey)
		}
	}
	var rec model.NotifyConfigRecord
	if err := db.Mdb.Order("id DESC").First(&rec).Error; err != nil {
		if err != gorm.ErrRecordNotFound {
			log.Println("GetNotifyConfig MySQL Error:", err)
		}
		return cfg
	}
	raw := []byte(rec.Payload)
	if err := json.Unmarshal(raw, &cfg); err != nil {
		log.Println("GetNotifyConfig Payload Unmarshal Error:", err)
		return DefaultNotifyConfig()
	}
	applyMissingOnlyNotifyOnUpdateDefault(raw, &cfg)
	cfg = normalizeNotifyConfig(cfg)
	cacheNotifyConfig(cfg)
	return cfg
}

// SaveNotifyConfig 持久化通知配置并刷新 Redis。
func SaveNotifyConfig(cfg model.NotifyConfig) error {
	cfg = normalizeNotifyConfig(cfg)
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
	cacheNotifyConfig(cfg)
	return nil
}

func cacheNotifyConfig(cfg model.NotifyConfig) {
	if db.Rdb == nil {
		return
	}
	data, err := json.Marshal(cfg)
	if err != nil {
		return
	}
	if err := db.Rdb.Set(db.Cxt, config.NotifyConfigKey, data, config.ConfigCacheTTL).Err(); err != nil {
		// Set 失败时删掉旧 key，避免后续读到过期 Token/配置（最长可残留 ConfigCacheTTL）。
		log.Println("SaveNotifyConfig Redis Error:", err)
		if delErr := db.Rdb.Del(db.Cxt, config.NotifyConfigKey).Err(); delErr != nil {
			log.Println("SaveNotifyConfig Redis Del Error:", delErr)
		}
	}
}

func normalizeNotifyConfig(cfg model.NotifyConfig) model.NotifyConfig {
	cfg.BotToken = strings.TrimSpace(cfg.BotToken)
	cfg.ChatIDs = NormalizeChatIDs(cfg.ChatIDs)

	if cfg.QuietHours.AllowLevels == nil {
		cfg.QuietHours.AllowLevels = []model.Severity{model.SeverityError, model.SeverityCritical}
	}

	if cfg.MaxFilmsInMessage <= 0 {
		cfg.MaxFilmsInMessage = model.DefaultMaxFilmsInMessage
	}
	// 与 model.MaxFilmsInMessageCap 一致（分页每页条数上限）
	if cfg.MaxFilmsInMessage > model.MaxFilmsInMessageCap {
		cfg.MaxFilmsInMessage = model.MaxFilmsInMessageCap
	}
	if cfg.MinIntervalSec < 0 {
		cfg.MinIntervalSec = 0
	}
	if cfg.MinIntervalSec > 3600 {
		cfg.MinIntervalSec = 3600
	}
	if cfg.QuietHours.Start == "" {
		cfg.QuietHours.Start = "23:00"
	}
	if cfg.QuietHours.End == "" {
		cfg.QuietHours.End = "07:00"
	}
	return cfg
}

// NormalizeChatIDs 去空、trim、去重（notify 校验与持久化共用）。
func NormalizeChatIDs(ids []string) []string {
	if len(ids) == 0 {
		return []string{}
	}
	seen := make(map[string]struct{}, len(ids))
	out := make([]string, 0, len(ids))
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}
