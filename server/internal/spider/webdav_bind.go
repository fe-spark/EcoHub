package spider

import (
	"context"
	"errors"
	"fmt"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/utils"

	"gorm.io/gorm/clause"
)

// BindWebDAVItems 手动绑定/改绑指定的待处理 items 到指定 TMDB 条目并触发与主站匹配挂载
func BindWebDAVItems(req model.WebdavBindRequest) (*model.WebdavMediaGroup, []int64, error) {
	if db.Mdb == nil {
		return nil, nil, errors.New("数据库未连接")
	}
	if req.SourceId == "" || len(req.ItemIds) == 0 || req.TmdbId <= 0 {
		return nil, nil, errors.New("请求参数不完整")
	}

	var source model.FilmSource
	if err := db.Mdb.Where("id = ? AND source_type = ?", req.SourceId, model.SourceTypeWebdav).First(&source).Error; err != nil {
		return nil, nil, errors.New("WebDAV 资源站不存在")
	}

	cfg, err := source.GetWebdavConfig()
	if err != nil {
		return nil, nil, errors.New("资源站配置异常")
	}

	// 1. TMDB 取规范名
	tmdbClient, err := utils.NewTmdbClient(cfg.TmdbApiKey, cfg.TmdbBaseURL)
	if err != nil {
		return nil, nil, fmt.Errorf("初始化 TMDB 客户端失败: %w", err)
	}

	mediaDetail, err := tmdbClient.GetDetailByID(context.Background(), req.MediaType, req.TmdbId)
	if err != nil {
		return nil, nil, fmt.Errorf("获取 TMDB 详情失败: %w", err)
	}
	tmdbName := mediaDetail.Title
	tmdbYear := mediaDetail.Year
	genreIDs := mediaDetail.GenreIDs

	if tmdbName == "" {
		return nil, nil, errors.New("TMDB 影片名称为空")
	}

	// 2. 查询待绑定 items
	var items []model.WebdavScanItem
	if err := db.Mdb.Where("source_id = ? AND id IN ?", source.Id, req.ItemIds).Find(&items).Error; err != nil || len(items) == 0 {
		return nil, nil, errors.New("未找到待绑定的文件项")
	}

	// 保留扫描用 group_key，仅在空时按扫描算法补齐，避免下次增量扫描拆掉绑定
	for i := range items {
		if items[i].GroupKey == "" {
			items[i].GroupKey = computeScanGroupKey(req.MediaType, items[i].RelPath, items[i].Title, items[i].Season)
		}
	}

	// 3. 执行主站匹配
	actualBucket := "tv"
	if req.MediaType == "movie" {
		actualBucket = "movie"
	}
	if req.MediaType == "tv" {
		for _, gid := range genreIDs {
			if gid == 16 {
				actualBucket = "anime"
				break
			}
		}
	} else {
		for _, gid := range genreIDs {
			if gid == 99 {
				actualBucket = "doc"
				break
			}
		}
	}

	mappedPid := findRootCategoryPid(actualBucket)
	firstSeason := 1
	for _, it := range items {
		if it.Season > 0 {
			firstSeason = it.Season
			break
		}
	}

	masterFilm, matchStatus, matchErr := MatchMasterSite(tmdbName, req.MediaType == "tv", firstSeason, mappedPid)
	if masterFilm == nil || masterFilm.Mid <= 0 {
		// 0 mid: 状态置为 unmatched / category_mismatch，不写库
		lastErr := ""
		if matchErr != nil {
			lastErr = matchErr.Error()
		}
		for _, it := range items {
			db.Mdb.Model(&model.WebdavScanItem{}).Where("id = ?", it.ID).Updates(map[string]any{
				"tmdb_id":    req.TmdbId,
				"title":      tmdbName,
				"year":       tmdbYear,
				"group_key":  it.GroupKey,
				"status":     matchStatus,
				"last_error": lastErr,
			})
		}
		return nil, nil, fmt.Errorf("未能匹配到主站已有影片（%s），暂未挂载播放线路", matchStatus)
	}

	// 4. 命中主站：按扫描 group_key 写分组，不改写扫描指纹
	var mediaGroup model.WebdavMediaGroup
	seenKeys := make(map[string]struct{}, len(items))
	for _, it := range items {
		if _, ok := seenKeys[it.GroupKey]; ok {
			continue
		}
		seenKeys[it.GroupKey] = struct{}{}
		mediaGroup = model.WebdavMediaGroup{
			SourceId:  source.Id,
			GroupKey:  it.GroupKey,
			GlobalMid: masterFilm.Mid,
			TmdbId:    req.TmdbId,
			TmdbType:  req.MediaType,
			Title:     masterFilm.Name,
			Year:      tmdbYear,
		}
		db.Mdb.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_id"}, {Name: "group_key"}},
			UpdateAll: true,
		}).Create(&mediaGroup)
	}

	for _, it := range items {
		db.Mdb.Model(&model.WebdavScanItem{}).Where("id = ?", it.ID).Updates(map[string]any{
			"tmdb_id":    req.TmdbId,
			"title":      tmdbName,
			"year":       tmdbYear,
			"group_key":  it.GroupKey,
			"status":     "bound",
			"last_error": "",
		})
	}

	// 5. 重新生成并挂载播放列表
	affectedMids, _, err := SyncWebDAVPlaylists(source, cfg)
	if err != nil {
		return &mediaGroup, nil, fmt.Errorf("生成播放列表失败: %w", err)
	}

	return &mediaGroup, affectedMids, nil
}

// RescrapeWebDAV 对未匹配或指定的 items 执行重新刮削与匹配
func RescrapeWebDAV(req model.WebdavRescrapeRequest) (int, error) {
	if db.Mdb == nil {
		return 0, errors.New("数据库未连接")
	}
	source := repository.FindCollectSourceById(req.SourceId)
	if source == nil || source.SourceType != model.SourceTypeWebdav {
		return 0, errors.New("WebDAV 资源站不存在")
	}
	cfg, err := source.GetWebdavConfig()
	if err != nil {
		return 0, errors.New("资源站配置异常")
	}

	query := db.Mdb.Model(&model.WebdavScanItem{}).Where("source_id = ?", source.Id)
	if len(req.ItemIds) > 0 {
		query = query.Where("id IN ?", req.ItemIds)
	} else if req.ItemId > 0 {
		query = query.Where("id = ?", req.ItemId)
	} else if req.GroupKey != "" {
		query = query.Where("group_key = ?", req.GroupKey)
	} else {
		query = query.Where("status IN ('unmatched', 'category_mismatch', 'parse_failed', 'failed', 'missing')")
	}

	var items []model.WebdavScanItem
	if err := query.Find(&items).Error; err != nil || len(items) == 0 {
		return 0, errors.New("未找到可重新刮削的文件")
	}

	tmdbClient, err := utils.NewTmdbClient(cfg.TmdbApiKey, cfg.TmdbBaseURL)
	if err != nil {
		return 0, fmt.Errorf("初始化 TMDB 客户端失败: %w", err)
	}
	actualBucket := "tv"
	if cfg.MediaType == "movie" {
		actualBucket = "movie"
	}
	mappedPid := findRootCategoryPid(actualBucket)

	successCount := 0
	for _, it := range items {
		pRes, _ := utils.ParseMediaFilename(it.RelPath, cfg.MediaType)
		lookupTitle := ""
		var parsedYear int64
		season := it.Season
		if pRes != nil {
			lookupTitle = pRes.Title
			parsedYear = pRes.Year
			if pRes.Season > 0 {
				season = pRes.Season
			}
		}
		if lookupTitle == "" {
			lookupTitle = it.Title
		}
		if lookupTitle == "" {
			continue
		}
		if season <= 0 {
			season = 1
		}

		var tmdbID int64
		officialTitle := lookupTitle
		var officialYear int64 = parsedYear

		detail, err := tmdbClient.SearchAndGetDetail(context.Background(), cfg.MediaType, lookupTitle, parsedYear)
		if err == nil && detail != nil {
			tmdbID = detail.TmdbID
			if detail.Title != "" {
				officialTitle = detail.Title
			}
			if detail.Year > 0 {
				officialYear = detail.Year
			}
		}

		masterFilm, matchStatus, matchErr := MatchMasterSite(officialTitle, cfg.MediaType == "tv", season, mappedPid)
		if masterFilm != nil && masterFilm.Mid > 0 {
			groupKey := it.GroupKey
			if groupKey == "" {
				groupKey = computeScanGroupKey(cfg.MediaType, it.RelPath, officialTitle, season)
			}
			mediaGroup := model.WebdavMediaGroup{
				SourceId:  source.Id,
				GroupKey:  groupKey,
				GlobalMid: masterFilm.Mid,
				TmdbId:    tmdbID,
				TmdbType:  cfg.MediaType,
				Title:     masterFilm.Name,
				Year:      officialYear,
			}
			db.Mdb.Clauses(clause.OnConflict{
				Columns:   []clause.Column{{Name: "source_id"}, {Name: "group_key"}},
				UpdateAll: true,
			}).Create(&mediaGroup)

			db.Mdb.Model(&model.WebdavScanItem{}).Where("id = ?", it.ID).Updates(map[string]any{
				"tmdb_id":    tmdbID,
				"title":      officialTitle,
				"year":       officialYear,
				"group_key":  groupKey,
				"status":     "scraped",
				"last_error": "",
			})
			successCount++
		} else {
			lastErr := ""
			if matchErr != nil {
				lastErr = matchErr.Error()
			}
			db.Mdb.Model(&model.WebdavScanItem{}).Where("id = ?", it.ID).Updates(map[string]any{
				"tmdb_id":    tmdbID,
				"title":      officialTitle,
				"status":     matchStatus,
				"last_error": lastErr,
			})
		}
	}

	if successCount > 0 {
		_, _, _ = SyncWebDAVPlaylists(*source, cfg)
	}

	return successCount, nil
}
