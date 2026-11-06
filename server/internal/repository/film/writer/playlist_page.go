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
	for _, idx := range writers {
		won, err := claimSourceVodTimeTx(tx, sourceID, items[idx].detail.Id, items[idx].detail.UpdateTime)
		if err != nil {
			return err
		}
		if !won {
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

// claimSourceVodTimeTx 只收下不早于已存档的源站 vod_time。更旧的页不是这次的更新，不能覆盖新片单。
// 时间相同或更新时返回 true，再由片单签名判断内容是否真的变了。
func claimSourceVodTimeTx(tx *gorm.DB, sourceID string, sourceMid int64, updateTime string) (bool, error) {
	if sourceMid <= 0 {
		return true, nil
	}
	var stored int64
	if err := tx.Model(&model.MovieSourceMapping{}).
		Where("source_id = ? AND source_mid = ?", sourceID, sourceMid).
		Pluck("source_vod_unix", &stored).Error; err != nil {
		return false, err
	}
	incoming, err := utils.ParseCollectUpdateTime(updateTime)
	if err != nil {
		return stored == 0, nil
	}
	if incoming < stored {
		return false, nil
	}
	if incoming == stored {
		return true, nil
	}
	res := tx.Model(&model.MovieSourceMapping{}).
		Where("source_id = ? AND source_mid = ? AND source_vod_unix < ?", sourceID, sourceMid, incoming).
		Updates(map[string]any{
			"source_vod_unix": incoming,
			"updated_at":      time.Now(),
		})
	if res.Error != nil {
		return false, res.Error
	}
	return res.RowsAffected > 0, nil
}
