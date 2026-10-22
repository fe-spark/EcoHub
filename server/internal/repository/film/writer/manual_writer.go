package writer

import (
	"errors"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/film/cache"
	"server/internal/repository/film/shared"
	"server/internal/repository/film/snapshot"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

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

func lockManualUpdate(source *model.FilmSource, existingFilm model.FilmIndex, detail model.MovieDetail) func() {
	held := make([]*sync.Mutex, 0, 8)
	keys := make([]string, 0, 8)
	if source != nil {
		keys = append(keys, peerMatchKeys(source, detail)...)
	}
	oldKeys := shared.BuildMovieMatchKeysWithCategory(existingFilm.DbId, existingFilm.Name, existingFilm.Pid)
	keys = append(keys, oldKeys...)

	for _, idx := range matchKeyLockIndexes(keys) {
		mu := &filmMatchKeyLocks[idx]
		mu.Lock()
		held = append(held, mu)
	}
	if source != nil {
		mu := getSourceWriteLock(source.Id)
		mu.Lock()
		held = append(held, mu)
	}
	if existingFilm.Mid > 0 {
		mu := getFilmMidLock(existingFilm.Mid)
		mu.Lock()
		held = append(held, mu)
	}
	return func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i].Unlock()
		}
	}
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
		unlock := lockManualUpdate(source, existingFilm, detail)
		defer unlock()
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

			if strings.TrimSpace(fi.Blurb) == "" {
				fi.Blurb = strings.TrimSpace(fi.Content)
			}
			locks := existingFilm.LockedFields
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
				locks = model.MergeLocks(locks, model.LockPoster)
				if strings.TrimSpace(fi.CustomPictureSlide) != "" {
					locks = model.MergeLocks(locks, model.LockBackdrop)
				} else {
					locks = model.DropLocks(locks, model.LockBackdrop)
				}
			} else {
				fi.CustomPicture = ""
				fi.CustomPictureSlide = ""
				fi.IsCustomPicture = false
				if strings.TrimSpace(fi.Picture) == "" {
					fi.Picture = existingFilm.Picture
				}
				if strings.TrimSpace(fi.PictureSlide) == "" {
					fi.PictureSlide = existingFilm.PictureSlide
				}
				locks = model.DropLocks(locks, model.LockPoster, model.LockBackdrop)
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
				"locked_fields":        locks,
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

			// 播放资源保存：保存当前影片所属采集站（或指定站点）的播放线路
			ownerSourceID := strings.TrimSpace(existingFilm.FirstSourceId)
			if ownerSourceID == "" {
				ownerSourceID = strings.TrimSpace(sourceID)
			}
			if ownerSourceID == "" {
				ownerSourceID = "manual"
			}
			if len(detail.PlayList) > 0 {
				newLines := buildPlaylistsFromDetail(mid, ownerSourceID, detail)
				if _, _, err := saveStationPlaylistsTx(tx, mid, ownerSourceID, newLines); err != nil {
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
		if !shared.HasPlayableLinks(detail) {
			return 0, errors.New("影片必须包含至少一条有效播放链接")
		}
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
