package notify

import (
	"fmt"
	"strings"
	"time"

	"server/internal/model"
)

var severityRank = map[model.Severity]int{
	model.SeverityInfo:     1,
	model.SeverityNotice:   2,
	model.SeverityWarn:     3,
	model.SeverityError:    4,
	model.SeverityCritical: 5,
}

func isLevelAllowed(targetMin model.Severity, evtLevel model.Severity) bool {
	if targetMin == "" {
		return true
	}
	rTarget, ok1 := severityRank[targetMin]
	rEvt, ok2 := severityRank[evtLevel]
	if !ok1 || !ok2 {
		return true
	}
	return rEvt >= rTarget
}

func isCategorySubscribed(subscribed []string, category string) bool {
	if len(subscribed) == 0 {
		return true
	}
	for _, c := range subscribed {
		if c == category || strings.HasPrefix(category, c+".") {
			return true
		}
	}
	return false
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

// effectiveTargets 优先 Targets；空时由 ChatIDs 兼容包装。
func effectiveTargets(cfg model.NotifyConfig) []model.NotifyTarget {
	if len(cfg.Targets) > 0 {
		return cfg.Targets
	}
	if len(cfg.ChatIDs) == 0 {
		return nil
	}
	targets := make([]model.NotifyTarget, 0, len(cfg.ChatIDs))
	for _, id := range cfg.ChatIDs {
		targets = append(targets, model.NotifyTarget{
			ID:       id,
			Name:     id,
			ChatID:   id,
			Enabled:  true,
			MinLevel: model.SeverityInfo,
		})
	}
	return targets
}

// routeTargets 按免打扰、最低等级、分类订阅筛选接收目标。
// muted=true 表示被免打扰整体静音（调用方应跳过发送）。
func routeTargets(cfg model.NotifyConfig, severity model.Severity, category string) (targets []model.NotifyTarget, muted bool) {
	if shouldMuteByQuietHours(cfg, severity) {
		return nil, true
	}
	for _, t := range effectiveTargets(cfg) {
		if !t.Enabled || strings.TrimSpace(t.ChatID) == "" {
			continue
		}
		if !isLevelAllowed(t.MinLevel, severity) {
			continue
		}
		if category != "" && !isCategorySubscribed(t.SubscribedCategories, category) {
			continue
		}
		targets = append(targets, t)
	}
	return targets, false
}
