package writer

import (
	"sort"
	"time"

	"server/internal/model"
	"server/internal/utils"

	"gorm.io/gorm"
)

func savePagePlaylistsTx(tx *gorm.DB, sourceID string, items []peerPageItem) error {
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

	vanished := make([]model.FilmSourcePlaylist, 0)
	upserts := make([]model.FilmSourcePlaylist, 0)
	claims := make([]vodTimeClaim, len(writers))
	for i, idx := range writers {
		claims[i] = vodTimeClaim{sourceMid: items[idx].detail.Id, updateTime: items[idx].detail.UpdateTime}
	}
	won, err := claimPageVodTimesTx(tx, sourceID, claims)
	if err != nil {
		return err
	}
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
	sortPlaylists(vanished)
	for _, line := range vanished {
		if err := deletePlaylistTx(tx, line); err != nil {
			return err
		}
	}
	if len(upserts) == 0 {
		return nil
	}
	sortPlaylists(upserts)
	return tx.Clauses(playlistUpsertClause()).CreateInBatches(&upserts, peerPagePlaylistBatch).Error
}

type vodTimeClaim struct {
	sourceMid  int64
	updateTime string
}

// claimSourceVodTimeTx 只收下不早于已存档的源站 vod_time。更旧的页不是这次的更新，不能覆盖新片单。
// 时间相同或更新时返回 true，再由片单签名判断内容是否真的变了。
func claimSourceVodTimeTx(tx *gorm.DB, sourceID string, sourceMid int64, updateTime string) (bool, error) {
	if sourceMid <= 0 {
		return true, nil
	}
	won, err := claimPageVodTimesTx(tx, sourceID, []vodTimeClaim{{sourceMid: sourceMid, updateTime: updateTime}})
	if err != nil || len(won) == 0 {
		return false, err
	}
	return won[0], nil
}

// claimPageVodTimesTx 一页只查一次已存档的 vod_time。时间没变的片子不再逐条更新。
// 返回值和 claims 一一对应，同一 vod 在本页出现多次时按顺序看见前一次写下的时间。
func claimPageVodTimesTx(tx *gorm.DB, sourceID string, claims []vodTimeClaim) ([]bool, error) {
	won := make([]bool, len(claims))
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
	stored := make(map[int64]int64, len(ids))
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
	now := time.Now()
	for i, claim := range claims {
		if claim.sourceMid <= 0 {
			won[i] = true
			continue
		}
		current := stored[claim.sourceMid]
		incoming, err := utils.ParseCollectUpdateTime(claim.updateTime)
		if err != nil {
			won[i] = current == 0
			continue
		}
		if incoming < current {
			continue
		}
		if incoming == current {
			won[i] = true
			continue
		}
		res := tx.Model(&model.MovieSourceMapping{}).
			Where("source_id = ? AND source_mid = ? AND source_vod_unix < ?", sourceID, claim.sourceMid, incoming).
			Updates(map[string]any{
				"source_vod_unix": incoming,
				"updated_at":      now,
			})
		if res.Error != nil {
			return nil, res.Error
		}
		if res.RowsAffected > 0 {
			stored[claim.sourceMid] = incoming
			won[i] = true
		}
	}
	return won, nil
}
