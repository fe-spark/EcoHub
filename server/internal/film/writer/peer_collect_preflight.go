package writer

import (
	"context"
	"sort"
	"strings"

	"server/internal/model"
	"server/internal/taxonomy"
	"server/internal/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func upsertSourceMappingsTx(tx *gorm.DB, sourceID string, items []peerPageItem, mapped map[int64]int64, withVodUnix bool, storedUnix map[int64]int64) error {
	mappings := make([]model.MovieSourceMapping, 0, len(items))
	for i := range items {
		if items[i].mid <= 0 {
			continue
		}
		if mapped != nil {
			if existing, ok := mapped[items[i].detail.Id]; ok && existing == items[i].mid {
				continue
			}
		}
		row := model.MovieSourceMapping{
			SourceId:  sourceID,
			SourceMid: items[i].detail.Id,
			GlobalMid: items[i].mid,
		}
		if withVodUnix {
			if unix, err := utils.ParseCollectUpdateTime(items[i].detail.UpdateTime); err == nil {
				row.SourceVodUnix = unix
				if storedUnix != nil && items[i].isNew {
					if _, exists := storedUnix[items[i].detail.Id]; !exists {
						storedUnix[items[i].detail.Id] = unix
					}
				}
			}
		}
		mappings = append(mappings, row)
	}
	if len(mappings) == 0 {
		return nil
	}
	sort.Slice(mappings, func(i, j int) bool { return mappings[i].SourceMid < mappings[j].SourceMid })
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_mid"}},
		DoUpdates: clause.AssignmentColumns([]string{"global_mid", "updated_at"}),
	}).Create(&mappings).Error
}

func loadPageMappingsTx(tx *gorm.DB, sourceID string, sourceMids []int64) (map[int64]int64, map[int64]int64, error) {
	out := make(map[int64]int64, len(sourceMids))
	vodUnix := make(map[int64]int64, len(sourceMids))
	var rows []model.MovieSourceMapping
	if err := tx.Select("source_mid", "global_mid", "source_vod_unix").
		Where("source_id = ? AND source_mid IN ?", sourceID, sourceMids).
		Find(&rows).Error; err != nil {
		return nil, nil, err
	}
	for _, row := range rows {
		if row.SourceMid <= 0 {
			continue
		}
		vodUnix[row.SourceMid] = row.SourceVodUnix
		if row.GlobalMid > 0 {
			out[row.SourceMid] = row.GlobalMid
		}
	}
	return out, vodUnix, nil
}

// preflightFilterUnchangedDetails 在写锁与写事务外先做无锁只读预检，提前过滤明显过期或完全未变化的条目。
// 若整页均未发生变化，直接返回空切片，彻底省去加锁、开事务及行锁开销。
func preflightFilterUnchangedDetails(
	ctx context.Context,
	conn *gorm.DB,
	source *model.FilmSource,
	details []model.MovieDetail,
) ([]model.MovieDetail, error) {
	if conn == nil || source == nil || len(details) == 0 {
		return details, nil
	}
	if ctx != nil {
		conn = conn.WithContext(ctx)
	}

	sourceMids := make([]int64, 0, len(details))
	seen := make(map[int64]struct{}, len(details))
	for _, d := range details {
		if d.Id <= 0 {
			continue
		}
		if _, ok := seen[d.Id]; ok {
			continue
		}
		seen[d.Id] = struct{}{}
		sourceMids = append(sourceMids, d.Id)
	}
	if len(sourceMids) == 0 {
		return details, nil
	}

	var mappings []model.MovieSourceMapping
	if err := conn.Select("source_mid", "global_mid", "source_vod_unix").
		Where("source_id = ? AND source_mid IN ?", source.Id, sourceMids).
		Find(&mappings).Error; err != nil {
		return details, nil
	}
	if len(mappings) == 0 {
		return details, nil
	}

	bySourceMid := make(map[int64]model.MovieSourceMapping, len(mappings))
	mids := make([]int64, 0, len(mappings))
	midSeen := make(map[int64]struct{}, len(mappings))
	for _, m := range mappings {
		bySourceMid[m.SourceMid] = m
		if m.GlobalMid > 0 {
			if _, ok := midSeen[m.GlobalMid]; !ok {
				midSeen[m.GlobalMid] = struct{}{}
				mids = append(mids, m.GlobalMid)
			}
		}
	}
	if len(mids) == 0 {
		return details, nil
	}

	var filmRows []model.FilmIndex
	if err := conn.Select(ownedFilmSyncSelect).Where("mid IN ?", mids).Find(&filmRows).Error; err != nil {
		return details, nil
	}
	filmsByMid := make(map[int64]*model.FilmIndex, len(filmRows))
	for i := range filmRows {
		filmsByMid[filmRows[i].Mid] = &filmRows[i]
	}

	var plRows []model.FilmSourcePlaylist
	if err := playlistMetaQuery(conn).
		Where("source_id = ? AND mid IN ?", source.Id, mids).
		Find(&plRows).Error; err != nil {
		return details, nil
	}
	plByMid := make(map[int64][]model.FilmSourcePlaylist, len(mids))
	for _, line := range plRows {
		plByMid[line.Mid] = append(plByMid[line.Mid], line)
	}

	// 同页内若出现同一 source_mid 多条记录，预检仅在所有命中均可跳过时跳过，避免误剪同页末条覆盖逻辑。
	countBySourceMid := make(map[int64]int, len(details))
	for _, d := range details {
		countBySourceMid[d.Id]++
	}

	catVer := taxonomy.GetCategoryVersion()
	ruleVer := taxonomy.GetRuleVersion()
	out := make([]model.MovieDetail, 0, len(details))
	for _, d := range details {
		if d.Id <= 0 || countBySourceMid[d.Id] > 1 {
			out = append(out, d)
			continue
		}
		m, ok := bySourceMid[d.Id]
		if !ok || m.GlobalMid <= 0 {
			out = append(out, d)
			continue
		}
		film := filmsByMid[m.GlobalMid]
		if film == nil || film.FirstSourceId != source.Id {
			// 外站归属映射需要在事务内拆分建新片，不跳过。
			out = append(out, d)
			continue
		}

		incoming, timeErr := utils.ParseCollectUpdateTime(d.UpdateTime)
		if timeErr == nil && incoming < m.SourceVodUnix {
			// 时间严格落后于库内已归档时间，必然被 claim 判定为 stale，可直接跳过。
			continue
		}
		if timeErr == nil && incoming > m.SourceVodUnix {
			// 时间更新，需要进写事务刷新 source_vod_unix 与可能变化的片单/档案。
			out = append(out, d)
			continue
		}
		if timeErr != nil && m.SourceVodUnix == 0 {
			out = append(out, d)
			continue
		}

		// 时间相等（或双方时间都缺失后仅存库有时间戳）：只有在档案与片单哈希均完全一致时才安全跳过。
		newLines := buildPlaylistsFromDetail(film.Mid, source.Id, d)
		if !isPlaylistSignatureIdentical(plByMid[film.Mid], newLines) {
			out = append(out, d)
			continue
		}
		nextDetail := d
		if nextDetail.Pid <= 0 && film.Pid > 0 {
			nextDetail.Pid = film.Pid
		}
		if nextDetail.Cid <= 0 && film.Cid > 0 {
			nextDetail.Cid = film.Cid
		}
		if strings.TrimSpace(nextDetail.CName) == "" && film.CName != "" {
			nextDetail.CName = film.CName
		}
		nextFilm, convErr := ConvertFilmIndex(source.Id, nextDetail, catVer, ruleVer)
		if convErr != nil {
			out = append(out, d)
			continue
		}
		if len(ownedFilmUpdates(film, &nextFilm)) > 0 {
			out = append(out, d)
			continue
		}
		// 全部完全一致，无需开事务或加锁。
	}
	return out, nil
}
