package writer

import (
	"sort"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/utils"

	"gorm.io/gorm"
)

func savePagePlaylistsTx(tx *gorm.DB, sourceID string, items []peerPageItem, storedUnix map[int64]int64) error {
	mids := make([]int64, 0, len(items))
	seen := make(map[int64]struct{}, len(items))
	for i := range items {
		if items[i].mid <= 0 {
			continue
		}
		if _, ok := seen[items[i].mid]; ok {
			continue
		}
		seen[items[i].mid] = struct{}{}
		mids = append(mids, items[i].mid)
	}
	if len(mids) == 0 {
		return nil
	}
	sort.Slice(mids, func(i, j int) bool { return mids[i] < mids[j] })

	var oldLines []model.FilmSourcePlaylist
	if err := playlistMetaQuery(tx).
		Where("source_id = ? AND mid IN ?", sourceID, mids).
		Find(&oldLines).Error; err != nil {
		return err
	}
	oldByMid := make(map[int64][]model.FilmSourcePlaylist, len(mids))
	for _, line := range oldLines {
		oldByMid[line.Mid] = append(oldByMid[line.Mid], line)
	}

	// 同一 vod 在本页出现多次时，后一条覆盖前一条。不同 vod 不会进到这里。
	lastIdx := make(map[int64]int, len(mids))
	for i := range items {
		if items[i].mid > 0 {
			lastIdx[items[i].mid] = i
		}
	}
	writers := make([]int, 0, len(lastIdx))
	for _, idx := range lastIdx {
		writers = append(writers, idx)
	}
	sort.Slice(writers, func(i, j int) bool {
		if items[writers[i]].mid == items[writers[j]].mid {
			return writers[i] < writers[j]
		}
		return items[writers[i]].mid < items[writers[j]].mid
	})

	claims := make([]vodTimeClaim, len(writers))
	for i, idx := range writers {
		claims[i] = vodTimeClaim{sourceMid: items[idx].detail.Id, updateTime: items[idx].detail.UpdateTime}
	}
	won, err := claimPageVodTimesTx(tx, sourceID, claims, storedUnix)
	if err != nil {
		return err
	}

	vanished := make([]model.FilmSourcePlaylist, 0)
	upserts := make([]model.FilmSourcePlaylist, 0)
	for i, idx := range writers {
		if !won[i] {
			items[idx].stale = true
			continue
		}
		newLines := buildPlaylistsFromDetail(items[idx].mid, sourceID, items[idx].detail)
		old := oldByMid[items[idx].mid]
		if isPlaylistSignatureIdentical(old, newLines) {
			continue
		}
		items[idx].linesChanged = true
		items[idx].playChanged = playHashesChanged(old, newLines)
		vanished = append(vanished, vanishedPlaylists(old, newLines)...)
		upserts = append(upserts, newLines...)
	}
	if err := batchDeleteVanishedPlaylistsTx(tx, vanished); err != nil {
		return err
	}
	if len(upserts) == 0 {
		return nil
	}
	sortPlaylists(upserts)
	return tx.Clauses(playlistUpsertClause()).CreateInBatches(&upserts, peerPagePlaylistBatch).Error
}

func batchDeleteVanishedPlaylistsTx(tx *gorm.DB, vanished []model.FilmSourcePlaylist) error {
	if len(vanished) == 0 {
		return nil
	}
	sortPlaylists(vanished)
	if len(vanished) == 1 {
		return deletePlaylistTx(tx, vanished[0])
	}
	sourceID := vanished[0].SourceId
	placeholders := make([]string, 0, len(vanished))
	args := make([]any, 0, 1+len(vanished)*3)
	args = append(args, sourceID)
	for _, line := range vanished {
		placeholders = append(placeholders, "(?, ?, ?)")
		args = append(args, line.Mid, line.LineKind, line.GroupIndex)
	}
	return tx.Where(
		"source_id = ? AND (mid, line_kind, group_index) IN ("+strings.Join(placeholders, ",")+")",
		args...,
	).Delete(&model.FilmSourcePlaylist{}).Error
}

type vodTimeClaim struct {
	sourceMid  int64
	updateTime string
}

// claimSourceVodTimeTx 只收下不早于已存档的源站 vod_time。
func claimSourceVodTimeTx(tx *gorm.DB, sourceID string, sourceMid int64, updateTime string) (bool, error) {
	if sourceMid <= 0 {
		return true, nil
	}
	won, err := claimPageVodTimesTx(tx, sourceID, []vodTimeClaim{{sourceMid: sourceMid, updateTime: updateTime}}, nil)
	if err != nil || len(won) == 0 {
		return false, err
	}
	return won[0], nil
}

// claimPageVodTimesTx 优先复用预加载的时间戳映射，对需要推进的记录按单条 SQL CASE WHEN 批量更新。
func claimPageVodTimesTx(tx *gorm.DB, sourceID string, claims []vodTimeClaim, stored map[int64]int64) ([]bool, error) {
	won := make([]bool, len(claims))
	if len(claims) == 0 {
		return won, nil
	}
	if stored == nil {
		ids := make([]int64, 0, len(claims))
		seen := make(map[int64]struct{}, len(claims))
		for _, claim := range claims {
			if claim.sourceMid <= 0 {
				continue
			}
			if _, ok := seen[claim.sourceMid]; ok {
				continue
			}
			seen[claim.sourceMid] = struct{}{}
			ids = append(ids, claim.sourceMid)
		}
		stored = make(map[int64]int64, len(ids))
		if len(ids) > 0 {
			var rows []model.MovieSourceMapping
			if err := tx.Select("source_mid", "source_vod_unix").
				Where("source_id = ? AND source_mid IN ?", sourceID, ids).
				Find(&rows).Error; err != nil {
				return nil, err
			}
			for _, row := range rows {
				stored[row.SourceMid] = row.SourceVodUnix
			}
		}
	}

	toUpdate := make(map[int64]int64)
	for i, claim := range claims {
		if claim.sourceMid <= 0 {
			won[i] = true
			continue
		}
		current, exists := stored[claim.sourceMid]
		incoming, err := utils.ParseCollectUpdateTime(claim.updateTime)
		if err != nil {
			won[i] = !exists || current == 0
			continue
		}
		if !exists {
			if incoming > 0 {
				stored[claim.sourceMid] = incoming
			}
			won[i] = true
			continue
		}
		if incoming < current {
			continue
		}
		if incoming == current {
			won[i] = true
			continue
		}
		stored[claim.sourceMid] = incoming
		toUpdate[claim.sourceMid] = incoming
		won[i] = true
	}

	if len(toUpdate) == 0 {
		return won, nil
	}

	mids := make([]int64, 0, len(toUpdate))
	for smid := range toUpdate {
		mids = append(mids, smid)
	}
	sort.Slice(mids, func(i, j int) bool { return mids[i] < mids[j] })

	now := time.Now()
	cases := make([]string, 0, len(mids))
	args := make([]any, 0, len(mids)*3+3)
	for _, smid := range mids {
		val := toUpdate[smid]
		cases = append(cases, "WHEN source_mid = ? AND source_vod_unix < ? THEN ?")
		args = append(args, smid, val, val)
	}
	args = append(args, now, sourceID, mids)
	query := "UPDATE " + model.TableMovieSourceMapping +
		" SET source_vod_unix = CASE " + strings.Join(cases, " ") +
		" ELSE source_vod_unix END, updated_at = ? WHERE source_id = ? AND source_mid IN ?"
	if err := tx.Exec(query, args...).Error; err != nil {
		return nil, err
	}
	return won, nil
}
