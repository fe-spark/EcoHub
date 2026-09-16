package film

import (
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

const (
	upsertBatchSize = 200
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
	ClearSearchTagsCache(pid)
}

func clearFilmIndexCachesByPids(list []model.FilmIndex) {
	pidSet := make(map[int64]struct{})
	for _, item := range list {
		pidSet[item.Pid] = struct{}{}
	}
	clearFilmIndexCachesByPidSet(pidSet)
}

func clearFilmIndexCachesByPidSet(pidSet map[int64]struct{}) {
	for pid := range pidSet {
		if pid <= 0 {
			continue
		}
		ClearSearchTagsCache(pid)
	}
	ClearProvideListCache()
}

func BatchSaveOrUpdate(list []model.FilmIndex) map[string]int64 {
	list = filterValidFilmIndexes(list)
	if len(list) == 0 {
		return nil
	}

	keyToMid, err := saveFilmIndexesAndMappings(list)
	if err != nil {
		log.Printf("BatchSaveOrUpdate upsert 失败: %v\n", err)
		return nil
	}

	clearFilmIndexCachesByPids(list)
	BatchHandleSearchTag(list...)
	return keyToMid
}

func SaveFilmIndex(s model.FilmIndex) error {
	if _, err := saveFilmIndexesAndMappings([]model.FilmIndex{s}); err != nil {
		return err
	}
	clearFilmIndexCachesByPids([]model.FilmIndex{s})
	BatchHandleSearchTag(s)
	return nil
}

func SaveDetails(id string, list []model.MovieDetail) error {
	_, err := saveDetails(id, list, true)
	return err
}

// CollectWriteResult 采集写入结果。
// AffectedMIDs：有业务写入的 mid（缓存/快照收尾）；NotifyMIDs：应进更新列表的 mid（剧集结构变更或新片）。
type CollectWriteResult struct {
	AffectedMIDs []int64
	NotifyMIDs   []int64
}

// SaveDetailsForCollect 采集写主站详情。返回的 NotifyMIDs 仅含新片，或本源集数第一次超过全库最大集数。
// 片名标点/备注/封面/链接签名等噪声写库时不进入更新列表；其它源已有相同集数不重进列表。
func SaveDetailsForCollect(id string, list []model.MovieDetail) (CollectWriteResult, error) {
	return saveDetails(id, list, false)
}

func saveDetails(id string, list []model.MovieDetail, refreshSearchTags bool) (CollectWriteResult, error) {
	var out CollectWriteResult
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
		if err := saveMovieSourceMappingsTxE(tx, buildMovieSourceMappings(infoList, keyToMid)); err != nil {
			return err
		}

		if len(changedInfos) == 0 {
			return nil
		}

		if err := saveMovieDetailInfosTx(tx, buildMovieDetailInfos(id, changedDetails, infoByKey, keyToMid)); err != nil {
			return err
		}
		matchKeyMappings = buildMovieMatchKeyMappings(changedDetails, infoByKey, keyToMid)
		if err := saveMovieMatchKeysByMidTx(tx, matchKeyMappings); err != nil {
			return err
		}

		for i := range changedInfos {
			if mid, ok := keyToMid[changedInfos[i].ContentKey]; ok && mid > 0 {
				changedInfos[i].Mid = mid
			}
		}
		out.AffectedMIDs = collectFilmIndexMIDs(changedInfos)
		return RefreshPlayFromSummaryByIndexesTx(tx, changedInfos)
	}); err != nil {
		return CollectWriteResult{}, err
	}

	// 解耦主站写入长事务与附属表复活：在核心写事务提交后独立触发，结合其内部 Fast-path 探针，彻底规避跨表死锁
	if len(matchKeyMappings) > 0 {
		revivedKeys := make([]string, 0, len(matchKeyMappings)*4)
		for _, keys := range matchKeyMappings {
			revivedKeys = append(revivedKeys, keys...)
		}
		if len(revivedKeys) > 0 {
			if err := ReviveSlavePlaylistsTx(db.Mdb, revivedKeys); err != nil {
				log.Printf("[Collect] 批量复活附属站播放列表异常: %v", err)
			}
		}
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

	if detail.IsCustomPicture {
		// 管理员设置了自定义封面：存入 CustomPicture 独立字段，确保绝不破坏 Picture（源站/海报源原图）
		if strings.TrimSpace(detail.CustomPicture) == "" && strings.TrimSpace(detail.Picture) != "" {
			detail.CustomPicture = strings.TrimSpace(detail.Picture)
		}
		// 如果 Picture 为空或被误传为自定义图，保留并恢复已有库存中的原图；新片则以自定义图打底
		if strings.TrimSpace(detail.Picture) == "" || detail.Picture == detail.CustomPicture {
			if hasExisting && strings.TrimSpace(existing.Picture) != "" {
				detail.Picture = existing.Picture
				if strings.TrimSpace(existing.PictureSlide) != "" {
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
			keys := BuildPlaylistMovieKeys(detail)
			if posters, err := LoadPostersBySourceAndKeysTx(db.Mdb, ps.Id, keys); err == nil && len(posters) > 0 {
				if matched := pickBestMatchedPoster(detail, posters); matched != nil && strings.TrimSpace(matched.Picture) != "" {
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
			if strings.TrimSpace(existing.PictureSlide) != "" {
				detail.PictureSlide = existing.PictureSlide
			}
		}
	}

	snapshot, err := ConvertFilmIndex(id, detail, support.GetCategoryVersion(), support.GetRuleVersion())
	if err != nil {
		return err
	}
	if strings.TrimSpace(snapshot.Name) == "" {
		return nil
	}

	changed := false
	var savedMid int64
	var matchKeyMappings map[int64][]string
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		infoList := []model.FilmIndex{snapshot}
		unchangedKeys, _, _, err := applyMasterBusinessUpdateStampsTx(tx, infoList, detailMapByContentKey([]model.MovieDetail{detail}), true) //nolint:dogsled // 第 2/3 返回值此处不需要
		if err != nil {
			return err
		}
		writeInfos, infoByKey, writeDetails := filterChangedMasterWrites(infoList, []model.MovieDetail{detail}, unchangedKeys)
		if len(writeInfos) > 0 {
			if err := upsertFilmIndexesTx(tx, writeInfos); err != nil {
				return err
			}
			snapshot = writeInfos[0]
			changed = true
		}

		keyToMid := keyToMidFromIndexes(infoList)
		if len(keyToMid) == 0 {
			return fmt.Errorf("load film index mids failed")
		}
		if err := saveMovieSourceMappingsTxE(tx, buildMovieSourceMappings(infoList, keyToMid)); err != nil {
			return err
		}

		if !changed {
			return nil
		}

		if err := saveMovieDetailInfosTx(tx, buildMovieDetailInfos(id, writeDetails, infoByKey, keyToMid)); err != nil {
			return err
		}
		matchKeyMappings = buildMovieMatchKeyMappings(writeDetails, infoByKey, keyToMid)
		if err := saveMovieMatchKeysByMidTx(tx, matchKeyMappings); err != nil {
			return err
		}

		mid, ok := keyToMid[snapshot.ContentKey]
		if !ok || mid <= 0 {
			return nil
		}
		snapshot.Mid = mid
		savedMid = mid
		return RefreshPlayFromSummaryByIndexesTx(tx, []model.FilmIndex{snapshot})
	}); err != nil {
		return err
	}
	if err := repository.TouchCollectSourceStatsTx(db.Mdb, id, time.Now()); err != nil {
		log.Printf("TouchCollectSourceStats Error: %v", err)
	}

	// 解耦主站长事务与附属表复活：在核心写事务提交后独立触发，结合其内部 Fast-path 探针，彻底规避跨表死锁
	if changed && len(matchKeyMappings) > 0 {
		revivedKeys := make([]string, 0, len(matchKeyMappings)*4)
		for _, keys := range matchKeyMappings {
			revivedKeys = append(revivedKeys, keys...)
		}
		if len(revivedKeys) > 0 {
			if err := ReviveSlavePlaylistsTx(db.Mdb, revivedKeys); err != nil {
				log.Printf("[Collect] 复活附属站播放列表异常: %v", err)
			}
		}
	}
	if !changed {
		return nil
	}

	BatchHandleSearchTag(snapshot)
	clearDetailCaches(snapshot.Pid)
	ClearProvideListCache()
	if err := UpsertActiveSnapshotByMid(savedMid); err != nil {
		return err
	}
	if err := RefreshActiveReadModelArtifacts(); err != nil {
		return err
	}
	return nil
}
