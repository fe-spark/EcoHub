package notify

import (
	"context"
	"errors"
	"fmt"
	"html"
	"server/internal/model"
	"server/internal/repobase"
	"strings"
	"unicode/utf8"
)

func formatTitlePrefix(siteName string) string {
	siteName = strings.TrimSpace(siteName)
	if siteName == "" || strings.EqualFold(siteName, "EcoHub") {
		return "[EcoHub]"
	}
	return fmt.Sprintf("[EcoHub · %s]", html.EscapeString(siteName))
}

func triggerLabel(trigger string) string {
	switch trigger {
	case model.CollectTriggerCron:
		return "定时任务"
	case model.CollectTriggerSingleUpdate:
		return "单片更新"
	case model.CollectTriggerManual:
		return "手动采集"
	default:
		if strings.TrimSpace(trigger) == "" {
			return "采集任务"
		}
		return trigger
	}
}

func formatDurationSec(sec int64) string {
	if sec <= 0 {
		return "1秒"
	}
	if sec < 60 {
		return fmt.Sprintf("%d秒", sec)
	}
	m := sec / 60
	s := sec % 60
	if m < 60 {
		if s == 0 {
			return fmt.Sprintf("%d分", m)
		}
		return fmt.Sprintf("%d分%d秒", m, s)
	}
	h := m / 60
	rm := m % 60
	return fmt.Sprintf("%d时%d分", h, rm)
}

func formatBatchOverviewHTML(siteName string, payload model.CollectBatchNotifyPayload, primaryName string, updatedCount int) string {
	durSec := int64(payload.FinishedAt.Sub(payload.StartedAt).Seconds())
	if durSec < 0 {
		durSec = 0
	}
	successSrc, failedSrc, totalStored := 0, 0, 0
	for _, s := range payload.Sources {
		if s.Status == model.CollectStatusFailed || s.Error != "" {
			failedSrc++
		} else {
			successSrc++
		}
		totalStored += s.FilmsTotal
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 采集汇总</b>\n", formatTitlePrefix(siteName))
	fmt.Fprintf(&b, "⚡ 触发：%s · ⏱ 耗时：%s\n",
		html.EscapeString(triggerLabel(payload.Trigger)),
		html.EscapeString(formatDurationSec(durSec)),
	)
	if failedSrc > 0 {
		fmt.Fprintf(&b, "📊 站点：成功 <b>%d</b> 个，失败 <b>%d</b> 个 · 共入库 <b>%d</b> 部\n", successSrc, failedSrc, totalStored)
	} else {
		fmt.Fprintf(&b, "📊 站点：成功 <b>%d</b> 个 · 共入库 <b>%d</b> 部\n", successSrc, totalStored)
	}
	if name := strings.TrimSpace(primaryName); name != "" {
		if updatedCount < 0 {
			fmt.Fprintf(&b, "⭐ 主站：<b>%s</b> · 前台可见：统计超时\n", html.EscapeString(singleLine(name)))
		} else {
			fmt.Fprintf(&b, "⭐ 主站：<b>%s</b> · 前台可见 <b>%d</b> 部\n", html.EscapeString(singleLine(name)), updatedCount)
		}
	} else if updatedCount < 0 {
		b.WriteString("🎬 前台可见：统计超时\n")
	} else {
		fmt.Fprintf(&b, "🎬 前台可见：<b>%d</b> 部\n", updatedCount)
	}
	if len(payload.Sources) > 0 {
		b.WriteString("\n👇 点击站点查看分类与片单")
	}
	return strings.TrimRight(b.String(), "\n")
}

func sourceStatusIconAndLabel(status, errMsg string) (string, string) {
	if status == model.CollectStatusFailed || errMsg != "" {
		return "❌", "失败"
	}
	if status == model.CollectStatusStopped {
		return "⏹", "已停止"
	}
	return "✅", "完成"
}

func formatSourceDetailHTML(siteName string, src model.SourceNotifyResult, isPrimary bool, filmCount int) string {
	icon, statusLabel := sourceStatusIconAndLabel(src.Status, src.Error)
	title := html.EscapeString(singleLine(src.SourceName))
	if title == "" {
		title = html.EscapeString(singleLine(src.SourceID))
	}
	primaryBadge := ""
	if isPrimary {
		primaryBadge = "⭐ "
	}
	displayTotal := src.FilmsTotal
	if filmCount > displayTotal {
		displayTotal = filmCount
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 站点明细 · %s%s</b>\n", formatTitlePrefix(siteName), primaryBadge, title)
	fmt.Fprintf(&b, "%s 状态：%s · 🎬 入库：<b>%d</b> 部", icon, statusLabel, displayTotal)
	if src.PageTotal > 0 {
		fmt.Fprintf(&b, " · 📄 页数：%d/%d", src.SuccessCnt, src.PageTotal)
	}
	b.WriteString("\n")
	if src.FailedCnt > 0 || src.CategorySkip > 0 {
		var extras []string
		if src.FailedCnt > 0 {
			extras = append(extras, fmt.Sprintf("失败 %d 页", src.FailedCnt))
		}
		if src.CategorySkip > 0 {
			extras = append(extras, fmt.Sprintf("跳过未映射 %d 部", src.CategorySkip))
		}
		b.WriteString("ℹ️ " + strings.Join(extras, "，") + "\n")
	}
	if strings.TrimSpace(src.Error) != "" {
		fmt.Fprintf(&b, "⚠️ 原因：<code>%s</code>\n", html.EscapeString(truncateRunes(singleLine(src.Error), 300)))
	}
	if filmCount > 0 {
		b.WriteString("\n👇 选择分类查看片单")
	}
	return strings.TrimRight(b.String(), "\n")
}

func isTelegramSupportedWebURL(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

// maxNotifyListPages 限制 Telegram 内联片单最大翻页深度，避免全量批次下深 OFFSET 回表超时。
const maxNotifyListPages = 100

func clampPageRange(total, pageSize, page int) (curPage, totalPages, start, end int) {
	if pageSize <= 0 {
		pageSize = model.DefaultNotifyPageSize
	}
	totalPages = 1
	if total > 0 {
		totalPages = (total + pageSize - 1) / pageSize
		if totalPages > maxNotifyListPages {
			totalPages = maxNotifyListPages
		}
	}
	curPage = page
	if curPage < 1 {
		curPage = 1
	}
	if curPage > totalPages {
		curPage = totalPages
	}
	if total == 0 {
		return curPage, totalPages, 0, 0
	}
	start = (curPage - 1) * pageSize
	end = start + pageSize
	if end > total {
		end = total
	}
	return curPage, totalPages, start, end
}

func formatFilmItemsPageHTML(siteName, title string, page, totalPages, start, total int, items []model.NotifyFilmItem) (string, error) {
	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 更新片单 · %s</b>\n", formatTitlePrefix(siteName), html.EscapeString(title))
	if total == 0 || len(items) == 0 {
		b.WriteString("<i>暂无更新影片</i>\n")
		return b.String(), nil
	}
	fmt.Fprintf(&b, "📄 第 <b>%d/%d</b> 页 · <code>%d–%d</code> / 共 <b>%d</b> 部\n\n",
		page, totalPages, start+1, start+len(items), total)

	basic, err := repobase.GetSiteBasic()
	if err != nil {
		return "", err
	}
	siteURL := strings.TrimRight(strings.TrimSpace(basic.SiteURL), "/")
	canLink := isTelegramSupportedWebURL(siteURL)
	for _, f := range items {
		name := truncateRunes(strings.TrimSpace(f.Name), 60)
		if name == "" {
			name = fmt.Sprintf("#%d", f.Mid)
		}
		if canLink && f.Mid > 0 {
			playHref := fmt.Sprintf("%s/play?id=%d", siteURL, f.Mid)
			fmt.Fprintf(&b, "· <a href=\"%s\">%s</a> (<code>#%d</code>)\n",
				html.EscapeString(playHref), html.EscapeString(name), f.Mid)
		} else {
			fmt.Fprintf(&b, "· %s (<code>#%d</code>)\n", html.EscapeString(name), f.Mid)
		}
	}
	return b.String(), nil
}

func formatSourceFilmListPageHTML(ctx context.Context, sess model.NotifyBatchSession, srcIdx, catIdx, page int) (string, int, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	if srcIdx < 0 || srcIdx >= len(sess.Sources) {
		return "", 1, 1, errors.New("站点不存在")
	}
	src := sess.Sources[srcIdx]
	catTitle := "全部"
	total := src.AllCount
	if catIdx >= 0 && catIdx < len(src.Categories) {
		selectedCat := src.Categories[catIdx]
		catTitle = selectedCat.CategoryName
		total = selectedCat.Count
	}
	curPage, totalPages, start, end := clampPageRange(total, sess.PageSize, page)
	var items []model.NotifyFilmItem
	if total > 0 && strings.TrimSpace(sess.CollectBatchID) != "" {
		matcher, err := loadSourceCategoryMatcher(src.SourceID)
		if err != nil {
			return "", curPage, totalPages, err
		}
		items, err = LoadBatchSourceCategoryPage(
			ctx,
			matcher.buildPageQuery(sess.CollectBatchID, src.SourceID, src.Categories, catIdx, end-start, start),
		)
		if err != nil {
			return "", curPage, totalPages, err
		}
	}
	srcTitle := singleLine(src.SourceName)
	if srcTitle == "" {
		srcTitle = src.SourceID
	}
	text, err := formatFilmItemsPageHTML(sess.SiteName, fmt.Sprintf("%s · %s", srcTitle, catTitle), curPage, totalPages, start, total, items)
	return text, curPage, totalPages, err
}

const maxSiteButtonsPerPage = 90

func safeTruncateHTML(htmlText string, maxRunes int) string {
	if maxRunes <= 0 || utf8.RuneCountInString(htmlText) <= maxRunes {
		return htmlText
	}
	budget := maxRunes - 16
	if budget <= 0 {
		return html.EscapeString(truncateRunes(htmlTagStripRe.ReplaceAllString(htmlText, ""), maxRunes))
	}
	lines := strings.Split(htmlText, "\n")
	var kept []string
	used := 0
	for _, line := range lines {
		lineRunes := utf8.RuneCountInString(line)
		sep := 0
		if len(kept) > 0 {
			sep = 1
		}
		if used+sep+lineRunes > budget {
			break
		}
		kept = append(kept, line)
		used += sep + lineRunes
	}
	if len(kept) == 0 {
		// 单行本身超长时，剥离标签后做纯文本转义截断作为兜底
		plain := html.UnescapeString(htmlTagStripRe.ReplaceAllString(htmlText, ""))
		return html.EscapeString(truncateRunes(plain, budget)) + "\n..."
	}
	return strings.Join(kept, "\n") + "\n..."
}

func truncateRunes(s string, max int) string {
	if max <= 0 || utf8.RuneCountInString(s) <= max {
		return s
	}
	r := []rune(s)
	return string(r[:max]) + "…"
}

// 定时任务失败原因的截断上限：多步错误按行拼接，逐行截断，避免第一行过长把后面的步骤挤掉。
const (
	cronReasonLineMax  = 200
	cronReasonTotalMax = 600
)

func singleLine(s string) string {
	s = strings.ReplaceAll(s, "\r\n", " ")
	s = strings.ReplaceAll(s, "\n", " ")
	s = strings.ReplaceAll(s, "\r", " ")
	return strings.TrimSpace(s)
}

// formatReasonCodeLines 按行截断并各自包裹 <code>，保证多行错误被 safeTruncateHTML 截断后标签仍自闭合。
func formatReasonCodeLines(s string, lineMax, totalMax int) string {
	text := truncateReasonLines(s, lineMax, totalMax)
	if text == "" {
		return "<code></code>"
	}
	lines := strings.Split(text, "\n")
	for i, line := range lines {
		lines[i] = "<code>" + html.EscapeString(line) + "</code>"
	}
	return strings.Join(lines, "\n")
}

// truncateReasonLines 按行截断（按 rune 计数，不会截坏中文），再整体截断。空行丢弃。
func truncateReasonLines(s string, lineMax, totalMax int) string {
	lines := strings.Split(strings.TrimSpace(s), "\n")
	kept := make([]string, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" {
			continue
		}
		kept = append(kept, truncateRunes(line, lineMax))
	}
	return truncateRunes(strings.Join(kept, "\n"), totalMax)
}
