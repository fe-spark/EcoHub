package writer

import (
	"context"
	"database/sql"
	"log"
	"math"
	"sort"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/film/cache"
	"server/internal/repository/film/shared"
	"server/internal/repository/film/snapshot"
	"server/internal/repository/support"
	"server/internal/spider/scheduler"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func createNewFilmIndexTx(tx *gorm.DB, sourceID string, detail model.MovieDetail) (*model.FilmIndex, error) {
	categoryVersion := support.GetCategoryVersion()
	ruleVersion := support.GetRuleVersion()
	fi, err := ConvertFilmIndex(sourceID, detail, categoryVersion, ruleVersion)
	if err != nil {
		return nil, err
	}
	fi.Mid = 0 // 自增生成
	fi.FirstSourceId = sourceID

	if err := tx.Create(&fi).Error; err != nil {
		return nil, err
	}

	pid := fi.Pid
	if pid <= 0 {
		pid = detail.RawPid
	}
	allKeys := shared.BuildMovieMatchKeysWithCategory(detail.DbId, detail.Name, pid)
	sort.Strings(allKeys)
	if len(allKeys) > 0 {
		matchKeys := make([]model.MovieMatchKey, 0, len(allKeys))
		for _, k := range allKeys {
			matchKeys = append(matchKeys, model.MovieMatchKey{
				Mid:      fi.Mid,
				MatchKey: k,
			})
		}
		if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&matchKeys).Error; err != nil {
			return nil, err
		}
	}

	return &fi, nil
}

func backfillFilmMetadataTx(tx *gorm.DB, existing *model.FilmIndex, source *model.FilmSource, detail model.MovieDetail) (bool, error) {
	updates := make(map[string]any)
	if existing.Actor == "" && detail.Actor != "" {
		updates["actor"] = detail.Actor
		existing.Actor = detail.Actor
	}
	if existing.Director == "" && detail.Director != "" {
		updates["director"] = detail.Director
		existing.Director = detail.Director
	}
	if existing.Writer == "" && detail.Writer != "" {
		updates["writer"] = detail.Writer
		existing.Writer = detail.Writer
	}
	if existing.Blurb == "" && detail.Blurb != "" {
		updates["blurb"] = detail.Blurb
		existing.Blurb = detail.Blurb
	}
	if existing.Content == "" && detail.Content != "" {
		updates["content"] = detail.Content
		existing.Content = detail.Content
	}
	if existing.ReleaseDate == "" {
		if clipped := firstISOReleaseDate(detail.ReleaseDate); clipped != "" {
			updates["release_date"] = clipped
			existing.ReleaseDate = clipped
		}
	}
	if existing.Year == 0 {
		y := parseYear(detail.ReleaseDate)
		if y == 0 {
			y = parseYear(detail.Year)
		}
		if y > 0 {
			updates["year"] = y
			existing.Year = y
		}
	}
	if existing.DbId == 0 && detail.DbId > 0 {
		updates["db_id"] = detail.DbId
		existing.DbId = detail.DbId
	}
	if source != nil && source.IsPosterSource && !existing.IsCustomPicture {
		if detail.Picture != "" && detail.Picture != existing.Picture {
			updates["picture"] = detail.Picture
			existing.Picture = detail.Picture
		}
		if detail.PictureSlide != "" && detail.PictureSlide != existing.PictureSlide {
			updates["picture_slide"] = detail.PictureSlide
			existing.PictureSlide = detail.PictureSlide
		}
	}

	if len(updates) > 0 {
		updates["updated_at"] = time.Now()
		return true, tx.Model(&model.FilmIndex{}).Where("mid = ?", existing.Mid).Updates(updates).Error
	}
	return false, nil
}

func refreshRemarksAndPlaySummaryTx(tx *gorm.DB, mid int64) error {
	if mid <= 0 {
		return nil
	}
	return refreshRemarksAndPlaySummaryMidsTx(tx, []int64{mid}, support.GetCollectSourceList())
}

func pickPlaylistRemarks(playLines []model.FilmSourcePlaylist, sources []model.FilmSource) string {
	if len(playLines) == 0 {
		return ""
	}
	orderByID := make(map[string]int, len(sources))
	for idx, source := range sources {
		orderByID[source.Id] = idx
	}
	bestLine := playLines[0]
	for i := 1; i < len(playLines); i++ {
		curr := playLines[i]
		if curr.EpisodeCount > bestLine.EpisodeCount {
			bestLine = curr
			continue
		}
		if curr.EpisodeCount != bestLine.EpisodeCount {
			continue
		}
		oCurr := math.MaxInt
		if idx, ok := orderByID[curr.SourceId]; ok {
			oCurr = idx
		}
		oBest := math.MaxInt
		if idx, ok := orderByID[bestLine.SourceId]; ok {
			oBest = idx
		}
		if oCurr < oBest || (oCurr == oBest && curr.GroupIndex < bestLine.GroupIndex) {
			bestLine = curr
		}
	}
	return bestLine.LastEpisode
}

func refreshRemarksAndPlaySummaryMidsTx(tx *gorm.DB, mids []int64, sources []model.FilmSource) error {
	ordered := uniqueMIDs(mids)
	if len(ordered) == 0 {
		return nil
	}
	sort.Slice(ordered, func(i, j int) bool { return ordered[i] < ordered[j] })

	var playLines []model.FilmSourcePlaylist
	if err := tx.Select("mid", "source_id", "line_kind", "group_index", "group_name", "episode_count", "last_episode").
		Where("mid IN ? AND line_kind = ?", ordered, "play").
		Find(&playLines).Error; err != nil {
		return err
	}
	byMid := make(map[int64][]model.FilmSourcePlaylist, len(ordered))
	for _, line := range playLines {
		byMid[line.Mid] = append(byMid[line.Mid], line)
	}
	now := time.Now()
	stamp := now.Unix()
	for _, mid := range ordered {
		if err := tx.Model(&model.FilmIndex{}).Where("mid = ?", mid).Updates(map[string]any{
			"remarks":           pickPlaylistRemarks(byMid[mid], sources),
			"play_from_summary": snapshot.BuildPlayFromSummaryFromPlaylists(byMid[mid], sources),
			"update_stamp":      stamp,
			"updated_at":        now,
		}).Error; err != nil {
			return err
		}
	}
	return nil
}

func runPeerCollectTx(fn func(tx *gorm.DB) error) error {
	if db.Mdb != nil && db.Mdb.Dialector != nil && db.Mdb.Dialector.Name() == "mysql" {
		return db.Mdb.Transaction(fn, &sql.TxOptions{Isolation: sql.LevelReadCommitted})
	}
	return db.Mdb.Transaction(fn)
}

func saveSinglePeerDetailTx(source *model.FilmSource, detail model.MovieDetail) (mid int64, isNew bool, playLinesChanged bool, err error) {
	err = runPeerCollectTx(func(tx *gorm.DB) error {
		var existingMapping model.MovieSourceMapping
		hasMapping := false
		if err := tx.Where("source_id = ? AND source_mid = ?", source.Id, detail.Id).First(&existingMapping).Error; err == nil && existingMapping.GlobalMid > 0 {
			hasMapping = true
			mid = existingMapping.GlobalMid
		}

		var filmIndex *model.FilmIndex
		if hasMapping {
			var fi model.FilmIndex
			if err := tx.Where("mid = ?", mid).First(&fi).Error; err == nil {
				filmIndex = &fi
			}
		}

		if filmIndex == nil {
			allKeys := peerMatchKeys(source, detail)
			if len(allKeys) > 0 {
				var matchKey model.MovieMatchKey
				if err := tx.Where("match_key IN ?", allKeys).Order("mid ASC").First(&matchKey).Error; err == nil && matchKey.Mid > 0 {
					var fi model.FilmIndex
					if err := tx.Where("mid = ?", matchKey.Mid).First(&fi).Error; err == nil && fi.Mid > 0 {
						mid = fi.Mid
						filmIndex = &fi
						if err := shared.SaveMovieSourceMappingTx(tx, source.Id, detail.Id, mid); err != nil {
							return err
						}
					}
				}
			}
		}

		if filmIndex == nil {
			fi, err := createNewFilmIndexTx(tx, source.Id, detail)
			if err != nil {
				return err
			}
			mid = fi.Mid
			filmIndex = fi
			isNew = true

			if err := shared.SaveMovieSourceMappingTx(tx, source.Id, detail.Id, mid); err != nil {
				return err
			}
		}

		newLines := buildPlaylistsFromDetail(mid, source.Id, detail)
		linesChanged, playChanged, err := saveStationPlaylistsTx(tx, mid, source.Id, newLines)
		if err != nil {
			return err
		}
		playLinesChanged = playChanged

		if linesChanged || isNew {
			if !isNew {
				if _, err := backfillFilmMetadataTx(tx, filmIndex, source, detail); err != nil {
					return err
				}
			}
			if err := refreshRemarksAndPlaySummaryTx(tx, mid); err != nil {
				return err
			}
		}

		return nil
	})
	return mid, isNew, playLinesChanged, err
}

// writePeerPage 和 savePeerFilm 默认走批量事务和单片事务。测试可替换它们。
var (
	writePeerPage = writePeerPageTx
	savePeerFilm  = saveSinglePeerDetailTx
)

func isDeterministicRowError(err error) bool {
	if err == nil {
		return false
	}
	msg := err.Error()
	return strings.Contains(msg, "Data too long") || strings.Contains(msg, "Error 1406")
}

// SaveCollectedPeerDetails 全站平权影片采集入库入口。
// 正常仍是一页一个事务。字段超长导致整页回滚时，改为逐片提交，只丢掉超长的那一部。
// 这类错误不返回给调用方，因此不会进入页级重试。连接失败等瞬时错误仍让整页重试。
func SaveCollectedPeerDetails(
	ctx context.Context,
	source *model.FilmSource,
	page int,
	list []model.MovieDetail,
) (scheduler.Mids, error) {
	_ = ctx
	details := filterPeerPageDetails(list)
	if len(details) == 0 {
		return scheduler.Mids{}, nil
	}
	if source == nil {
		return scheduler.Mids{}, errNilCollectSource
	}
	sources := support.GetCollectSourceList()
	unlock := lockPeerCollectPage(source, details)
	defer unlock()

	var affected []int64
	var notify []int64
	err := runPeerCollectTx(func(tx *gorm.DB) error {
		var writeErr error
		affected, notify, writeErr = writePeerPage(tx, source, details, sources)
		return writeErr
	})
	if err == nil {
		return scheduler.Mids{
			Affected: uniqueMIDs(affected),
			Notify:   uniqueMIDs(notify),
		}, nil
	}
	if isDeterministicRowError(err) {
		log.Printf("[Spider][PeerCollect] 整页因字段超长回滚，改为逐片入库 source=%s page=%d vods=%d err=%v",
			source.Id, page, len(details), err)
		return savePeerFilmsIsolated(source, page, details)
	}
	log.Printf("[Spider][PeerCollect] 整页入库失败 source=%s page=%d vods=%d err=%v",
		source.Id, page, len(details), err)
	return scheduler.Mids{}, err
}

func savePeerFilmsIsolated(source *model.FilmSource, page int, details []model.MovieDetail) (scheduler.Mids, error) {
	affected := make([]int64, 0, len(details))
	notify := make([]int64, 0, len(details))
	for _, detail := range details {
		mid, isNew, playChanged, err := savePeerFilm(source, detail)
		if err != nil {
			if isDeterministicRowError(err) {
				log.Printf("[Spider][PeerCollect] 单片入库跳过 source=%s page=%d vod=%d name=%s err=%v",
					source.Id, page, detail.Id, detail.Name, err)
				continue
			}
			log.Printf("[Spider][PeerCollect] 单片入库失败 source=%s page=%d vod=%d err=%v",
				source.Id, page, detail.Id, err)
			return scheduler.Mids{}, err
		}
		if mid <= 0 {
			continue
		}
		affected = append(affected, mid)
		if isNew || playChanged {
			notify = append(notify, mid)
		}
	}
	return scheduler.Mids{
		Affected: uniqueMIDs(affected),
		Notify:   uniqueMIDs(notify),
	}, nil
}

// SaveDetailsForCollect 兼容采集调度层调用。
func SaveDetailsForCollect(sourceID string, list []model.MovieDetail) (shared.CollectWriteResult, error) {
	source := repository.FindCollectSourceById(sourceID)
	if source == nil {
		source = &model.FilmSource{Id: sourceID, Name: sourceID, State: true}
	}
	mids, err := SaveCollectedPeerDetails(context.Background(), source, 1, list)
	if err != nil {
		return shared.CollectWriteResult{}, err
	}
	return shared.CollectWriteResult{
		AffectedMIDs: mids.Affected,
		NotifyMIDs:   mids.Notify,
	}, nil
}

type SaveDetailOptions struct {
	PublishSnapshot bool
}

// ClearFilmIndexCachesByPidSet 清理指定分类的搜索标签与列表缓存。
func ClearFilmIndexCachesByPidSet(pidSet map[int64]struct{}) {
	for pid := range pidSet {
		if pid <= 0 {
			continue
		}
		cache.ClearSearchTagsCache(pid)
	}
	cache.ClearProvideListCache()
}

func isFilmSearchTagFieldsChanged(oldInfo, newInfo model.FilmIndex) bool {
	return oldInfo.Pid != newInfo.Pid ||
		oldInfo.Cid != newInfo.Cid ||
		oldInfo.ClassTag != newInfo.ClassTag ||
		oldInfo.Area != newInfo.Area ||
		oldInfo.Language != newInfo.Language ||
		oldInfo.Year != newInfo.Year
}

// SaveDetail 后台单片手动保存/修改入口。
func SaveDetail(sourceID string, detail model.MovieDetail) error {
	_, err := SaveDetailWithOptions(sourceID, detail, SaveDetailOptions{PublishSnapshot: true})
	return err
}

// SaveDetailWithOptions 支持控制快照发布的单片保存入口（TMDB应用等场景）。
func SaveDetailWithOptions(sourceID string, detail model.MovieDetail, opts SaveDetailOptions) (int64, error) {
	source := repository.FindCollectSourceById(sourceID)
	if source == nil {
		source = &model.FilmSource{Id: sourceID, Name: sourceID, State: true}
	}

	identLock := getFilmIdentityLock(detail.Name)
	identLock.Lock()
	defer identLock.Unlock()

	sourceLock := getSourceWriteLock(source.Id)
	sourceLock.Lock()
	defer sourceLock.Unlock()

	var existingFilm model.FilmIndex
	isManualUpdate := false
	if detail.Id > 0 {
		if err := db.Mdb.Where("mid = ?", detail.Id).First(&existingFilm).Error; err == nil && existingFilm.Mid > 0 {
			isManualUpdate = true
		}
	}

	var mid int64
	if isManualUpdate {
		mid = existingFilm.Mid
		err := db.Mdb.Transaction(func(tx *gorm.DB) error {
			categoryVersion := support.GetCategoryVersion()
			ruleVersion := support.GetRuleVersion()
			fi, err := ConvertFilmIndex(sourceID, detail, categoryVersion, ruleVersion)
			if err != nil {
				return err
			}
			fi.Mid = mid
			fi.FirstSourceId = existingFilm.FirstSourceId
			if fi.FirstSourceId == "" {
				fi.FirstSourceId = sourceID
			}

			if detail.IsCustomPicture {
				if strings.TrimSpace(detail.CustomPicture) == "" && strings.TrimSpace(detail.Picture) != "" {
					detail.CustomPicture = strings.TrimSpace(detail.Picture)
				}
				fi.CustomPicture = detail.CustomPicture
				fi.CustomPictureSlide = detail.CustomPictureSlide
				fi.IsCustomPicture = true
				if strings.TrimSpace(fi.Picture) == "" {
					fi.Picture = existingFilm.Picture
				}
			} else {
				fi.CustomPicture = ""
				fi.CustomPictureSlide = ""
				fi.IsCustomPicture = false
			}

			updateCols := map[string]interface{}{
				"cid":                  fi.Cid,
				"pid":                  fi.Pid,
				"root_category_key":    fi.RootCategoryKey,
				"category_key":         fi.CategoryKey,
				"original_category":    fi.OriginalCategory,
				"name":                 fi.Name,
				"sub_title":            fi.SubTitle,
				"c_name":               fi.CName,
				"class_tag":            fi.ClassTag,
				"series_key":           fi.SeriesKey,
				"area":                 fi.Area,
				"language":             fi.Language,
				"year":                 fi.Year,
				"initial":              fi.Initial,
				"score":                fi.Score,
				"hits":                 fi.Hits,
				"state":                fi.State,
				"remarks":              fi.Remarks,
				"db_id":                fi.DbId,
				"picture":              fi.Picture,
				"picture_slide":        fi.PictureSlide,
				"custom_picture":       fi.CustomPicture,
				"custom_picture_slide": fi.CustomPictureSlide,
				"is_custom_picture":    fi.IsCustomPicture,
				"actor":                fi.Actor,
				"director":             fi.Director,
				"writer":               fi.Writer,
				"blurb":                fi.Blurb,
				"content":              fi.Content,
				"release_date":         firstISOReleaseDate(fi.ReleaseDate),
				"update_reason":        "后台修改",
				"update_stamp":         time.Now().Unix(),
			}
			if err := tx.Model(&model.FilmIndex{}).Where("mid = ?", mid).Updates(updateCols).Error; err != nil {
				return err
			}

			if len(detail.PlayList) > 0 || len(detail.DownloadList) > 0 {
				newLines := buildPlaylistsFromDetail(mid, source.Id, detail)
				if _, _, err := saveStationPlaylistsTx(tx, mid, source.Id, newLines); err != nil {
					return err
				}
				if err := refreshRemarksAndPlaySummaryTx(tx, mid); err != nil {
					return err
				}
			}

			pid := fi.Pid
			if pid <= 0 {
				pid = detail.RawPid
			}
			allKeys := shared.BuildMovieMatchKeysWithCategory(detail.DbId, detail.Name, pid)
			if len(allKeys) > 0 {
				_ = tx.Where("mid = ?", mid).Delete(&model.MovieMatchKey{}).Error
				mks := make([]model.MovieMatchKey, 0, len(allKeys))
				for _, k := range allKeys {
					mks = append(mks, model.MovieMatchKey{Mid: mid, MatchKey: k})
				}
				_ = tx.Create(&mks).Error
			}

			if isFilmSearchTagFieldsChanged(existingFilm, fi) {
				_ = UpsertDynamicSearchTags(fi)
			}
			return nil
		})
		if err != nil {
			return 0, err
		}
		ClearFilmIndexCachesByPidSet(map[int64]struct{}{existingFilm.Pid: {}})
	} else {
		var err error
		mid, _, _, err = saveSinglePeerDetailSynced(source, detail)
		if err != nil {
			return 0, err
		}
	}

	if opts.PublishSnapshot && mid > 0 {
		_, _, _ = snapshot.UpsertActiveSnapshotsByMids(mid)
	}

	return mid, nil
}
