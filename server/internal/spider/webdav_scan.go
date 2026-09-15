package spider

import (
	"context"
	"crypto/sha1"
	"errors"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	filmrepo "server/internal/repository/film"
	"server/internal/utils"

	"gorm.io/gorm/clause"
)

type parsedItemHolder struct {
	file     utils.WebdavFileInfo
	pathHash string
	fp       string
	parsed   *utils.ParsedMedia
	groupKey string
}

// RunWebDAVScan 执行 WebDAV 附属源扫描入库全流程：
// 1. PROPFIND Depth:1 BFS 列举视频文件；
// 2. 空库/异常保护（熔断）；
// 3. 文件名解析、季集合并分组与 TMDB 刮削；
// 4. 双轨匹配 MacCMS 主站并挂载播放列表 (wdv:// 协议)；
// 5. 增量指纹比对与失效文件下线；
// 6. 生成审计报告并返回受影响的全局 mid 列表。
func RunWebDAVScan(ctx context.Context, s *model.FilmSource) ([]int64, error) {
	if s == nil || s.SourceType != model.SourceTypeWebdav {
		return nil, errors.New("非 WebDAV 资源站")
	}

	cfg, err := s.GetWebdavConfig()
	if err != nil {
		return nil, fmt.Errorf("解析 WebDAV 配置失败: %w", err)
	}

	startedAt := time.Now()
	bucket := strings.ToLower(strings.TrimSpace(cfg.MediaType))
	if bucket != "tv" {
		bucket = "movie"
	}
	mappedPid := findRootCategoryPid(bucket)

	updateCollectProgress(s.Id, func(p *model.CollectProgress) {
		p.Kind = "webdav"
		p.Phase = "listing"
		p.Status = progressStatusRunning
		p.Current = 0
		p.Total = 0
		p.Success = 0
		p.Failed = 0
		p.Error = ""
	})
	reportID := persistScanReport(0, s.Id, startedAt, time.Time{}, "running", 0, 0, 0, 0, 0, 0, 0, 0, false, "")

	foundSoFar := 0
	files, truncated, err := utils.ListWebDAVFilesProgress(ctx, cfg.ServerURL, cfg.RootPath, cfg.Username, cfg.Password, cfg.MinFileBytes, func(n int) {
		foundSoFar = n
		updateCollectProgress(s.Id, func(p *model.CollectProgress) {
			p.Phase = "listing"
			p.Found = n
		})
		if n == 1 || n%25 == 0 {
			persistScanReport(reportID, s.Id, startedAt, time.Time{}, "running", n, 0, 0, 0, 0, 0, 0, 0, false, "")
		}
	})
	if err != nil {
		updateCollectProgress(s.Id, func(p *model.CollectProgress) {
			p.Status = progressStatusFailed
			p.Error = err.Error()
			p.Found = foundSoFar
		})
		persistScanReport(reportID, s.Id, startedAt, time.Now(), "failed", foundSoFar, 0, 0, 0, 0, 0, 0, 0, false, err.Error())
		return nil, err
	}

	// 空库 / 存储卷卸载熔断保护：原有记录 > 0 但列举为 0 时阻断下线
	var existingItemCount int64
	db.Mdb.Model(&model.WebdavScanItem{}).Where("source_id = ?", s.Id).Count(&existingItemCount)
	if len(files) == 0 && existingItemCount > 0 {
		errMsg := "存储卷未挂载或目录为空，已熔断保护现有播放列表"
		log.Printf("[WebDAV Scan] 站点 %s (%s): %s\n", s.Name, s.Id, errMsg)
		updateCollectProgress(s.Id, func(p *model.CollectProgress) {
			p.Status = progressStatusFailed
			p.Error = errMsg
		})
		persistScanReport(reportID, s.Id, startedAt, time.Now(), "storage_unmounted", 0, 0, 0, 0, 0, 0, 0, 0, false, errMsg)
		return nil, errors.New(errMsg)
	}

	updateCollectProgress(s.Id, func(p *model.CollectProgress) {
		p.Phase = "parsing"
		p.Total = len(files)
		p.Found = len(files)
	})
	persistScanReport(reportID, s.Id, startedAt, time.Time{}, "running", len(files), 0, 0, 0, 0, 0, 0, 0, truncated, "")

	// 加载库内已有 item，用于增量指纹比对
	var existingItems []model.WebdavScanItem
	db.Mdb.Where("source_id = ?", s.Id).Find(&existingItems)
	existingMap := make(map[string]model.WebdavScanItem, len(existingItems))
	for _, it := range existingItems {
		existingMap[it.PathHash] = it
	}

	var parsedCount, failedCount, skippedCount, tmdbHitCount, unmatchedCount, savedCount int
	itemsByGroup := make(map[string][]parsedItemHolder)

	for _, file := range files {
		pathHash := fmt.Sprintf("%x", sha1.Sum([]byte(file.RelPath)))
		fp := fmt.Sprintf("%x", sha1.Sum([]byte(fmt.Sprintf("%d|%s|%s", file.Size, file.ModTime.Format(time.RFC3339), file.RelPath))))

		parsed, parseErr := utils.ParseMediaFilename(file.RelPath, cfg.MediaType)
		if parseErr != nil {
			failedCount++
			item := model.WebdavScanItem{
				SourceId:     s.Id,
				PathHash:     pathHash,
				RelPath:      file.RelPath,
				Size:         file.Size,
				LastModified: file.ModTime.Format(time.RFC3339),
				Fingerprint:  fp,
				Status:       "parse_failed",
				LastError:    parseErr.Error(),
			}
			db.Mdb.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "source_id"}, {Name: "path_hash"}},
				UpdateAll: true,
			}).Create(&item)
			continue
		}

		parsedCount++
		groupKey := computeScanGroupKey(cfg.MediaType, file.RelPath, parsed.Title, parsed.Season)

		itemsByGroup[groupKey] = append(itemsByGroup[groupKey], parsedItemHolder{
			file:     file,
			pathHash: pathHash,
			fp:       fp,
			parsed:   parsed,
			groupKey: groupKey,
		})
	}

	updateCollectProgress(s.Id, func(p *model.CollectProgress) {
		p.Phase = "matching"
		p.Parsed = parsedCount
	})

	// 初始化 TMDB 客户端
	tmdbKey := strings.TrimSpace(cfg.TmdbApiKey)
	if tmdbKey == "" {
		tmdbKey = strings.TrimSpace(os.Getenv("TMDB_API_KEY"))
	}
	var tmdbClient *utils.TmdbClient
	if tmdbKey != "" {
		if c, err := utils.NewTmdbClient(tmdbKey, cfg.TmdbBaseURL); err == nil {
			tmdbClient = c
		}
	}

	// 依次处理各个分组的 TMDB 刮削与主站匹配
	for groupKey, holders := range itemsByGroup {
		rep := holders[0]

		// 手动绑定过的文件按 path_hash 跳过，不依赖扫描重算的 group_key
		allBoundUnchanged := true
		for _, h := range holders {
			old, ok := existingMap[h.pathHash]
			if !ok || old.Fingerprint != h.fp || old.Status != "bound" {
				allBoundUnchanged = false
				break
			}
		}
		if allBoundUnchanged {
			skippedCount += len(holders)
			continue
		}

		// 增量检查：指纹未变且已刮削/跳过，且扫描 group 仍指向主站
		allUnchanged := true
		for _, h := range holders {
			if old, ok := existingMap[h.pathHash]; !ok || old.Fingerprint != h.fp || (old.Status != "scraped" && old.Status != "skipped") {
				allUnchanged = false
				break
			}
		}

		var existingGroup model.WebdavMediaGroup
		groupFound := db.Mdb.Where("source_id = ? AND group_key = ?", s.Id, groupKey).First(&existingGroup).Error == nil
		if allUnchanged && groupFound && existingGroup.GlobalMid > 0 {
			skippedCount += len(holders)
			continue
		}

		lookupName := rep.parsed.Title
		var tmdbID int64
		var genreIDs []int64
		if tmdbClient != nil {
			var res *utils.TmdbMediaDetail
			if rep.parsed.TmdbID > 0 {
				res, _ = tmdbClient.GetDetailByID(ctx, cfg.MediaType, rep.parsed.TmdbID)
			}
			if res == nil {
				res, _ = tmdbClient.SearchAndGetDetail(ctx, cfg.MediaType, rep.parsed.Title, rep.parsed.Year)
			}
			if res != nil {
				tmdbHitCount++
				if res.Title != "" {
					lookupName = res.Title
				}
				tmdbID = res.TmdbID
				genreIDs = res.GenreIDs
			}
		}

		// 分类桶映射细化
		actualBucket := bucket
		if cfg.MediaType == "tv" {
			for _, gid := range genreIDs {
				if gid == 16 {
					actualBucket = "anime"
					break
				}
			}
		} else if cfg.MediaType == "movie" {
			for _, gid := range genreIDs {
				if gid == 99 {
					actualBucket = "doc"
					break
				}
			}
		}
		groupMappedPid := findRootCategoryPid(actualBucket)
		if groupMappedPid <= 0 {
			groupMappedPid = mappedPid
		}

		// 主站条目匹配
		masterFilm, matchStatus, matchErr := MatchMasterSite(lookupName, cfg.MediaType == "tv", rep.parsed.Season, groupMappedPid)
		if masterFilm != nil && masterFilm.Mid > 0 {
			mediaGroup := model.WebdavMediaGroup{
				SourceId:  s.Id,
				GroupKey:  groupKey,
				GlobalMid: masterFilm.Mid,
				TmdbId:    tmdbID,
				TmdbType:  cfg.MediaType,
				Title:     masterFilm.Name,
				Year:      rep.parsed.Year,
			}
			db.Mdb.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "source_id"}, {Name: "group_key"}},
				UpdateAll: true,
			}).Create(&mediaGroup)

			for _, h := range holders {
				item := model.WebdavScanItem{
					SourceId:     s.Id,
					PathHash:     h.pathHash,
					RelPath:      h.file.RelPath,
					Size:         h.file.Size,
					LastModified: h.file.ModTime.Format(time.RFC3339),
					Fingerprint:  h.fp,
					GroupKey:     groupKey,
					Title:        lookupName,
					Year:         h.parsed.Year,
					Season:       h.parsed.Season,
					Episode:      h.parsed.Episode,
					TmdbId:       tmdbID,
					Status:       "scraped",
					Hint:         h.parsed.Hint,
					LastError:    "",
				}
				db.Mdb.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "source_id"}, {Name: "path_hash"}},
					UpdateAll: true,
				}).Create(&item)
			}
		} else {
			unmatchedCount += len(holders)
			lastErrStr := ""
			if matchErr != nil {
				lastErrStr = matchErr.Error()
			}
			for _, h := range holders {
				item := model.WebdavScanItem{
					SourceId:     s.Id,
					PathHash:     h.pathHash,
					RelPath:      h.file.RelPath,
					Size:         h.file.Size,
					LastModified: h.file.ModTime.Format(time.RFC3339),
					Fingerprint:  h.fp,
					GroupKey:     groupKey,
					Title:        lookupName,
					Year:         h.parsed.Year,
					Season:       h.parsed.Season,
					Episode:      h.parsed.Episode,
					TmdbId:       tmdbID,
					Status:       matchStatus,
					Hint:         h.parsed.Hint,
					LastError:    lastErrStr,
				}
				db.Mdb.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "source_id"}, {Name: "path_hash"}},
					UpdateAll: true,
				}).Create(&item)
			}
		}
	}

	var affectedMids []int64

	// 处理失效文件下线 (Old \ Seen)
	seenPathHashes := make(map[string]struct{}, len(files))
	for _, f := range files {
		h := fmt.Sprintf("%x", sha1.Sum([]byte(f.RelPath)))
		seenPathHashes[h] = struct{}{}
	}

	deletedCount := 0
	if truncated {
		log.Printf("[WebDAV Scan] 站点 %s (%s): 列举达到上限已截断，跳过失效下线\n", s.Name, s.Id)
	} else {
		for _, old := range existingItems {
			if _, seen := seenPathHashes[old.PathHash]; !seen && old.Status != "missing" {
				db.Mdb.Model(&model.WebdavScanItem{}).Where("id = ?", old.ID).Update("status", "missing")
				deletedCount++

				// 检查该 group 是否还有剩余活跃文件
				var remainingCount int64
				db.Mdb.Model(&model.WebdavScanItem{}).
					Where("source_id = ? AND group_key = ? AND status != 'missing'", s.Id, old.GroupKey).
					Count(&remainingCount)
				if remainingCount == 0 {
					var group model.WebdavMediaGroup
					if err := db.Mdb.Where("source_id = ? AND group_key = ?", s.Id, old.GroupKey).First(&group).Error; err == nil {
						if group.GlobalMid > 0 {
							var master model.FilmIndex
							if err := db.Mdb.Where("mid = ?", group.GlobalMid).First(&master).Error; err == nil {
								primaryKey := filmrepo.BuildPlaylistPrimaryMovieKey(model.MovieDetail{
									Id:   master.Mid,
									Name: master.Name,
									Pid:  master.Pid,
									Cid:  master.Cid,
									MovieDescriptor: model.MovieDescriptor{
										DbId:  master.DbId,
										CName: master.CName,
										Year:  strconv.FormatInt(master.Year, 10),
									},
								})
								if primaryKey != "" {
									db.Mdb.Unscoped().
										Where("source_id = ? AND movie_key = ?", s.Id, primaryKey).
										Delete(&model.SlaveMoviePlaylist{})
									affectedMids = append(affectedMids, group.GlobalMid)
								}
							}
						}
					}
				}
			}
		}
	}

	// 统一同步与挂载播放列表
	syncedMids, savedCount, syncErr := SyncWebDAVPlaylists(*s, cfg)
	if syncErr != nil {
		log.Printf("[WebDAV Scan] 站点 %s SyncWebDAVPlaylists 失败: %v\n", s.Name, syncErr)
	} else {
		affectedMids = append(affectedMids, syncedMids...)
	}

	persistScanReport(reportID, s.Id, startedAt, time.Now(), "done", len(files), parsedCount, tmdbHitCount, unmatchedCount, skippedCount, savedCount, deletedCount, failedCount, truncated, "")

	updateCollectProgress(s.Id, func(p *model.CollectProgress) {
		p.Phase = "done"
		p.Status = progressStatusDone
		p.Success = savedCount
		p.Failed = failedCount
		p.Found = len(files)
		p.Parsed = parsedCount
		p.TmdbHit = tmdbHitCount
		p.Unmatched = unmatchedCount
		p.Skipped = skippedCount
	})

	return uniqueInt64s(affectedMids), nil
}

// computeScanGroupKey 扫描侧分组键：剧集按规范化标题+季，电影按相对路径。
// 绑定/重刮不得改用 TMDB 名重算，否则增量扫描会把已绑定线路拆掉。
func computeScanGroupKey(mediaType, relPath, title string, season int) string {
	if mediaType == "tv" {
		if season <= 0 {
			season = 1
		}
		normTitle := strings.ToLower(strings.TrimSpace(title))
		return fmt.Sprintf("tv:%x", sha1.Sum([]byte(normTitle+"|"+strconv.Itoa(season))))
	}
	return fmt.Sprintf("movie:%x", sha1.Sum([]byte(relPath)))
}
