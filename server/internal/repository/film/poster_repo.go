package film

import (
	"encoding/json"
	"fmt"
	"sort"
	"strings"

	"server/internal/model"
	"server/internal/repository"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// SyncSlavePostersIfConfiguredTx 当附属站开启了 IsPosterSource 时：
// 1. 无论主站是否已入库，均将海报按 match_key 持久化落库 movie_poster 表（彻底解耦采集时序）；
// 2. 若主站已有对应匹配影片，同步增量覆盖主站 film_index 与详情 JSON。
func SyncSlavePostersIfConfiguredTx(tx *gorm.DB, sourceID string, details []model.MovieDetail, infos []model.FilmIndex) ([]int64, error) {
	if len(details) == 0 {
		return nil, nil
	}
	src := repository.FindCollectSourceById(sourceID)
	if src == nil || !src.IsPosterSource || !src.State {
		return nil, nil
	}

	// 1. 提取所有有效海报并写入 movie_poster 表（内存按 movie_key 去重）
	postersMap := make(map[string]model.MoviePoster, len(details)*2)
	for _, detail := range details {
		pic := strings.TrimSpace(detail.Picture)
		slide := strings.TrimSpace(detail.PictureSlide)
		if pic == "" {
			continue
		}
		keys := BuildPlaylistMovieKeys(detail)
		for _, key := range keys {
			if strings.TrimSpace(key) == "" {
				continue
			}
			postersMap[key] = model.MoviePoster{
				SourceId:     sourceID,
				MovieKey:     key,
				Picture:      pic,
				PictureSlide: slide,
			}
		}
	}

	if len(postersMap) > 0 {
		posters := make([]model.MoviePoster, 0, len(postersMap))
		for _, p := range postersMap {
			posters = append(posters, p)
		}
		if err := saveMoviePostersTx(tx, posters); err != nil {
			return nil, fmt.Errorf("save movie_poster failed: %w", err)
		}
	}

	if len(infos) == 0 {
		return nil, nil
	}

	// 2. 若主站已有匹配影片，同步更新主站库中的海报
	mids := make([]int64, 0, len(infos))
	for _, info := range infos {
		if info.Mid > 0 {
			mids = append(mids, info.Mid)
		}
	}
	if len(mids) == 0 {
		return nil, nil
	}

	globalMidByKey := make(map[string]int64, len(mids)*2)
	keysByMid := loadMovieMatchKeysByMidsTx(tx, mids)
	for mid, keys := range keysByMid {
		for _, key := range keys {
			if strings.TrimSpace(key) == "" {
				continue
			}
			globalMidByKey[key] = mid
		}
	}
	if len(globalMidByKey) == 0 {
		return nil, nil
	}

	midToDetail := make(map[int64]model.MovieDetail, len(details))
	for _, detail := range details {
		if strings.TrimSpace(detail.Picture) == "" {
			continue
		}
		globalMid, ok := resolveSlaveGlobalMid(detail, globalMidByKey)
		if !ok || globalMid <= 0 {
			continue
		}
		if _, seen := midToDetail[globalMid]; !seen {
			midToDetail[globalMid] = detail
		}
	}
	if len(midToDetail) == 0 {
		return nil, nil
	}

	indexByMid := make(map[int64]model.FilmIndex, len(infos))
	for _, idx := range infos {
		if idx.Mid > 0 {
			indexByMid[idx.Mid] = idx
		}
	}

	var toUpdate []pendingPosterUpdate
	var updatedMids []int64
	for mid, itemDetail := range midToDetail {
		curIndex, ok := indexByMid[mid]
		if !ok {
			continue
		}
		// 防冲刷保护：若该影片被人工手动修改/自定义锁定，坚决不覆盖
		if curIndex.IsCustomPicture {
			continue
		}
		pic := strings.TrimSpace(itemDetail.Picture)
		slide := strings.TrimSpace(itemDetail.PictureSlide)
		if stripURLQuery(curIndex.Picture) == stripURLQuery(pic) && (slide == "" || stripURLQuery(curIndex.PictureSlide) == stripURLQuery(slide)) {
			continue
		}
		toUpdate = append(toUpdate, pendingPosterUpdate{
			mid:          mid,
			picture:      pic,
			pictureSlide: slide,
		})
		updatedMids = append(updatedMids, mid)
	}

	if len(updatedMids) == 0 {
		return nil, nil
	}

	// 严格按 mid 升序排序，避免并发写入时出现 MySQL 行锁死锁
	sort.Slice(toUpdate, func(i, j int) bool {
		return toUpdate[i].mid < toUpdate[j].mid
	})
	sort.Slice(updatedMids, func(i, j int) bool {
		return updatedMids[i] < updatedMids[j]
	})

	err := tx.Transaction(func(dtx *gorm.DB) error {
		if err := batchUpdateFilmIndexPostersTx(dtx, toUpdate); err != nil {
			return fmt.Errorf("batch update film_index poster failed: %w", err)
		}

		var detailInfos []model.MovieDetailInfo
		if err := dtx.Where("mid IN ?", updatedMids).Order("mid ASC").Find(&detailInfos).Error; err != nil {
			return fmt.Errorf("find movie_detail_info failed: %w", err)
		}
		newContents := make(map[int64]string, len(detailInfos))
		for _, info := range detailInfos {
			if info.Content == "" {
				continue
			}
			itemDetail := midToDetail[info.Mid]
			pic := strings.TrimSpace(itemDetail.Picture)
			slide := strings.TrimSpace(itemDetail.PictureSlide)
			var md model.MovieDetail
			if err := json.Unmarshal([]byte(info.Content), &md); err != nil {
				continue
			}
			md.Picture = pic
			if slide != "" {
				md.PictureSlide = slide
			}
			newData, err := json.Marshal(md)
			if err != nil {
				continue
			}
			newContents[info.Mid] = string(newData)
		}
		if err := batchUpdateMovieDetailInfoContentsTx(dtx, newContents); err != nil {
			return fmt.Errorf("batch update movie_detail_info failed: %w", err)
		}
		return nil
	})
	if err != nil {
		return nil, err
	}

	return updatedMids, nil
}

type pendingPosterUpdate struct {
	mid          int64
	picture      string
	pictureSlide string
}

func batchUpdateFilmIndexPostersTx(tx *gorm.DB, updates []pendingPosterUpdate) error {
	if len(updates) == 0 {
		return nil
	}
	const batchChunkSize = 200
	for start := 0; start < len(updates); start += batchChunkSize {
		end := start + batchChunkSize
		if end > len(updates) {
			end = len(updates)
		}
		chunk := updates[start:end]
		mids := make([]int64, 0, len(chunk))
		picCase := "CASE mid"
		picArgs := make([]any, 0, len(chunk)*2)
		hasSlide := false
		slideCase := "CASE mid"
		slideArgs := make([]any, 0, len(chunk)*2)

		for _, item := range chunk {
			if item.mid <= 0 {
				continue
			}
			mids = append(mids, item.mid)
			picCase += " WHEN ? THEN ?"
			picArgs = append(picArgs, item.mid, item.picture)
			if item.pictureSlide != "" {
				hasSlide = true
				slideCase += " WHEN ? THEN ?"
				slideArgs = append(slideArgs, item.mid, item.pictureSlide)
			}
		}
		if len(mids) == 0 {
			continue
		}
		picCase += " ELSE picture END"
		updateMap := map[string]any{
			"picture": clause.Expr{SQL: picCase, Vars: picArgs},
		}
		if hasSlide {
			slideCase += " ELSE picture_slide END"
			updateMap["picture_slide"] = clause.Expr{SQL: slideCase, Vars: slideArgs}
		}
		if err := tx.Model(&model.FilmIndex{}).Where("mid IN ? AND is_custom_picture = ?", mids, false).Updates(updateMap).Error; err != nil {
			return err
		}
	}
	return nil
}

func batchUpdateMovieDetailInfoContentsTx(tx *gorm.DB, contents map[int64]string) error {
	if len(contents) == 0 {
		return nil
	}
	sortedMids := make([]int64, 0, len(contents))
	for mid := range contents {
		if mid > 0 {
			sortedMids = append(sortedMids, mid)
		}
	}
	sort.Slice(sortedMids, func(i, j int) bool {
		return sortedMids[i] < sortedMids[j]
	})

	const batchChunkSize = 200
	for start := 0; start < len(sortedMids); start += batchChunkSize {
		end := start + batchChunkSize
		if end > len(sortedMids) {
			end = len(sortedMids)
		}
		chunkMids := sortedMids[start:end]
		caseExpr := "CASE mid"
		args := make([]any, 0, len(chunkMids)*2)
		for _, mid := range chunkMids {
			caseExpr += " WHEN ? THEN ?"
			args = append(args, mid, contents[mid])
		}
		caseExpr += " ELSE content END"
		if err := tx.Model(&model.MovieDetailInfo{}).Where("mid IN ?", chunkMids).Update("content", clause.Expr{SQL: caseExpr, Vars: args}).Error; err != nil {
			return err
		}
	}
	return nil
}

// saveMoviePostersTx 批量保存海报源海报数据
func saveMoviePostersTx(tx *gorm.DB, list []model.MoviePoster) error {
	if len(list) == 0 {
		return nil
	}
	const batchChunkSize = 200
	for start := 0; start < len(list); start += batchChunkSize {
		end := start + batchChunkSize
		if end > len(list) {
			end = len(list)
		}
		chunk := list[start:end]
		if err := tx.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_id"}, {Name: "movie_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"picture", "picture_slide", "updated_at", "deleted_at"}),
		}).Create(&chunk).Error; err != nil {
			return err
		}
	}
	return nil
}

// LoadPostersBySourceAndKeysTx 根据源站 ID 和 movie_keys 批量加载海报
func LoadPostersBySourceAndKeysTx(tx *gorm.DB, sourceID string, movieKeys []string) (map[string]model.MoviePoster, error) {
	result := make(map[string]model.MoviePoster, len(movieKeys))
	if strings.TrimSpace(sourceID) == "" || len(movieKeys) == 0 {
		return result, nil
	}
	var rows []model.MoviePoster
	if err := tx.Select("movie_key", "picture", "picture_slide").
		Where("source_id = ? AND movie_key IN ?", sourceID, UniqueKeys(movieKeys)).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		result[row.MovieKey] = row
	}
	return result, nil
}

// ApplyExternalPosterSourceToMasterWritesTx 主站写入时应用海报三层优先级：
// 1. 若已有库存且被人工自定义锁定 (existing.IsCustomPicture == true)，保持原自定义海报，坚决不覆盖；
// 2. 若当前启用了有效海报源且 movie_poster 命中该片高清图，应用海报源的高清海报与幻灯图；
// 3. 否则保持主站自身采集到的封面海报。
func ApplyExternalPosterSourceToMasterWritesTx(
	tx *gorm.DB,
	masterSourceID string,
	infos []model.FilmIndex,
	detailsByKey map[string]model.MovieDetail,
	existingByMid map[int64]model.FilmIndex,
	isManual bool,
) error {
	// 1. 若非人工手动保存（即爬虫日常采集写入），执行人工自定义海报保护
	if !isManual {
		for index := range infos {
			if existing, ok := existingByMid[infos[index].Mid]; ok && existing.IsCustomPicture {
				infos[index].IsCustomPicture = true
				infos[index].CustomPicture = existing.CustomPicture
				infos[index].CustomPictureSlide = existing.CustomPictureSlide
				if newDetail, ok := detailsByKey[infos[index].ContentKey]; ok {
					newDetail.IsCustomPicture = true
					newDetail.CustomPicture = existing.CustomPicture
					newDetail.CustomPictureSlide = existing.CustomPictureSlide
					detailsByKey[infos[index].ContentKey] = newDetail
				}
			}
		}
	}

	ps := repository.GetPosterSource()
	if ps == nil || ps.Id == masterSourceID || !ps.State {
		return nil
	}

	// 收集本批详情的所有 match_keys（跳过已自定义锁定的项）
	allKeys := make([]string, 0, len(detailsByKey)*2)
	for _, detail := range detailsByKey {
		if detail.IsCustomPicture {
			continue
		}
		allKeys = append(allKeys, BuildPlaylistMovieKeys(detail)...)
	}
	allKeys = UniqueKeys(allKeys)

	postersByKey, err := LoadPostersBySourceAndKeysTx(tx, ps.Id, allKeys)
	if err != nil {
		return err
	}

	for index := range infos {
		if infos[index].IsCustomPicture {
			continue
		}
		contentKey := infos[index].ContentKey
		newDetail, ok := detailsByKey[contentKey]
		if !ok || newDetail.IsCustomPicture {
			continue
		}

		matchedPoster := pickBestMatchedPoster(newDetail, postersByKey)
		if matchedPoster != nil && strings.TrimSpace(matchedPoster.Picture) != "" {
			// 海报源已采集并命中匹配 -> 直接应用海报源的高清海报与幻灯图
			infos[index].Picture = strings.TrimSpace(matchedPoster.Picture)
			if strings.TrimSpace(matchedPoster.PictureSlide) != "" {
				infos[index].PictureSlide = strings.TrimSpace(matchedPoster.PictureSlide)
			}
			newDetail.Picture = infos[index].Picture
			if infos[index].PictureSlide != "" {
				newDetail.PictureSlide = infos[index].PictureSlide
			}
			detailsByKey[contentKey] = newDetail
		}
	}
	return nil
}

func pickBestMatchedPoster(detail model.MovieDetail, postersByKey map[string]model.MoviePoster) *model.MoviePoster {
	if len(postersByKey) == 0 {
		return nil
	}
	for _, key := range BuildPlaylistMovieKeys(detail) {
		if p, ok := postersByKey[key]; ok && strings.TrimSpace(p.Picture) != "" {
			poster := p
			return &poster
		}
	}
	return nil
}

// DeletePostersBySourceIdTx 删除指定采集站的海报数据
func DeletePostersBySourceIdTx(tx *gorm.DB, sourceID string) error {
	return tx.Where("source_id = ?", sourceID).Unscoped().Delete(&model.MoviePoster{}).Error
}

func hasExternalPosterSource(masterSourceID string) bool {
	ps := repository.GetPosterSource()
	return ps != nil && ps.Id != masterSourceID && ps.State
}
