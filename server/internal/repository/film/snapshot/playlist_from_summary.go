package snapshot

import (
	"log"
	"sort"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// BuildPlayFromSummaryFromPlaylists 从影片的多源播放列表中构建播放源摘要。
// 每条播放线路一个展示名，按站点权重降序、group_index 升序拼接。
func BuildPlayFromSummaryFromPlaylists(playlists []model.FilmSourcePlaylist, sources []model.FilmSource) string {
	if len(playlists) == 0 {
		return ""
	}
	orderByID := make(map[string]int, len(sources))
	nameByID := make(map[string]string, len(sources))
	for idx, s := range sources {
		orderByID[s.Id] = idx
		nameByID[s.Id] = s.Name
	}

	// 统计各源播放线路数量
	countBySource := make(map[string]int)
	playLines := make([]model.FilmSourcePlaylist, 0, len(playlists))
	for _, p := range playlists {
		if p.LineKind == "play" {
			countBySource[p.SourceId]++
			playLines = append(playLines, p)
		}
	}
	if len(playLines) == 0 {
		return ""
	}

	// 按站点列表顺序升序、站点ID升序、group_index 升序排序
	sort.SliceStable(playLines, func(i, j int) bool {
		oi, okI := orderByID[playLines[i].SourceId]
		if !okI {
			oi = 999999
		}
		oj, okJ := orderByID[playLines[j].SourceId]
		if !okJ {
			oj = 999999
		}
		if oi != oj {
			return oi < oj
		}
		if playLines[i].SourceId != playLines[j].SourceId {
			return playLines[i].SourceId < playLines[j].SourceId
		}
		return playLines[i].GroupIndex < playLines[j].GroupIndex
	})

	var names []string
	seen := make(map[string]struct{}, len(playLines))
	for _, line := range playLines {
		sName := nameByID[line.SourceId]
		if sName == "" {
			sName = line.SourceId
		}
		displayName := shared.BuildDisplaySourceName(sName, line.GroupName, line.GroupIndex, countBySource[line.SourceId])
		displayName = strings.TrimSpace(displayName)
		if displayName == "" {
			continue
		}
		if _, ok := seen[displayName]; ok {
			continue
		}
		seen[displayName] = struct{}{}
		names = append(names, displayName)
	}

	return strings.Join(names, "$$$")
}

func RefreshPlayFromSummaryByIndexesTx(tx *gorm.DB, infos []model.FilmIndex) error {
	if len(infos) == 0 {
		return nil
	}

	mids := make([]int64, 0, len(infos))
	seenMid := make(map[int64]struct{}, len(infos))
	for _, info := range infos {
		if info.Mid <= 0 {
			continue
		}
		if _, ok := seenMid[info.Mid]; ok {
			continue
		}
		seenMid[info.Mid] = struct{}{}
		mids = append(mids, info.Mid)
	}
	if len(mids) == 0 {
		return nil
	}

	startedAt := time.Now()
	var playlists []model.FilmSourcePlaylist
	if err := tx.Where("mid IN ? AND line_kind = ?", mids, "play").Find(&playlists).Error; err != nil {
		return err
	}

	playlistsByMid := make(map[int64][]model.FilmSourcePlaylist, len(mids))
	for _, pl := range playlists {
		playlistsByMid[pl.Mid] = append(playlistsByMid[pl.Mid], pl)
	}

	sources := support.GetCollectSourceList()
	summaries := make(map[int64]string, len(mids))
	for _, mid := range mids {
		summaries[mid] = BuildPlayFromSummaryFromPlaylists(playlistsByMid[mid], sources)
	}

	if err := batchUpdatePlayFromSummariesTx(tx, summaries); err != nil {
		return err
	}
	log.Printf("[PlaySummaryRefresh] mid_count=%d cost=%s", len(mids), time.Since(startedAt))
	return nil
}

func batchUpdatePlayFromSummariesTx(tx *gorm.DB, summaries map[int64]string) error {
	if len(summaries) == 0 {
		return nil
	}

	caseExpr := "CASE mid"
	mids := make([]int64, 0, len(summaries))
	args := make([]any, 0, len(summaries)*2)
	for mid, summary := range summaries {
		if mid <= 0 {
			continue
		}
		caseExpr += " WHEN ? THEN ?"
		args = append(args, mid, summary)
		mids = append(mids, mid)
	}
	if len(mids) == 0 {
		return nil
	}
	caseExpr += " ELSE play_from_summary END"

	return tx.Model(&model.FilmIndex{}).
		Where("mid IN ?", mids).
		Update("play_from_summary", clause.Expr{SQL: caseExpr, Vars: args}).Error
}
