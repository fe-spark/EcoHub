package writer

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/film/cache"
	"server/internal/repository/film/poster"
	"server/internal/repository/film/shared"
	"server/internal/repository/film/snapshot"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

func buildFilmIndexesFromDetails(sourceID string, details []model.MovieDetail) ([]model.FilmIndex, map[string]model.FilmIndex, error) {
	infoList := make([]model.FilmIndex, 0, len(details))
	infoByKey := make(map[string]model.FilmIndex, len(details))
	categoryVersion := support.GetCategoryVersion()
	ruleVersion := support.GetRuleVersion()
	for _, detail := range details {
		if strings.TrimSpace(detail.Name) == "" {
			continue
		}
		info, err := ConvertFilmIndex(sourceID, detail, categoryVersion, ruleVersion)
		if err != nil {
			return nil, nil, err
		}
		infoList = append(infoList, info)
		infoByKey[info.ContentKey] = info
	}
	return infoList, infoByKey, nil
}

func clearDetailCaches(pid int64) {
	cache.ClearSearchTagsCache(pid)
}

func clearFilmIndexCachesByPids(list []model.FilmIndex) {
	pidSet := make(map[int64]struct{})
	for _, item := range list {
		pidSet[item.Pid] = struct{}{}
	}
	ClearFilmIndexCachesByPidSet(pidSet)
}

func ClearFilmIndexCachesByPidSet(pidSet map[int64]struct{}) {
	for pid := range pidSet {
		if pid <= 0 {
			continue
		}
		cache.ClearSearchTagsCache(pid)
	}
	cache.ClearProvideListCache()
}

// SaveDetailsForCollect 采集写主站详情。返回的 NotifyMIDs 仅含新片，或本源集数第一次超过全库最大集数。
// 片名标点/备注/封面/链接签名等噪声写库时不进入更新列表；其它源已有相同集数不重进列表。
func SaveDetailsForCollect(id string, list []model.MovieDetail) (shared.CollectWriteResult, error) {
	return saveDetails(id, list, false)
}

func saveDetails(id string, list []model.MovieDetail, refreshSearchTags bool) (shared.CollectWriteResult, error) {
	var out shared.CollectWriteResult
	infoList, _, err := buildFilmIndexesFromDetails(id, list)
	if err != nil {
		return out, err
	}
	infoList = filterValidFilmIndexes(infoList)
	if len(infoList) == 0 {
		return out, nil
	}

	detailsByKey := detailMapByContentKey(list)
	var changedInfos []model.FilmIndex
	var infoByKey map[string]model.FilmIndex
	var changedDetails []model.MovieDetail
	var oldDetailsByMid map[int64]model.MovieDetail
	var preWriteCounts map[int64][]int
	var matchKeyMappings map[int64][]string
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		unchangedKeys, oldDetails, counts, err := applyMasterBusinessUpdateStampsTx(tx, infoList, detailsByKey, false)
		if err != nil {
			return err
		}
		oldDetailsByMid = oldDetails
		preWriteCounts = counts
		changedInfos, infoByKey, changedDetails = filterChangedMasterWrites(infoList, list, unchangedKeys)

		if len(changedInfos) > 0 {
			if err := upsertFilmIndexesTx(tx, changedInfos); err != nil {
				return err
			}
		}

		// 主站 mid=源站 id：直接由本批构造，避免未懒升行 content_key 仍为 name_* 时反查 miss。
		keyToMid := keyToMidFromIndexes(infoList)
		if len(keyToMid) == 0 {
			return fmt.Errorf("load film index mids failed")
		}
		if err := shared.SaveMovieSourceMappingsTxE(tx, shared.BuildMovieSourceMappings(infoList, keyToMid)); err != nil {
			return err
		}

		if len(changedInfos) == 0 {
			return nil
		}

		if err := saveMovieDetailInfosTx(tx, buildMovieDetailInfos(id, changedDetails, infoByKey, keyToMid)); err != nil {
			return err
		}
		matchKeyMappings = buildMovieMatchKeyMappings(changedDetails, infoByKey, keyToMid)
		if err := shared.SaveMovieMatchKeysByMidTx(tx, matchKeyMappings); err != nil {
			return err
		}

		for i := range changedInfos {
			if mid, ok := keyToMid[changedInfos[i].ContentKey]; ok && mid > 0 {
				changedInfos[i].Mid = mid
			}
		}
		out.AffectedMIDs = collectFilmIndexMIDs(changedInfos)
		return snapshot.RefreshPlayFromSummaryByIndexesTx(tx, changedInfos)
	}); err != nil {
		return shared.CollectWriteResult{}, err
	}


	if len(changedInfos) == 0 {
		return out, nil
	}

	// 更新列表：新片，或本源最大集数严格大于全库已有（含附属站）最大集数
	out.NotifyMIDs = filterPlayStructureNotifyMIDs(changedInfos, detailsByKey, oldDetailsByMid, preWriteCounts)

	// 仅在有实质变更时更新 last_collect_time / 失效缓存。
	if refreshSearchTags {
		if err := repository.TouchCollectSourceStatsTx(db.Mdb, id, time.Now()); err != nil {
			log.Printf("TouchCollectSourceStats Error: %v", err)
		}
		clearFilmIndexCachesByPids(changedInfos)
		BatchHandleSearchTag(changedInfos...)
	} else {
		repository.NoteCollectSourceStats(id)
		NoteCollectCacheInvalidationByIndexes(changedInfos)
	}
	return out, nil
}

func SaveDetail(id string, detail model.MovieDetail) error {
	var existing model.FilmIndex
	hasExisting := false
	if detail.Id > 0 && db.Mdb.Where("mid = ?", detail.Id).First(&existing).Error == nil {
		hasExisting = true
	}

	// 若更新既有影片且未指定播放资源，绝不覆盖/清空既有播放资源和线路
	if hasExisting && len(detail.PlayList) == 0 {
		var existingDetailRec model.MovieDetailInfo
		if db.Mdb.Where("mid = ?", detail.Id).First(&existingDetailRec).Error == nil && existingDetailRec.Content != "" {
			var oldDetail model.MovieDetail
			if json.Unmarshal([]byte(existingDetailRec.Content), &oldDetail) == nil {
				detail.PlayList = oldDetail.PlayList
				if len(detail.PlayFrom) == 0 {
					detail.PlayFrom = oldDetail.PlayFrom
				}
				if len(detail.DownloadList) == 0 {
					detail.DownloadList = oldDetail.DownloadList
				}
				if detail.DownFrom == "" {
					detail.DownFrom = oldDetail.DownFrom
				}
			}
		}
	}

	if detail.IsCustomPicture {
		// 管理员设置了自定义封面：存入 CustomPicture 独立字段，确保绝不破坏 Picture（源站/海报源原图）
		if strings.TrimSpace(detail.CustomPicture) == "" && strings.TrimSpace(detail.Picture) != "" {
			detail.CustomPicture = strings.TrimSpace(detail.Picture)
		}
		// 如果 Picture 为空或被误传为自定义图，保留并恢复已有库存中的原图；新片则以自定义图打底
		if strings.TrimSpace(detail.Picture) == "" || detail.Picture == detail.CustomPicture {
			if hasExisting && strings.TrimSpace(existing.Picture) != "" {
				detail.Picture = existing.Picture
				if strings.TrimSpace(detail.PictureSlide) == "" && strings.TrimSpace(existing.PictureSlide) != "" {
					detail.PictureSlide = existing.PictureSlide
				}
			} else if strings.TrimSpace(detail.Picture) == "" {
				detail.Picture = detail.CustomPicture
			}
		}
	} else {
		// 管理员选择跟随海报源：清空自定义海报，并自动同步当前启用的海报图源 (movie_poster)
		detail.CustomPicture = ""
		detail.CustomPictureSlide = ""

		matchedPoster := false
		ps := repository.GetPosterSource()
		if ps != nil && ps.State {
			keys := shared.BuildPlaylistMovieKeys(detail)
			if posters, err := poster.LoadPostersBySourceAndKeysTx(db.Mdb, ps.Id, keys); err == nil && len(posters) > 0 {
				if matched := poster.PickBestMatchedPoster(detail, posters); matched != nil && strings.TrimSpace(matched.Picture) != "" {
					detail.Picture = strings.TrimSpace(matched.Picture)
					if strings.TrimSpace(matched.PictureSlide) != "" {
						detail.PictureSlide = strings.TrimSpace(matched.PictureSlide)
					}
					matchedPoster = true
				}
			}
		}
		// 若外部海报源未命中且存在已有库存，强制恢复库存中的底层采集原图（避免前端传入的旧自定义图污染底层 Picture）
		if !matchedPoster && hasExisting && strings.TrimSpace(existing.Picture) != "" {
			detail.Picture = existing.Picture
			if strings.TrimSpace(detail.PictureSlide) == "" && strings.TrimSpace(existing.PictureSlide) != "" {
				detail.PictureSlide = existing.PictureSlide
			}
		}
	}

	filmIndex, err := ConvertFilmIndex(id, detail, support.GetCategoryVersion(), support.GetRuleVersion())
	if err != nil {
		return err
	}
	if strings.TrimSpace(filmIndex.Name) == "" {
		return nil
	}

	changed := false
	var savedMid int64
	var matchKeyMappings map[int64][]string
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		infoList := []model.FilmIndex{filmIndex}
		unchangedKeys, _, _, err := applyMasterBusinessUpdateStampsTx(tx, infoList, detailMapByContentKey([]model.MovieDetail{detail}), true) //nolint:dogsled // 第 2/3 返回值此处不需要
		if err != nil {
			return err
		}
		writeInfos, infoByKey, writeDetails := filterChangedMasterWrites(infoList, []model.MovieDetail{detail}, unchangedKeys)
		if len(writeInfos) > 0 {
			if err := upsertFilmIndexesTx(tx, writeInfos); err != nil {
				return err
			}
			filmIndex = writeInfos[0]
			changed = true
		}

		keyToMid := keyToMidFromIndexes(infoList)
		if len(keyToMid) == 0 {
			return fmt.Errorf("load film index mids failed")
		}
		if err := shared.SaveMovieSourceMappingsTxE(tx, shared.BuildMovieSourceMappings(infoList, keyToMid)); err != nil {
			return err
		}

		if !changed {
			return nil
		}

		if err := saveMovieDetailInfosTx(tx, buildMovieDetailInfos(id, writeDetails, infoByKey, keyToMid)); err != nil {
			return err
		}
		matchKeyMappings = buildMovieMatchKeyMappings(writeDetails, infoByKey, keyToMid)
		if err := shared.SaveMovieMatchKeysByMidTx(tx, matchKeyMappings); err != nil {
			return err
		}

		mid, ok := keyToMid[filmIndex.ContentKey]
		if !ok || mid <= 0 {
			return nil
		}
		filmIndex.Mid = mid
		savedMid = mid
		return snapshot.RefreshPlayFromSummaryByIndexesTx(tx, []model.FilmIndex{filmIndex})
	}); err != nil {
		return err
	}
	if err := repository.TouchCollectSourceStatsTx(db.Mdb, id, time.Now()); err != nil {
		log.Printf("TouchCollectSourceStats Error: %v", err)
	}

	if !changed {
		return nil
	}

	go BatchHandleSearchTag(filmIndex)
	clearDetailCaches(filmIndex.Pid)
	cache.ClearProvideListCache()
	if err := snapshot.UpsertActiveSnapshotByMid(savedMid); err != nil {
		return err
	}
	if err := snapshot.RefreshActiveReadModelArtifacts(); err != nil {
		return err
	}
	return nil
}
