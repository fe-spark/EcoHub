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
	return vodKeepsMappedFilm(sourceMid, vods), nil
}

// vodKeepsMappedFilm 一个 mid 只留给该站最小的 vod_id。只有一条映射时保留。
func vodKeepsMappedFilm(sourceMid int64, vods []int64) bool {
	if sourceMid <= 0 {
		return false
	}
	if len(vods) <= 1 {
		return true
	}
	keeper := vods[0]
	for _, id := range vods[1:] {
		if id > 0 && (keeper <= 0 || id < keeper) {
			keeper = id
		}
	}
	return sourceMid == keeper
}

func detachSharedSourceVodsTx(tx *gorm.DB, sourceID string, items []peerPageItem) error {
	mids := make([]int64, 0, len(items))
	seen := make(map[int64]struct{}, len(items))
	for i := range items {
		if items[i].mid <= 0 || items[i].film == nil {
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
	var rows []model.MovieSourceMapping
	if err := tx.Select("global_mid", "source_mid").
		Where("source_id = ? AND global_mid IN ?", sourceID, mids).
		Find(&rows).Error; err != nil {
		return err
	}
	vodsByMid := make(map[int64][]int64, len(mids))
	for _, row := range rows {
		vodsByMid[row.GlobalMid] = append(vodsByMid[row.GlobalMid], row.SourceMid)
	}
	for i := range items {
		if items[i].mid <= 0 || items[i].film == nil {
			continue
		}
		if vodKeepsMappedFilm(items[i].detail.Id, vodsByMid[items[i].mid]) {
			continue
		}
		items[i].film = nil
		items[i].mid = 0
	}
	return nil
}

func markUnmappedCreates(items []peerPageItem) {
	for i := range items {
		if items[i].film != nil {
			continue
		}
		items[i].create = true
	}
}
