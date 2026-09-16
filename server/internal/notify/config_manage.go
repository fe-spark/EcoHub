package notify

import (
	"fmt"
	"regexp"
	"strings"

	"server/internal/model"
	"server/internal/repository"
)

var (
	// 数字 chat id（含负数群组/频道）或 @username
	chatIDPattern = regexp.MustCompile(`^(-?\d+|@[A-Za-z0-9_]+)$`)
)

// MaskBotToken Token 脱敏展示。
func MaskBotToken(token string) string {
	token = strings.TrimSpace(token)
	if token == "" {
		return ""
	}
	r := []rune(token)
	n := len(r)
	if n <= 10 {
		return strings.Repeat("*", n)
	}
	return string(r[:6]) + "***" + string(r[n-4:])
}

// IsMaskedToken 判断是否为脱敏后的占位 token（更新时保留旧值）。
// 与 MaskBotToken 输出对齐：短 token 全 `*`，长 token 为「前6 + *** + 后4」。
func IsMaskedToken(token string) bool {
	token = strings.TrimSpace(token)
	if token == "" {
		return false
	}
	if strings.Trim(token, "*") == "" {
		return true
	}
	// 与 MaskBotToken 长 token 形态一致
	if len([]rune(token)) > 10 && strings.Contains(token, "***") {
		parts := strings.Split(token, "***")
		if len(parts) == 2 && len([]rune(parts[0])) == 6 && len([]rune(parts[1])) == 4 {
			return true
		}
	}
	return false
}

// PublicConfig 返回给前端的配置（Token 脱敏）。
func PublicConfig(cfg model.NotifyConfig) model.NotifyConfig {
	cfg.BotToken = MaskBotToken(cfg.BotToken)
	if cfg.ChatIDs == nil {
		cfg.ChatIDs = []string{}
	}
	return cfg
}

// ValidateAndMergeUpdate 校验更新请求，并与旧配置合并 Token。
func ValidateAndMergeUpdate(old, incoming model.NotifyConfig) (model.NotifyConfig, error) {
	cfg := incoming
	// Token：脱敏或空则保留旧值
	token := strings.TrimSpace(cfg.BotToken)
	if token == "" || IsMaskedToken(token) {
		cfg.BotToken = old.BotToken
	} else {
		cfg.BotToken = token
	}

	chatIDs := repository.NormalizeChatIDs(cfg.ChatIDs)
	// 仅启用时校验 Chat ID 格式：禁用状态下允许保存（含清理残留无效 ID），避免无法保存配置
	if cfg.Enabled {
		for _, id := range chatIDs {
			if !chatIDPattern.MatchString(id) {
				return model.NotifyConfig{}, fmt.Errorf("无效的 Chat ID: %s", id)
			}
		}
	}
	cfg.ChatIDs = chatIDs

	// ChatIDs 为成员真相源：合并旧配置与请求中的 Target 元数据（Thread/等级/订阅），
	// 再按 ChatIDs 重建，避免前端只改 chatIds 时残留旧 Targets 导致仍发到旧群。
	targetSources := make([]model.NotifyTarget, 0, len(old.Targets)+len(cfg.Targets))
	targetSources = append(targetSources, old.Targets...)
	targetSources = append(targetSources, cfg.Targets...)
	cfg.Targets = repository.RebuildTargetsFromChatIDs(cfg.ChatIDs, targetSources)

	if cfg.MaxFilmsInMessage <= 0 {
		cfg.MaxFilmsInMessage = model.DefaultMaxFilmsInMessage
	}
	if cfg.MaxFilmsInMessage > model.MaxFilmsInMessageCap {
		return model.NotifyConfig{}, fmt.Errorf("maxFilmsInMessage 范围为 1-%d", model.MaxFilmsInMessageCap)
	}
	if cfg.MinIntervalSec < 0 || cfg.MinIntervalSec > 3600 {
		return model.NotifyConfig{}, fmt.Errorf("minIntervalSec 范围为 0-3600")
	}

	if cfg.QuietHours.Enabled {
		if strings.TrimSpace(cfg.QuietHours.Start) == "" || strings.TrimSpace(cfg.QuietHours.End) == "" {
			return model.NotifyConfig{}, fmt.Errorf("启用免打扰时必须填写开始与结束时间 (HH:mm)")
		}
		if _, _, err := parseHHMM(cfg.QuietHours.Start); err != nil {
			return model.NotifyConfig{}, fmt.Errorf("免打扰开始时间格式无效，应为 HH:mm")
		}
		if _, _, err := parseHHMM(cfg.QuietHours.End); err != nil {
			return model.NotifyConfig{}, fmt.Errorf("免打扰结束时间格式无效，应为 HH:mm")
		}
	}

	if cfg.Enabled {
		if strings.TrimSpace(cfg.BotToken) == "" {
			return model.NotifyConfig{}, fmt.Errorf("启用通知时必须配置 Bot Token")
		}
		if len(cfg.ChatIDs) == 0 {
			return model.NotifyConfig{}, fmt.Errorf("启用通知时至少配置一个 Chat ID")
		}
	}
	return cfg, nil
}

// GetConfig 读取配置。
func GetConfig() model.NotifyConfig {
	return repository.GetNotifyConfig()
}

// SaveConfig 保存配置。
func SaveConfig(cfg model.NotifyConfig) error {
	if err := repository.SaveNotifyConfig(cfg); err != nil {
		return err
	}
	EnsureBotPoller()
	return nil
}

// siteName 读取站点名用于消息前缀。
func siteName() string {
	return strings.TrimSpace(repository.GetSiteBasic().SiteName)
}

// eventEnabled 判断事件是否开启。
func eventEnabled(cfg model.NotifyConfig, event string) bool {
	if !cfg.Enabled {
		return false
	}
	switch event {
	case model.NotifyEventCollectBatchSummary:
		return cfg.Events.CollectBatchSummary
	case model.NotifyEventCollectSourceFailed:
		return cfg.Events.CollectSourceFailed
	case model.NotifyEventCollectFinalizeFailed:
		return cfg.Events.CollectFinalizeFailed
	case model.NotifyEventCollectProgressStale:
		return cfg.Events.CollectProgressStale
	case model.NotifyEventCronTaskFailed:
		return cfg.Events.CronTaskFailed
	case model.NotifyEventCronTaskDone:
		return cfg.Events.CronTaskDone
	case model.NotifyEventSourceConfigChanged:
		return cfg.Events.SourceConfigChanged
	default:
		return false
	}
}

// IsEventEnabled 读取当前配置判断某事件是否开启（总开关 + 子开关）。
func IsEventEnabled(event string) bool {
	return eventEnabled(GetConfig(), event)
}
