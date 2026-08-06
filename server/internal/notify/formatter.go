package notify

import (
	"fmt"
	"html"
	"strings"
	"time"
	"unicode/utf8"

	"server/internal/model"
	"server/internal/repository"
)

const telegramMaxMessageLen = 4096

// sitePlayBaseURL 读取网站配置中的 siteUrl（测试可覆盖 sitePlayBaseURLFn）。
var sitePlayBaseURLFn = func() string {
	return strings.TrimSpace(repository.GetSiteBasic().SiteURL)
}

// filmPlayURL 使用网站配置中的 siteUrl 拼播放页；未配置则返回空。
func filmPlayURL(mid int64) string {
	if mid <= 0 {
		return ""
	}
	base := strings.TrimSpace(sitePlayBaseURLFn())
	if base == "" {
		return ""
	}
	return fmt.Sprintf("%s/play?id=%d", strings.TrimRight(base, "/"), mid)
}

// formatFilmLine 影片明细一行：名称 + 可点击 #mid（跳转站内播放页）。
func formatFilmLine(film model.FilmNotifyItem) string {
	name := strings.TrimSpace(film.Name)
	if name == "" {
		name = fmt.Sprintf("#%d", film.Mid)
	}
	name = truncateRunes(name, 80)
	idLabel := fmt.Sprintf("#%d", film.Mid)
	if href := filmPlayURL(film.Mid); href != "" {
		return fmt.Sprintf("· %s (<a href=\"%s\">%s</a>)\n",
			html.EscapeString(name),
			html.EscapeString(href),
			html.EscapeString(idLabel),
		)
	}
	return fmt.Sprintf("· %s (<code>%s</code>)\n",
		html.EscapeString(name),
		html.EscapeString(idLabel),
	)
}

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

// formatBatchOverview 采集结果总览（不含影片明细；明细用内联键盘分页消息）。
func formatBatchOverview(payload model.CollectBatchNotifyPayload, filmItemCount, pageSize int) string {
	if pageSize <= 0 {
		pageSize = 30
	}
	var overview strings.Builder
	fmt.Fprintf(&overview, "<b>%s 采集结果</b>\n", formatTitlePrefix(payload.SiteName))
	fmt.Fprintf(&overview, "<b>⚡ 触发:</b> %s\n", html.EscapeString(triggerLabel(payload.Trigger)))
	if payload.DurationSec > 0 {
		fmt.Fprintf(&overview, "<b>⏱ 耗时:</b> %s\n", html.EscapeString(formatDuration(payload.DurationSec)))
	}
	fmt.Fprintf(&overview, "<b>📊 统计:</b> 源 ✅<b>%d</b> · ❌<b>%d</b> | 影片 <b>%d</b> 部\n",
		payload.SuccessSources, payload.FailedSources, payload.TotalFilms)
	if errMsg := strings.TrimSpace(payload.FinalizeError); errMsg != "" {
		fmt.Fprintf(&overview, "<b>⚠️ 收尾错误:</b> <code>%s</code>\n", html.EscapeString(truncateRunes(errMsg, 300)))
	}

	for _, src := range payload.Sources {
		overview.WriteByte('\n')
		fmt.Fprintf(&overview, "<b>%s %s</b> <code>(%s)</code>\n",
			statusIcon(src.Status),
			html.EscapeString(src.SourceName),
			html.EscapeString(gradeLabel(src.Grade)),
		)
		fmt.Fprintf(&overview, "页 <code>%d/%d</code> · 成功 <code>%d</code> · 失败 <code>%d</code>",
			src.PageCurrent, src.PageTotal, src.SuccessCnt, src.FailedCnt)
		if src.FilmsTotal > 0 {
			fmt.Fprintf(&overview, " · 影片 <b>%d</b>", src.FilmsTotal)
		}
		overview.WriteByte('\n')
		if errMsg := strings.TrimSpace(src.Error); errMsg != "" {
			fmt.Fprintf(&overview, "❌ <code>原因: %s</code>\n", html.EscapeString(truncateRunes(errMsg, 200)))
		}
	}

	if payload.IncludeFilmDetails && filmItemCount > 0 {
		pages := (filmItemCount + pageSize - 1) / pageSize
		fmt.Fprintf(&overview, "\n📋 <b>影片明细</b> 共 <b>%d</b> 条 · <b>%d</b> 页（每页 %d 条）\n",
			filmItemCount, pages, pageSize)
		fmt.Fprintf(&overview, "<i>请点击下方消息的「上一页 / 下一页」浏览</i>\n")
	}
	return overview.String()
}

// formatBatchSummary 兼容旧调用：仅返回总览文本（可能按 4096 拆分）。
// 带按钮的影片列表由 PublishBatchSummary 单独发送。
func formatBatchSummary(payload model.CollectBatchNotifyPayload, maxFilms int) []string {
	n := 0
	for _, s := range payload.Sources {
		n += len(s.Films)
	}
	return splitTelegramMessages(formatBatchOverview(payload, n, maxFilms))
}

func formatSourceFailed(siteName, sourceName, sourceID, reason string, at time.Time) []string {
	return formatSourceAlert(siteName, "采集源失败告警", "failed", sourceName, sourceID, reason, at)
}

func formatProgressStale(siteName, sourceName, sourceID, reason string, at time.Time) []string {
	return formatSourceAlert(siteName, "采集进度超时告警", "stale", sourceName, sourceID, reason, at)
}

func formatSourceAlert(siteName, title, status, sourceName, sourceID, reason string, at time.Time) []string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s ⚠️ %s</b>\n", formatTitlePrefix(siteName), html.EscapeString(title))
	fmt.Fprintf(&b, "<b>📌 采集源:</b> %s", html.EscapeString(sourceName))
	if sourceID != "" {
		fmt.Fprintf(&b, " (<code>%s</code>)", html.EscapeString(sourceID))
	}
	b.WriteByte('\n')
	fmt.Fprintf(&b, "<b>🚨 状态:</b> <code>%s</code>\n", html.EscapeString(status))
	if reason != "" {
		fmt.Fprintf(&b, "<b>❌ 原因:</b> <code>%s</code>\n", html.EscapeString(truncateRunes(reason, 400)))
	}
	if !at.IsZero() {
		fmt.Fprintf(&b, "<b>🕒 时间:</b> <code>%s</code>\n", html.EscapeString(at.In(time.FixedZone("CST", 8*3600)).Format(time.DateTime)))
	}
	return []string{b.String()}
}

func formatFinalizeFailed(siteName, reason string, sourceCount int, at time.Time) []string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s ⚠️ 采集收尾失败</b>\n", formatTitlePrefix(siteName))
	fmt.Fprintf(&b, "<b>📊 涉及源数:</b> <code>%d</code>\n", sourceCount)
	if reason != "" {
		fmt.Fprintf(&b, "<b>❌ 原因:</b> <code>%s</code>\n", html.EscapeString(truncateRunes(reason, 500)))
	}
	if !at.IsZero() {
		fmt.Fprintf(&b, "<b>🕒 时间:</b> <code>%s</code>\n", html.EscapeString(at.In(time.FixedZone("CST", 8*3600)).Format(time.DateTime)))
	}
	return []string{b.String()}
}

func formatCronFailed(siteName, taskID, remark, reason string, at time.Time) []string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 🚨 定时任务失败</b>\n", formatTitlePrefix(siteName))
	if taskID != "" {
		fmt.Fprintf(&b, "<b>📌 任务ID:</b> <code>%s</code>\n", html.EscapeString(taskID))
	}
	if remark != "" {
		fmt.Fprintf(&b, "<b>📝 备注:</b> %s\n", html.EscapeString(remark))
	}
	if reason != "" {
		fmt.Fprintf(&b, "<b>❌ 原因:</b> <code>%s</code>\n", html.EscapeString(truncateRunes(reason, 400)))
	}
	if !at.IsZero() {
		fmt.Fprintf(&b, "<b>🕒 时间:</b> <code>%s</code>\n", html.EscapeString(at.In(time.FixedZone("CST", 8*3600)).Format(time.DateTime)))
	}
	return []string{b.String()}
}

func formatCronDone(siteName, taskID, remark, detail string, at time.Time) []string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s ✅ 定时任务完成</b>\n", formatTitlePrefix(siteName))
	if taskID != "" {
		fmt.Fprintf(&b, "<b>📌 任务ID:</b> <code>%s</code>\n", html.EscapeString(taskID))
	}
	if remark != "" {
		fmt.Fprintf(&b, "<b>📝 备注:</b> %s\n", html.EscapeString(remark))
	}
	if detail != "" {
		fmt.Fprintf(&b, "<b>📋 明细:</b> %s\n", html.EscapeString(truncateRunes(detail, 400)))
	}
	if !at.IsZero() {
		fmt.Fprintf(&b, "<b>🕒 时间:</b> <code>%s</code>\n", html.EscapeString(at.In(time.FixedZone("CST", 8*3600)).Format(time.DateTime)))
	}
	return []string{b.String()}
}

func formatTestMessage(siteName string) string {
	return fmt.Sprintf("<b>%s 通知测试</b>\n✅ <b>Telegram 通知服务联通成功！</b>\n🕒 <b>发送时间:</b> <code>%s</code>",
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
		hint := "\n<i>…消息过长，已截断，详见后台</i>"
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
