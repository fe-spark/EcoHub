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

// formatFilmLine 影片一行：片名可点进站内播放页（依赖网站配置 siteUrl），尾部挂源名称。
func formatFilmLine(film model.FilmNotifyItem) string {
	name := strings.TrimSpace(film.Name)
	if name == "" {
		name = fmt.Sprintf("#%d", film.Mid)
	}
	name = truncateRunes(name, 80)
	idLabel := fmt.Sprintf("#%d", film.Mid)
	sourceSuffix := ""
	if src := strings.TrimSpace(film.SourceName); src != "" {
		sourceSuffix = fmt.Sprintf(" [%s]", html.EscapeString(src))
	}
	if href := filmPlayURL(film.Mid); href != "" {
		// 片名整段可点；Telegram 内打开浏览器/内置 WebView，非聊天窗内嵌播放
		return fmt.Sprintf("· <a href=\"%s\">%s</a> (<code>%s</code>)%s\n",
			html.EscapeString(href),
			html.EscapeString(name),
			html.EscapeString(idLabel),
			sourceSuffix,
		)
	}
	return fmt.Sprintf("· %s (<code>%s</code>)%s\n",
		html.EscapeString(name),
		html.EscapeString(idLabel),
		sourceSuffix,
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

// formatBatchOverview 采集概要（普通 HTML，不用 <pre>，避免 TG 显示「复制代码」）。
// listN=去重后列表条数（与更新列表一致）；pageSize 用于算页数。
func formatBatchOverview(payload model.CollectBatchNotifyPayload, listN, pageSize int) string {
	if pageSize <= 0 {
		pageSize = 30
	}
	// 头行「变更」与列表一致：优先 listN；无明细时用 payload.TotalFilms（已是去重语义时由 Build 保证）
	headerChanged := listN
	if headerChanged <= 0 {
		headerChanged = payload.TotalFilms
	}
	var overview strings.Builder
	fmt.Fprintf(&overview, "<b>%s 采集概要</b>\n", formatTitlePrefix(payload.SiteName))
	fmt.Fprintf(&overview, "⚡ %s", html.EscapeString(triggerLabel(payload.Trigger)))
	if payload.DurationSec > 0 {
		fmt.Fprintf(&overview, " · ⏱ %s", html.EscapeString(formatDuration(payload.DurationSec)))
	}
	overview.WriteByte('\n')
	if payload.FailedSources > 0 {
		fmt.Fprintf(&overview, "📊 采集源：成功 <b>%d</b> 个，失败 <b>%d</b> 个\n",
			payload.SuccessSources, payload.FailedSources)
	} else {
		fmt.Fprintf(&overview, "📊 采集源：成功 <b>%d</b> 个\n",
			payload.SuccessSources)
	}
	fmt.Fprintf(&overview, "🎬 采集数量：更新 <b>%d</b> 部\n", headerChanged)
	if errMsg := strings.TrimSpace(payload.FinalizeError); errMsg != "" {
		fmt.Fprintf(&overview, "⚠️ 收尾: <code>%s</code>\n", html.EscapeString(truncateRunes(errMsg, 200)))
	}

	// 仅列出失败/异常的源
	var failedSources []model.SourceNotifyResult
	for _, src := range payload.Sources {
		if src.Status != "done" || strings.TrimSpace(src.Error) != "" {
			failedSources = append(failedSources, src)
		}
	}
	if len(failedSources) > 0 {
		overview.WriteByte('\n')
		for _, src := range failedSources {
			name := strings.TrimSpace(src.SourceName)
			if name == "" {
				name = "未命名源"
			}
			name = truncateRunes(name, 40)
			grade := gradeLabel(src.Grade)
			errMsg := strings.TrimSpace(src.Error)
			if errMsg == "" {
				errMsg = "采集失败"
			}
			fmt.Fprintf(&overview, "❌ <b>%s</b>（%s）: <code>%s</code>\n",
				html.EscapeString(name),
				html.EscapeString(grade),
				html.EscapeString(truncateRunes(errMsg, 120)),
			)
		}
	}

	if listN > 0 {
		pages := (listN + pageSize - 1) / pageSize
		fmt.Fprintf(&overview, "\n📋 更新列表 <b>%d</b> 部 · <b>%d</b> 页\n", listN, pages)
	}
	return overview.String()
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

// formatSourceConfigChanged 采集源配置变更通知（新增/删除/主站切换/启用停用等）。
func formatSourceConfigChanged(siteName, sourceName, sourceID string, changes []string, at time.Time) []string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 🔧 采集源配置变更</b>\n", formatTitlePrefix(siteName))
	fmt.Fprintf(&b, "<b>📌 站点:</b> %s", html.EscapeString(sourceName))
	if sourceID != "" {
		fmt.Fprintf(&b, " (<code>%s</code>)", html.EscapeString(sourceID))
	}
	b.WriteByte('\n')
	if len(changes) > 0 {
		b.WriteString("<b>🛠 变更:</b>\n")
		for _, ch := range changes {
			fmt.Fprintf(&b, "· %s\n", html.EscapeString(ch))
		}
	} else {
		b.WriteString("<b>🛠 操作:</b> 配置已更新\n")
	}
	if !at.IsZero() {
		fmt.Fprintf(&b, "<b>🕒 时间:</b> <code>%s</code>\n", html.EscapeString(at.In(time.FixedZone("CST", 8*3600)).Format(time.DateTime)))
	}
	return []string{b.String()}
}

// formatSourceConfigChangeItemBlock 单条源变更块（不含页眉/时间）。
func formatSourceConfigChangeItemBlock(item SourceConfigChangeItem) string {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>📌 站点:</b> %s", html.EscapeString(strings.TrimSpace(item.SourceName)))
	if id := strings.TrimSpace(item.SourceID); id != "" {
		fmt.Fprintf(&b, " (<code>%s</code>)", html.EscapeString(id))
	}
	b.WriteByte('\n')
	if len(item.Changes) > 0 {
		b.WriteString("<b>🛠 变更:</b>\n")
		for _, ch := range item.Changes {
			fmt.Fprintf(&b, "· %s\n", html.EscapeString(ch))
		}
	} else {
		b.WriteString("<b>🛠 操作:</b> 配置已更新\n")
	}
	return b.String()
}

// formatSourceConfigsChanged 批量采集源配置变更聚合通知（批量启用/禁用等）。
// 按条目贪心装箱，单条消息不超过 Telegram 4096 上限；多页时带「N 个 · i/m」页眉，不截断丢源。
func formatSourceConfigsChanged(siteName string, items []SourceConfigChangeItem, at time.Time) []string {
	if len(items) == 0 {
		return nil
	}
	blocks := make([]string, 0, len(items))
	for _, item := range items {
		blocks = append(blocks, formatSourceConfigChangeItemBlock(item))
	}

	// 预留页眉 + 时间戳空间，正文按块装箱
	const reserveRunes = 180
	bodyLimit := telegramMaxMessageLen - reserveRunes
	if bodyLimit < 256 {
		bodyLimit = telegramMaxMessageLen / 2
	}

	var groups [][]string
	var cur []string
	curLen := 0
	for _, block := range blocks {
		bl := utf8.RuneCountInString(block)
		need := bl
		if len(cur) > 0 {
			need += 1 // 块间换行
		}
		if len(cur) > 0 && curLen+need > bodyLimit {
			groups = append(groups, cur)
			cur = nil
			curLen = 0
			need = bl
		}
		cur = append(cur, block)
		curLen += need
	}
	if len(cur) > 0 {
		groups = append(groups, cur)
	}

	total, pages := len(items), len(groups)
	out := make([]string, 0, pages)
	for i, group := range groups {
		var b strings.Builder
		if pages > 1 {
			fmt.Fprintf(&b, "<b>%s 🔧 采集源配置变更（批量 %d 个 · %d/%d）</b>\n",
				formatTitlePrefix(siteName), total, i+1, pages)
		} else {
			fmt.Fprintf(&b, "<b>%s 🔧 采集源配置变更（批量 %d 个）</b>\n",
				formatTitlePrefix(siteName), total)
		}
		for _, block := range group {
			b.WriteByte('\n')
			b.WriteString(block)
		}
		// 时间戳只挂末页，避免每页重复
		if !at.IsZero() && i == pages-1 {
			fmt.Fprintf(&b, "\n<b>🕒 时间:</b> <code>%s</code>\n",
				html.EscapeString(at.In(time.FixedZone("CST", 8*3600)).Format(time.DateTime)))
		}
		part := b.String()
		// 单块极端超长时兜底拆分（最多 3 段并截断提示）
		if utf8.RuneCountInString(part) > telegramMaxMessageLen {
			out = append(out, splitTelegramMessages(part)...)
		} else {
			out = append(out, part)
		}
	}
	return out
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

func severityBadge(level model.Severity) string {
	switch level {
	case model.SeverityInfo:
		return "ℹ️ INFO"
	case model.SeverityNotice:
		return "📢 NOTICE"
	case model.SeverityWarn:
		return "⚠️ WARN"
	case model.SeverityError:
		return "🚨 ERROR"
	case model.SeverityCritical:
		return "🔥 CRITICAL"
	default:
		return string(level)
	}
}

// formatEventHTML 统一事件格式化。
func formatEventHTML(siteName string, evt model.NotifyEvent) string {
	var b strings.Builder
	prefix := formatTitlePrefix(siteName)
	badge := severityBadge(evt.Severity)
	title := strings.TrimSpace(evt.Title)
	if title == "" {
		title = evt.Key
	}
	fmt.Fprintf(&b, "<b>%s %s</b> [%s]\n", prefix, html.EscapeString(title), badge)
	if summary := strings.TrimSpace(evt.Summary); summary != "" {
		fmt.Fprintf(&b, "%s\n", html.EscapeString(summary))
	}
	if len(evt.Data) > 0 {
		b.WriteString("\n📋 <b>详细信息:</b>\n")
		for k, v := range evt.Data {
			valStr := fmt.Sprintf("%v", v)
			fmt.Fprintf(&b, "· <code>%s</code>: %s\n", html.EscapeString(k), html.EscapeString(valStr))
		}
	}
	t := evt.Timestamp
	if t.IsZero() {
		t = time.Now()
	}
	fmt.Fprintf(&b, "\n🕒 %s\n", t.Format("2006-01-02 15:04:05"))
	return b.String()
}
