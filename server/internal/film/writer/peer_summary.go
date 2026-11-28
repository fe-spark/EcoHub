package writer

import (
	"math"
	"sort"
	"strings"
	"time"

	"server/internal/film/snapshot"
	"server/internal/model"
	"server/internal/sourcecatalog"

	"gorm.io/gorm"
)

func refreshRemarksAndPlaySummaryTx(tx *gorm.DB, mid int64) error {
	if mid <= 0 {
		return nil
	}
	return refreshRemarksAndPlaySummaryWithStampTx(tx, []int64{mid}, map[int64]struct{}{mid: {}}, sourcecatalog.GetCollectSourceListTx(tx))
}

func pickPlaylistRemarks(playLines []model.FilmSourcePlaylist, sources []model.FilmSource) string {
	if len(playLines) == 0 {
		return ""
	}
	orderByID := make(map[string]int, len(sources))
	enabledByID := make(map[string]bool, len(sources))
	for idx, source := range sources {
		orderByID[source.Id] = idx
		if source.State {
			enabledByID[source.Id] = true
		}
	}
	candidates := playLines
	if len(enabledByID) > 0 {
		enabledLines := make([]model.FilmSourcePlaylist, 0, len(playLines))
		for _, line := range playLines {
			if enabledByID[line.SourceId] {
				enabledLines = append(enabledLines, line)
			}
		}
		if len(enabledLines) > 0 {
			candidates = enabledLines
		}
	}
	bestLine := candidates[0]
	for i := 1; i < len(candidates); i++ {
		curr := candidates[i]
		if curr.EpisodeCount > bestLine.EpisodeCount {
			bestLine = curr
			continue
		}
		if curr.EpisodeCount != bestLine.EpisodeCount {
			continue
		}
		oCurr := math.MaxInt
		if idx, ok := orderByID[curr.SourceId]; ok {
			oCurr = idx
		}
		oBest := math.MaxInt
		if idx, ok := orderByID[bestLine.SourceId]; ok {
			oBest = idx
		}
		if oCurr < oBest || (oCurr == oBest && curr.GroupIndex < bestLine.GroupIndex) {
			bestLine = curr
		}
	}
	return bestLine.LastEpisode
}

// refreshRemarksAndPlaySummaryWithStampTx 批量刷新备注与播放摘要；仅 stampMids 中的存量追更影片将 update_stamp 刷为当前采集时间，新片保留源站 vod_time。
func refreshRemarksAndPlaySummaryWithStampTx(tx *gorm.DB, mids []int64, stampMids map[int64]struct{}, sources []model.FilmSource) error {
	ordered := uniqueMIDs(mids)
	if len(ordered) == 0 {
		return nil
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })

	var playLines []model.FilmSourcePlaylist
	if err := tx.Select("mid", "source_id", "line_kind", "group_index", "group_name", "episode_count", "last_episode").
		Where("mid IN ? AND line_kind = ?", ordered, "play").
		Find(&playLines).Error; err != nil {
		return err
	}
	byMid := make(map[int64][]model.FilmSourcePlaylist, len(ordered))
	for _, line := range playLines {
		byMid[line.Mid] = append(byMid[line.Mid], line)
	}

	now := time.Now()
	stamp := now.Unix()
	remarksCases := make([]string, 0, len(ordered))
	remarksArgs := make([]any, 0, len(ordered)*2)
	summaryCases := make([]string, 0, len(ordered))
	summaryArgs := make([]any, 0, len(ordered)*2)
	stampCases := make([]string, 0, len(ordered))
	stampArgs := make([]any, 0, len(ordered)*2)

	for _, mid := range ordered {
		lines := byMid[mid]
		remarksCases = append(remarksCases, "WHEN mid = ? THEN ?")
		remarksArgs = append(remarksArgs, mid, pickPlaylistRemarks(lines, sources))
		summaryCases = append(summaryCases, "WHEN mid = ? THEN ?")
		summaryArgs = append(summaryArgs, mid, snapshot.BuildPlayFromSummaryFromPlaylists(lines, sources))
		if _, ok := stampMids[mid]; ok {
			stampCases = append(stampCases, "WHEN mid = ? THEN ?")
			stampArgs = append(stampArgs, mid, stamp)
		}
	}

	setExprs := []string{
		"remarks = CASE " + strings.Join(remarksCases, " ") + " END",
		"play_from_summary = CASE " + strings.Join(summaryCases, " ") + " END",
		"updated_at = ?",
	}
	args := make([]any, 0, len(remarksArgs)+len(summaryArgs)+len(stampArgs)+3)
	args = append(args, remarksArgs...)
	args = append(args, summaryArgs...)
	args = append(args, now)
	if len(stampCases) > 0 {
		setExprs = append(setExprs, "update_stamp = CASE "+strings.Join(stampCases, " ")+" ELSE update_stamp END")
		args = append(args, stampArgs...)
	}
	if tx != nil && tx.Statement != nil {
		if recordID := CollectRecordIDFromContext(tx.Statement.Context); recordID > 0 {
			setExprs = append(setExprs, "last_record_id = ?")
			args = append(args, recordID)
		}
	}
	args = append(args, ordered)
	query := "UPDATE " + model.TableFilmIndex + " SET " + strings.Join(setExprs, ", ") + " WHERE mid IN ?"
	return tx.Exec(query, args...).Error
}
