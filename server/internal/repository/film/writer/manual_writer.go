package writer

import (
	"strings"
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
