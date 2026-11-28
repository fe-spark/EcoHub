package notify

import (
	"context"
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

const maxFailedSourcesInOverview = 8

func isNotifySourceAbnormal(s model.SourceNotifyResult) bool {
	return s.Status == model.CollectStatusFailed ||
		s.Status == model.CollectRecordStatusPartial ||
		s.Status == model.CollectRecordStatusInterrupted ||
		s.Error != ""
}

func formatBatchOverviewHTML(siteName string, payload model.CollectBatchNotifyPayload, primaryName string, primaryTotal, visibleCount int) string {
	durSec := int64(payload.FinishedAt.Sub(payload.StartedAt).Seconds())
	if durSec < 0 {
		durSec = 0
	}
	successSrc, failedSrc := 0, 0
	var failedItems []model.SourceNotifyResult
	for _, s := range payload.Sources {
		if isNotifySourceAbnormal(s) {
			failedSrc++
			failedItems = append(failedItems, s)
		} else {
			successSrc++
		}
	}

	var b strings.Builder
	fmt.Fprintf(&b, "<b>%s 采集汇总</b>\n", formatTitlePrefix(siteName))
	fmt.Fprintf(&b, "⚡ 触发：%s · ⏱ 耗时：%s\n",
		html.EscapeString(triggerLabel(payload.Trigger)),
		html.EscapeString(formatDurationSec(durSec)),
	)
	if failedSrc > 0 {
		fmt.Fprintf(&b, "📊 站点：成功 <b>%d</b> 个，异常 <b>%d</b> 个\n", successSrc, failedSrc)
	} else {
		fmt.Fprintf(&b, "📊 站点：成功 <b>%d</b> 个\n", successSrc)
	}
	if name := strings.TrimSpace(primaryName); name != "" {
		if visibleCount < 0 {
			fmt.Fprintf(&b, "⭐ 主站：<b>%s</b> · 统计超时\n", html.EscapeString(singleLine(name)))
		} else {
			fmt.Fprintf(&b, "⭐ 主站：<b>%s</b> · 本次更新 <b>%d</b> 部（前台可见 <b>%d</b> 部）\n",
				html.EscapeString(singleLine(name)), primaryTotal, visibleCount)
		}
	} else if visibleCount < 0 {
		b.WriteString("🎬 前台可见：统计超时\n")
	} else {
		fmt.Fprintf(&b, "🎬 前台可见：<b>%d</b> 部\n", visibleCount)
	}

	if len(failedItems) > 0 {
		b.WriteString("\n⚠️ <b>异常站点：</b>\n")
		limit := len(failedItems)
		if limit > maxFailedSourcesInOverview {
			limit = maxFailedSourcesInOverview
		}
		for i := 0; i < limit; i++ {
			s := failedItems[i]
			name := singleLine(s.SourceName)
			if name == "" {
				name = singleLine(s.SourceID)
			}
			var parts []string
			if s.FailedCnt > 0 {
				parts = append(parts, fmt.Sprintf("失败 %d 页", s.FailedCnt))
			}
			if msg := strings.TrimSpace(singleLine(s.Error)); msg != "" {
				parts = append(parts, truncateRunes(msg, 80))
			}
			if len(parts) == 0 {
				parts = append(parts, "执行失败")
			}
			fmt.Fprintf(&b, "· <b>%s</b>：%s\n", html.EscapeString(name), html.EscapeString(strings.Join(parts, " · ")))
		}
		if len(failedItems) > limit {
			fmt.Fprintf(&b, "· <i>其余 %d 个异常站点请在后台查看</i>\n", len(failedItems)-limit)
		}
	}

	if primaryTotal > 0 {
		b.WriteString("\n👇 选择主站分类查看更新片单")
	}
	return strings.TrimRight(b.String(), "\n")
}

func isTelegramSupportedWebURL(raw string) bool {
	lower := strings.ToLower(strings.TrimSpace(raw))
	return strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://")
}

func buildManageRecordURL(siteURL, collectBatchID string) string {
	base := strings.TrimRight(strings.TrimSpace(siteURL), "/")
	if !isTelegramSupportedWebURL(base) {
		return ""
	}
	if id := strings.TrimSpace(collectBatchID); id != "" {
		return fmt.Sprintf("%s/manage/collect/record?batchId=%s", base, id)
	}
	return base + "/manage/collect/record"
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

func formatPrimaryFilmListPageHTML(ctx context.Context, sess model.NotifyBatchSession, catIdx, page int) (string, int, int, error) {
	if ctx == nil {
		ctx = context.Background()
	}
	catTitle := "全部"
	total := sess.PrimaryTotal
	if catIdx >= 0 && catIdx < len(sess.Categories) {
		selectedCat := sess.Categories[catIdx]
		catTitle = selectedCat.CategoryName
		total = selectedCat.Count
	}
	curPage, totalPages, start, end := clampPageRange(total, sess.PageSize, page)
	var items []model.NotifyFilmItem
	if total > 0 && sess.CollectRecordID > 0 && strings.TrimSpace(sess.PrimarySourceID) != "" {
		matcher, err := loadSourceCategoryMatcher(sess.PrimarySourceID)
		if err != nil {
			return "", curPage, totalPages, err
		}
		items, err = LoadBatchPrimaryCategoryPage(
			ctx,
			matcher.buildPageQuery(sess.CollectRecordID, sess.PrimarySourceID, sess.Categories, catIdx, end-start, start),
		)
		if err != nil {
			return "", curPage, totalPages, err
		}
		// 若旧批次部分影片被后续批次覆写 last_record_id，导致深页偏移查空，回退首页避免误导空页。
		if len(items) == 0 && curPage > 1 {
			curPage, totalPages, start = 1, 1, 0
			pageSize := sess.PageSize
			if pageSize <= 0 {
				pageSize = model.DefaultNotifyPageSize
			}
			items, err = LoadBatchPrimaryCategoryPage(
				ctx,
				matcher.buildPageQuery(sess.CollectRecordID, sess.PrimarySourceID, sess.Categories, catIdx, pageSize, 0),
			)
			if err != nil {
				return "", curPage, totalPages, err
			}
		}
	}
	srcTitle := singleLine(sess.PrimarySourceName)
	if srcTitle == "" {
		srcTitle = sess.PrimarySourceID
	}
	title := catTitle
	if srcTitle != "" {
		title = fmt.Sprintf("%s · %s", srcTitle, catTitle)
	}
	text, err := formatFilmItemsPageHTML(sess.SiteName, title, curPage, totalPages, start, total, items)
	return text, curPage, totalPages, err
}

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
