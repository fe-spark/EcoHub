package repository

import (
	"encoding/json"
	"log"
	"sort"
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
	if data := db.Rdb.Get(db.Cxt, config.NotifyConfigKey).Val(); data != "" {
		raw := []byte(data)
		if err := json.Unmarshal(raw, &cfg); err == nil {
			applyMissingOnlyNotifyOnUpdateDefault(raw, &cfg)
			return normalizeNotifyConfig(cfg)
		}
		log.Println("GetNotifyConfig Redis Unmarshal Error")
		db.Rdb.Del(db.Cxt, config.NotifyConfigKey)
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

	// ChatIDs 为成员列表真相源；Targets 承载 Thread/等级等路由元数据。
	// 若仅有 Targets（高级配置），则反推 ChatIDs。
	if len(cfg.ChatIDs) == 0 && len(cfg.Targets) > 0 {
		derived := make([]string, 0, len(cfg.Targets))
		for _, t := range cfg.Targets {
			if id := strings.TrimSpace(t.ChatID); id != "" {
				derived = append(derived, id)
			}
		}
		cfg.ChatIDs = NormalizeChatIDs(derived)
	}
	// 始终按 ChatIDs 重建 Targets，避免「改了 Chat 仍发到旧 Target」的漂移。
	cfg.Targets = RebuildTargetsFromChatIDs(cfg.ChatIDs, cfg.Targets)

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

// RebuildTargetsFromChatIDs 以 chatIDs 为成员真相源重建 Targets。
// sources 中同 ChatID（及 ThreadID）的元数据会被保留；后出现的覆盖先前的。
// 不在 chatIDs 中的目标丢弃；chatIDs 中全新的项生成默认 Target。
func RebuildTargetsFromChatIDs(chatIDs []string, sources []model.NotifyTarget) []model.NotifyTarget {
	chatIDs = NormalizeChatIDs(chatIDs)
	if len(chatIDs) == 0 {
		if len(sources) == 0 {
			return []model.NotifyTarget{}
		}
		// 无成员列表时不保留游离 Targets，避免与 ChatIDs 再次漂移
		return []model.NotifyTarget{}
	}

	// chatID -> threadID -> target（后写覆盖）
	byChatThread := make(map[string]map[string]model.NotifyTarget, len(chatIDs))
	for _, t := range sources {
		chat := strings.TrimSpace(t.ChatID)
		if chat == "" {
			continue
		}
		thread := strings.TrimSpace(t.ThreadID)
		if byChatThread[chat] == nil {
			byChatThread[chat] = make(map[string]model.NotifyTarget)
		}
		nt := t
		nt.ChatID = chat
		nt.ThreadID = thread
		if strings.TrimSpace(nt.ID) == "" {
			nt.ID = chat
		}
		if strings.TrimSpace(nt.Name) == "" {
			nt.Name = chat
		}
		if nt.MinLevel == "" {
			nt.MinLevel = model.SeverityInfo
		}
		byChatThread[chat][thread] = nt
	}

	out := make([]model.NotifyTarget, 0, len(chatIDs))
	for _, id := range chatIDs {
		tm := byChatThread[id]
		if len(tm) == 0 {
			out = append(out, model.NotifyTarget{
				ID:       id,
				Name:     id,
				ChatID:   id,
				Enabled:  true,
				MinLevel: model.SeverityInfo,
			})
			continue
		}
		// 稳定顺序：无 thread 优先，其余按 threadID 排序
		if t, ok := tm[""]; ok {
			out = append(out, t)
			delete(tm, "")
		}
		if len(tm) == 0 {
			continue
		}
		threads := make([]string, 0, len(tm))
		for th := range tm {
			threads = append(threads, th)
		}
		sort.Strings(threads)
		for _, th := range threads {
			out = append(out, tm[th])
		}
	}
	return out
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
