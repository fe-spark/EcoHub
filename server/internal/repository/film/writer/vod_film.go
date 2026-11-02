package writer

import (
	"server/internal/model"

	"gorm.io/gorm"
)

// vodKeepsMappedFilmTx 站内身份是采集站 + vod_id。一个 mid 只能属于一个 vod。
// 已经并进去的旁路 vod 下次采集拆成自己的档案，不再和别人抢同一套播放列表。
func vodKeepsMappedFilmTx(tx *gorm.DB, sourceID string, sourceMid, mid int64) (bool, error) {
	if sourceMid <= 0 || mid <= 0 {
		return false, nil
	}
	var vods []int64
	if err := tx.Model(&model.MovieSourceMapping{}).
		Where("source_id = ? AND global_mid = ?", sourceID, mid).
		Pluck("source_mid", &vods).Error; err != nil {
		return false, err
	}
	if len(vods) <= 1 {
		return true, nil
	}
	keeper := vods[0]
	for _, id := range vods[1:] {
		if id > 0 && (keeper <= 0 || id < keeper) {
			keeper = id
		}
	}
	return sourceMid == keeper, nil
}

func detachSharedSourceVodsTx(tx *gorm.DB, sourceID string, items []peerPageItem) error {
	for i := range items {
		if items[i].mid <= 0 || items[i].film == nil {
			continue
		}
		keep, err := vodKeepsMappedFilmTx(tx, sourceID, items[i].detail.Id, items[i].mid)
		if err != nil {
			return err
		}
		if keep {
			continue
		}
		items[i].film = nil
		items[i].mid = 0
	}
	return nil
}

func markUnmappedCreates(items []peerPageItem) {
	for i := range items {
		if items[i].disabled || items[i].film != nil {
			continue
		}
		items[i].create = true
	}
}
