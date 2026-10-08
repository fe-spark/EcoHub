package writer

import (
	"context"
	"crypto/md5"
	"encoding/hex"
	"encoding/json"
	"log"
	"math"
	"strconv"
	"strings"
	"sync"
	"time"

	"regexp"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/film/cache"
	"server/internal/repository/film/shared"
	"server/internal/repository/film/snapshot"
	"server/internal/repository/support"
	"server/internal/spider/scheduler"
	"server/internal/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var yearRegex = regexp.MustCompile(`[1-9][0-9]{3}`)

func parseYear(raw string) int64 {
	m := yearRegex.FindString(raw)
	if m == "" {
		return 0
	}
	y, _ := strconv.ParseInt(m, 10, 64)
	return y
}

func uniqueMIDs(mids []int64) []int64 {
	out := make([]int64, 0, len(mids))
	seen := make(map[int64]struct{}, len(mids))
	for _, m := range mids {
		if m <= 0 {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

var filmIdentityLocks [1024]sync.Mutex

func getFilmIdentityLock(name string) *sync.Mutex {
	clean := utils.NormalizeIdentityTitle(name)
	if clean == "" {
		clean = strings.TrimSpace(name)
	}
	hash := utils.GenerateHashKey(clean)
	var idx uint64
	if len(hash) >= 8 {
		idx, _ = strconv.ParseUint(hash[:8], 16, 32)
	}
	return &filmIdentityLocks[idx%1024]
}

var sourceWriteLocks sync.Map

func getSourceWriteLock(sourceID string) *sync.Mutex {
	if lock, ok := sourceWriteLocks.Load(sourceID); ok {
		return lock.(*sync.Mutex)
	}
	lock := &sync.Mutex{}
	actual, _ := sourceWriteLocks.LoadOrStore(sourceID, lock)
	return actual.(*sync.Mutex)
}

func computePlaylistContentHash(lineKind string, groupIndex int, groupName string, content string) string {
	raw := lineKind + "#" + strconv.Itoa(groupIndex) + "#" + groupName + "#" + content
	h := md5.Sum([]byte(raw))
	return hex.EncodeToString(h[:])
}

func playlistIdentity(line model.FilmSourcePlaylist) string {
	return line.LineKind + "#" + strconv.Itoa(line.GroupIndex)
}

func isPlaylistSignatureIdentical(oldLines, newLines []model.FilmSourcePlaylist) bool {
	if len(oldLines) != len(newLines) {
		return false
	}
	oldByKey := make(map[string]model.FilmSourcePlaylist, len(oldLines))
	for _, line := range oldLines {
		oldByKey[playlistIdentity(line)] = line
	}
	for _, line := range newLines {
		old, ok := oldByKey[playlistIdentity(line)]
		if !ok || old.ContentHash != line.ContentHash || old.GroupName != line.GroupName {
			return false
		}
	}
	return true
}

func saveStationPlaylistsTx(tx *gorm.DB, mid int64, sourceId string, newLines []model.FilmSourcePlaylist) (bool, bool, error) {
	var oldLines []model.FilmSourcePlaylist
	if err := tx.Where("mid = ? AND source_id = ?", mid, sourceId).Find(&oldLines).Error; err != nil {
		return false, false, err
	}
	if isPlaylistSignatureIdentical(oldLines, newLines) {
		return false, false, nil
	}

	// 检查是否有播放线路实质发生变更（非下载线路）
	playChanged := false
	oldPlayMap := make(map[int]string)
	for _, l := range oldLines {
		if l.LineKind == "play" {
			oldPlayMap[l.GroupIndex] = l.ContentHash
		}
	}
	newPlayMap := make(map[int]string)
	for _, l := range newLines {
		if l.LineKind == "play" {
			newPlayMap[l.GroupIndex] = l.ContentHash
		}
	}
	if len(oldPlayMap) != len(newPlayMap) {
		playChanged = true
	} else {
		for idx, h := range newPlayMap {
			if oldPlayMap[idx] != h {
				playChanged = true
				break
			}
		}
	}

	if err := tx.Where("mid = ? AND source_id = ?", mid, sourceId).Delete(&model.FilmSourcePlaylist{}).Error; err != nil {
		return false, false, err
	}
	if len(newLines) == 0 {
		return true, playChanged, nil
	}
	return true, playChanged, tx.Create(&newLines).Error
}

func buildPlaylistsFromDetail(mid int64, sourceID string, detail model.MovieDetail) []model.FilmSourcePlaylist {
	lines := make([]model.FilmSourcePlaylist, 0, len(detail.PlayList)+len(detail.DownloadList))

	for i, group := range detail.PlayList {
		if len(group) == 0 {
			continue
		}
		rawName := ""
		if i < len(detail.PlayFrom) {
			rawName = strings.TrimSpace(detail.PlayFrom[i])
		}
		contentBytes, _ := json.Marshal(group)
		contentStr := string(contentBytes)
		lastEp := ""
		if len(group) > 0 {
			lastEp = strings.TrimSpace(group[len(group)-1].Episode)
		}
		lines = append(lines, model.FilmSourcePlaylist{
			Mid:          mid,
			SourceId:     sourceID,
			LineKind:     "play",
			GroupIndex:   i,
			GroupName:    rawName,
			EpisodeCount: len(group),
			LastEpisode:  lastEp,
			Content:      contentStr,
			ContentHash:  computePlaylistContentHash("play", i, rawName, contentStr),
		})
	}

	for i, group := range detail.DownloadList {
		if len(group) == 0 {
			continue
		}
		rawName := strings.TrimSpace(detail.DownFrom)
		contentBytes, _ := json.Marshal(group)
		contentStr := string(contentBytes)
		lastEp := ""
		if len(group) > 0 {
			lastEp = strings.TrimSpace(group[len(group)-1].Episode)
		}
		lines = append(lines, model.FilmSourcePlaylist{
			Mid:          mid,
			SourceId:     sourceID,
			LineKind:     "download",
			GroupIndex:   i,
			GroupName:    rawName,
			EpisodeCount: len(group),
			LastEpisode:  lastEp,
			Content:      contentStr,
			ContentHash:  computePlaylistContentHash("download", i, rawName, contentStr),
		})
	}

	return lines
}

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
	if len(allKeys) > 0 {
		matchKeys := make([]model.MovieMatchKey, 0, len(allKeys))
		for _, k := range allKeys {
			matchKeys = append(matchKeys, model.MovieMatchKey{
				Mid:      fi.Mid,
				MatchKey: k,
			})
		}
		_ = tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&matchKeys).Error
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
	if existing.ReleaseDate == "" && detail.ReleaseDate != "" {
		updates["release_date"] = detail.ReleaseDate
		existing.ReleaseDate = detail.ReleaseDate
	}
	if existing.Year == 0 {
		y := parseYear(detail.Year)
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
	var playLines []model.FilmSourcePlaylist
	if err := tx.Where("mid = ? AND line_kind = ?", mid, "play").Find(&playLines).Error; err != nil {
		return err
	}

	sources := support.GetCollectSourceList()
	orderByID := make(map[string]int, len(sources))
	for idx, s := range sources {
		orderByID[s.Id] = idx
	}

	remarks := ""
	if len(playLines) > 0 {
		bestLine := playLines[0]
		for i := 1; i < len(playLines); i++ {
			curr := playLines[i]
			if curr.EpisodeCount > bestLine.EpisodeCount {
				bestLine = curr
			} else if curr.EpisodeCount == bestLine.EpisodeCount {
				oCurr := math.MaxInt
				if idx, ok := orderByID[curr.SourceId]; ok {
					oCurr = idx
				}
				oBest := math.MaxInt
				if idx, ok := orderByID[bestLine.SourceId]; ok {
					oBest = idx
				}
				if oCurr < oBest {
					bestLine = curr
				} else if oCurr == oBest && curr.GroupIndex < bestLine.GroupIndex {
					bestLine = curr
				}
			}
		}
		remarks = bestLine.LastEpisode
	}

	summary := snapshot.BuildPlayFromSummaryFromPlaylists(playLines, sources)

	updates := map[string]any{
		"remarks":           remarks,
		"play_from_summary": summary,
		"update_stamp":      time.Now().Unix(),
		"updated_at":        time.Now(),
	}
	return tx.Model(&model.FilmIndex{}).Where("mid = ?", mid).Updates(updates).Error
}

func saveSinglePeerDetailTx(source *model.FilmSource, detail model.MovieDetail) (mid int64, isNew bool, playLinesChanged bool, err error) {
	err = db.Mdb.Transaction(func(tx *gorm.DB) error {
		var existingMapping model.MovieSourceMapping
		hasMapping := false
		if err := tx.Where("source_id = ? AND source_mid = ?", source.Id, detail.Id).First(&existingMapping).Error; err == nil && existingMapping.GlobalMid > 0 {
			hasMapping = true
			mid = existingMapping.GlobalMid
		}

		lockNames := sqlFilmLockNames(source, detail, mid)
		if err := acquireSQLFilmLocks(tx, lockNames); err != nil {
			return err
		}
		defer releaseSQLFilmLocks(tx, lockNames)

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
				if err := tx.Where("match_key IN ?", allKeys).Order("id ASC").First(&matchKey).Error; err == nil && matchKey.Mid > 0 {
					var fi model.FilmIndex
					if err := tx.Where("mid = ?", matchKey.Mid).First(&fi).Error; err == nil && fi.Mid > 0 {
						mid = fi.Mid
						filmIndex = &fi
						_ = shared.SaveMovieSourceMappingTx(tx, source.Id, detail.Id, mid)
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

// SaveCollectedPeerDetails 全站平权影片采集入库入口。
// 每部片独立事务落库，自动识别或新建影片档案，增量挂接多源播放线路。
func SaveCollectedPeerDetails(
	ctx context.Context,
	source *model.FilmSource,
	page int,
	list []model.MovieDetail,
) (scheduler.Mids, error) {
	var affectedMIDs []int64
	var notifyMIDs []int64

	for _, detail := range list {
		if detail.Id <= 0 || strings.TrimSpace(detail.Name) == "" {
			continue
		}

		mid, isNew, playLinesChanged, err := saveSinglePeerDetailSynced(source, detail)

		if err != nil {
			log.Printf("[Spider][PeerCollect] 单片入库失败 source=%s page=%d vod_id=%d name=%s err=%v",
				source.Id, page, detail.Id, detail.Name, err)
			continue
		}

		if mid > 0 {
			affectedMIDs = append(affectedMIDs, mid)
			if isNew || playLinesChanged {
				notifyMIDs = append(notifyMIDs, mid)
			}
		}
	}

	return scheduler.Mids{
		Affected: uniqueMIDs(affectedMIDs),
		Notify:   uniqueMIDs(notifyMIDs),
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
				"release_date":         fi.ReleaseDate,
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
