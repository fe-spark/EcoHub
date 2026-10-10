package notify

import (
	"fmt"
	"strings"
	"time"

	"server/internal/model"
)

// ChatTarget 解析后的目标聊天（支持 Topic Thread ID）
type ChatTarget struct {
	ChatID   string
	ThreadID string
}

// ParseChatTarget 从 "chatId" 或 "chatId:threadId" 解析出目标聊天与线程
func ParseChatTarget(raw string) (chatID, threadID string) {
	raw = strings.TrimSpace(raw)
	if idx := strings.Index(raw, ":"); idx != -1 {
		return strings.TrimSpace(raw[:idx]), strings.TrimSpace(raw[idx+1:])
	}
	return raw, ""
}

func parseHHMM(s string) (int, int, error) {
	parts := strings.Split(s, ":")
	if len(parts) != 2 {
		return 0, 0, fmt.Errorf("invalid time format")
	}
	var h, m int
	_, err := fmt.Sscanf(s, "%d:%d", &h, &m)
	if err != nil {
		return 0, 0, err
	}
	if h < 0 || h > 23 || m < 0 || m > 59 {
		return 0, 0, fmt.Errorf("invalid time range")
	}
	return h, m, nil
}

// quietHoursLocation 免打扰按业务时区（东八区）计算，避免 Docker 默认 UTC 导致时段偏移。
var quietHoursLocation = func() *time.Location {
	loc, err := time.LoadLocation("Asia/Shanghai")
	if err != nil {
		return time.FixedZone("CST", 8*3600)
	}
	return loc
}()

func isInQuietHours(qh model.NotifyQuietHours, now time.Time) bool {
	if !qh.Enabled || qh.Start == "" || qh.End == "" {
		return false
	}
	startHour, startMin, err1 := parseHHMM(qh.Start)
	endHour, endMin, err2 := parseHHMM(qh.End)
	if err1 != nil || err2 != nil {
		return false
	}

	local := now.In(quietHoursLocation)
	curMinute := local.Hour()*60 + local.Minute()
	startMinute := startHour*60 + startMin
	endMinute := endHour*60 + endMin

	if startMinute < endMinute {
		return curMinute >= startMinute && curMinute < endMinute
	}
	// 跨午夜：如 23:00–07:00
	return curMinute >= startMinute || curMinute < endMinute
}

// shouldMuteByQuietHours 免打扰时段内且等级不在穿透列表时静音。
func shouldMuteByQuietHours(cfg model.NotifyConfig, severity model.Severity) bool {
	if !isInQuietHours(cfg.QuietHours, time.Now()) {
		return false
	}
	for _, lvl := range cfg.QuietHours.AllowLevels {
		if lvl == severity {
			return false
		}
	}
	return true
}

// routeTargets 按免打扰规则筛选接收目标。
// muted=true 表示被免打扰整体静音（调用方应跳过发送）。
func routeTargets(cfg model.NotifyConfig, severity model.Severity) (targets []ChatTarget, muted bool) {
	if shouldMuteByQuietHours(cfg, severity) {
		return nil, true
	}
	for _, raw := range cfg.ChatIDs {
		cID, tID := ParseChatTarget(raw)
		if cID == "" {
			continue
		}
		targets = append(targets, ChatTarget{ChatID: cID, ThreadID: tID})
	}
	return targets, false
}
