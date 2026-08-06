package notify

import (
	"fmt"
	"html"
	"strings"
	"time"
	"unicode/utf8"

	"server/internal/model"
)

const telegramMaxMessageLen = 4096

func formatTitlePrefix(siteName string) string {
	siteName = strings.TrimSpace(siteName)
	if siteName == "" {
		return "[EcoHub]"
	}
	return fmt.Sprintf("[EcoHub · %s]", html.EscapeString(siteName))
}

func triggerLabel(trigger string) string {
	switch trigger {
	case model.NotifyTriggerCron:
		return "定时任务"
	case model.NotifyTriggerSingleUpdate:
		return "单片更新"
	case model.NotifyTriggerRecover:
		return "失败重试"
	case model.NotifyTriggerManual:
		return "手动采集"
	default:
		if trigger == "" {
			return "采集任务"
		}
		return trigger
	}
}

func gradeLabel(grade int) string {
	if grade == int(model.MasterCollect) {
		return "主站"
	}
	return "附属"
}

func formatBatchSummary(payload model.CollectBatchNotifyPayload, maxFilms int) []string {
	if maxFilms <= 0 {
		maxFilms = 30
	}
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 采集结果</b>\n", formatTitlePrefix(payload.SiteName))
	fmt.Fprintf(&b, "触发: %s\n", html.EscapeString(triggerLabel(payload.Trigger)))
	if payload.DurationSec > 0 {
		fmt.Fprintf(&b, "耗时: %s\n", html.EscapeString(formatDuration(payload.DurationSec)))
	}
	fmt.Fprintf(&b, "源: ✅%d  ❌%d  影片: %d 部\n",
		payload.SuccessSources, payload.FailedSources, payload.TotalFilms)
	if errMsg := strings.TrimSpace(payload.FinalizeError); errMsg != "" {
		fmt.Fprintf(&b, "收尾错误: %s\n", html.EscapeString(truncateRunes(errMsg, 300)))
	}

	filmBudget := maxFilms
	if !payload.IncludeFilmDetails {
		filmBudget = 0
	}
	for _, src := range payload.Sources {
		fmt.Fprintf(&b, "\n")
		statusIcon := statusIcon(src.Status)
		fmt.Fprintf(&b, "<b>%s %s</b> (%s)\n",
			statusIcon,
			html.EscapeString(src.SourceName),
			html.EscapeString(gradeLabel(src.Grade)),
		)
		fmt.Fprintf(&b, "页 %d/%d · 成功 %d · 失败 %d",
			src.PageCurrent, src.PageTotal, src.SuccessCnt, src.FailedCnt)
		if src.FilmsTotal > 0 {
			fmt.Fprintf(&b, " · 影片 %d", src.FilmsTotal)
		}
		b.WriteByte('\n')
		if errMsg := strings.TrimSpace(src.Error); errMsg != "" {
			fmt.Fprintf(&b, "原因: %s\n", html.EscapeString(truncateRunes(errMsg, 200)))
		}
		if filmBudget > 0 && len(src.Films) > 0 {
			shown := 0
			for _, film := range src.Films {
				if filmBudget <= 0 {
					break
				}
				name := film.Name
				if name == "" {
					name = fmt.Sprintf("#%d", film.Mid)
				}
				fmt.Fprintf(&b, "· %s (#%d)\n", html.EscapeString(truncateRunes(name, 80)), film.Mid)
				filmBudget--
				shown++
			}
			remain := src.FilmsTotal - shown
			if remain < 0 {
				remain = 0
			}
			if src.FilmsTrunc || remain > 0 {
				if remain == 0 && src.FilmsTrunc {
					remain = src.FilmsTotal - shown
				}
				if remain > 0 {
					fmt.Fprintf(&b, "· … 另有 %d 部\n", remain)
				}
			}
		}
	}

	return splitTelegramMessages(b.String())
}

func formatSourceFailed(siteName, sourceName, sourceID, reason string, at time.Time) []string {
	return formatSourceAlert(siteName, "采集源失败", "failed", sourceName, sourceID, reason, at)
}

func formatProgressStale(siteName, sourceName, sourceID, reason string, at time.Time) []string {
	return formatSourceAlert(siteName, "采集进度超时", "stale", sourceName, sourceID, reason, at)
}

func formatSourceAlert(siteName, title, status, sourceName, sourceID, reason string, at time.Time) []string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s %s</b>\n", formatTitlePrefix(siteName), html.EscapeString(title))
	fmt.Fprintf(&b, "源: %s", html.EscapeString(sourceName))
	if sourceID != "" {
		fmt.Fprintf(&b, " (%s)", html.EscapeString(sourceID))
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "状态: %s\n", html.EscapeString(status))
	if reason != "" {
		fmt.Fprintf(&b, "原因: %s\n", html.EscapeString(truncateRunes(reason, 400)))
	}
	if !at.IsZero() {
		fmt.Fprintf(&b, "时间: %s\n", html.EscapeString(at.In(time.FixedZone("CST", 8*3600)).Format(time.DateTime)))
	}
	return []string{b.String()}
}

func formatFinalizeFailed(siteName, reason string, sourceCount int, at time.Time) []string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 采集收尾失败</b>\n", formatTitlePrefix(siteName))
	fmt.Fprintf(&b, "涉及源数: %d\n", sourceCount)
	if reason != "" {
		fmt.Fprintf(&b, "原因: %s\n", html.EscapeString(truncateRunes(reason, 500)))
	}
	if !at.IsZero() {
		fmt.Fprintf(&b, "时间: %s\n", html.EscapeString(at.In(time.FixedZone("CST", 8*3600)).Format(time.DateTime)))
	}
	return []string{b.String()}
}

func formatCronFailed(siteName, taskID, remark, reason string, at time.Time) []string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 定时任务失败</b>\n", formatTitlePrefix(siteName))
	if taskID != "" {
		fmt.Fprintf(&b, "任务: %s\n", html.EscapeString(taskID))
	}
	if remark != "" {
		fmt.Fprintf(&b, "备注: %s\n", html.EscapeString(remark))
	}
	if reason != "" {
		fmt.Fprintf(&b, "原因: %s\n", html.EscapeString(truncateRunes(reason, 400)))
	}
	if !at.IsZero() {
		fmt.Fprintf(&b, "时间: %s\n", html.EscapeString(at.In(time.FixedZone("CST", 8*3600)).Format(time.DateTime)))
	}
	return []string{b.String()}
}

func formatCronDone(siteName, taskID, remark, detail string, at time.Time) []string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 定时任务完成</b>\n", formatTitlePrefix(siteName))
	if taskID != "" {
		fmt.Fprintf(&b, "任务: %s\n", html.EscapeString(taskID))
	}
	if remark != "" {
		fmt.Fprintf(&b, "备注: %s\n", html.EscapeString(remark))
	}
	if detail != "" {
		fmt.Fprintf(&b, "%s\n", html.EscapeString(truncateRunes(detail, 400)))
	}
	if !at.IsZero() {
		fmt.Fprintf(&b, "时间: %s\n", html.EscapeString(at.In(time.FixedZone("CST", 8*3600)).Format(time.DateTime)))
	}
	return []string{b.String()}
}

func formatTestMessage(siteName string) string {
	return fmt.Sprintf("<b>%s 通知测试</b>\nTelegram 通知已连通。\n时间: %s",
		formatTitlePrefix(siteName),
		html.EscapeString(time.Now().In(time.FixedZone("CST", 8*3600)).Format(time.DateTime)),
	)
}

func statusIcon(status string) string {
	switch status {
	case "done":
		return "✅"
	case "failed":
		return "❌"
	case "stopped":
		return "⏹"
	default:
		return "•"
	}
}

func formatDuration(sec int64) string {
	if sec < 60 {
		return fmt.Sprintf("%ds", sec)
	}
	m := sec / 60
	s := sec % 60
	if m < 60 {
		return fmt.Sprintf("%dm%ds", m, s)
	}
	h := m / 60
	m = m % 60
	return fmt.Sprintf("%dh%dm%ds", h, m, s)
}

func truncateRunes(s string, max int) string {
	if max <= 0 || s == "" {
		return s
	}
	if utf8.RuneCountInString(s) <= max {
		return s
	}
	runes := []rune(s)
	return string(runes[:max]) + "…"
}

// splitTelegramMessages 按 4096 字符上限拆分；优先在换行处切。
func splitTelegramMessages(text string) []string {
	if text == "" {
		return nil
	}
	if utf8.RuneCountInString(text) <= telegramMaxMessageLen {
		return []string{text}
	}
	runes := []rune(text)
	var parts []string
	for len(runes) > 0 && len(parts) < 3 {
		if len(runes) <= telegramMaxMessageLen {
			parts = append(parts, string(runes))
			break
		}
		cut := telegramMaxMessageLen
		window := runes[:cut]
		if idx := lastNewline(window); idx > cut/2 {
			cut = idx + 1
		}
		parts = append(parts, string(runes[:cut]))
		runes = runes[cut:]
	}
	if len(runes) > 0 && len(parts) >= 3 {
		// 第三条已满，丢弃剩余并在末尾提示
		last := parts[len(parts)-1]
		hint := "\n…消息过长，已截断，详见后台"
		if utf8.RuneCountInString(last)+utf8.RuneCountInString(hint) > telegramMaxMessageLen {
			r := []rune(last)
			keep := telegramMaxMessageLen - utf8.RuneCountInString(hint)
			if keep > 0 {
				last = string(r[:keep])
			}
		}
		parts[len(parts)-1] = last + hint
	}
	return parts
}

func lastNewline(runes []rune) int {
	for i := len(runes) - 1; i >= 0; i-- {
		if runes[i] == '\n' {
			return i
		}
	}
	return -1
}
