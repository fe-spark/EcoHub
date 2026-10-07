package service

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"sort"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmshared "server/internal/repository/film/shared"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/utils"

	"golang.org/x/sync/singleflight"
)

var (
	filmDetailSfGroup  singleflight.Group
	relateMovieSfGroup singleflight.Group
)

func cloneMovieDetailVo(v model.MovieDetailVo) model.MovieDetailVo {
	cp := v
	if v.List != nil {
		cp.List = make([]model.PlayLinkVo, len(v.List))
		for i, source := range v.List {
			cp.List[i] = source
			if source.LinkList != nil {
				cp.List[i].LinkList = make([]model.MovieUrlInfo, len(source.LinkList))
				copy(cp.List[i].LinkList, source.LinkList)
			}
		}
	}
	if v.PlayFrom != nil {
		cp.PlayFrom = make([]string, len(v.PlayFrom))
		copy(cp.PlayFrom, v.PlayFrom)
	}
	if v.PlayList != nil {
		cp.PlayList = make([][]model.MovieUrlInfo, len(v.PlayList))
		for i, group := range v.PlayList {
			cp.PlayList[i] = make([]model.MovieUrlInfo, len(group))
			copy(cp.PlayList[i], group)
		}
	}
	if v.DownloadList != nil {
		cp.DownloadList = make([][]model.MovieUrlInfo, len(v.DownloadList))
		for i, group := range v.DownloadList {
			cp.DownloadList[i] = make([]model.MovieUrlInfo, len(group))
			copy(cp.DownloadList[i], group)
		}
	}
	return cp
}

// GetFilmDetail 影片详情信息页面处理（基础聚合版，按采集站权重排序）。
func (i *IndexService) GetFilmDetail(id int) (model.MovieDetailVo, error) {
	if id <= 0 {
		return model.MovieDetailVo{}, nil
	}

	cacheKey := fmt.Sprintf("%s:%d", config.FilmPlayInfoKey, id)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			if data == "{}" {
				return model.MovieDetailVo{}, nil
			}
			var cached model.MovieDetailVo
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached, nil
			}
		}
	}

	val, err, _ := filmDetailSfGroup.Do(cacheKey, func() (any, error) {
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				if data == "{}" {
					return model.MovieDetailVo{}, nil
				}
				var cached model.MovieDetailVo
				if json.Unmarshal([]byte(data), &cached) == nil {
					return cached, nil
				}
			}
		}

		playGen := filmsnapshot.PlayInfoGeneration()
		startedAt := time.Now()
		version := filmsnapshot.GetActiveReadModelVersion()
		snapshotStartedAt := time.Now()
		snapshot := filmsnapshot.GetSnapshotByMid(version, int64(id))
		logSlowIndexServiceStep("GetFilmDetail.snapshot", snapshotStartedAt, "id", id)
		if snapshot == nil {
			storeFilmPlayInfoCache(cacheKey, "{}", 60*time.Second, playGen)
			return model.MovieDetailVo{}, nil
		}
		detailStartedAt := time.Now()
		movieDetail, localUpdateTime := filmsnapshot.GetMovieDetailBySnapshot(*snapshot)
		logSlowIndexServiceStep("GetFilmDetail.detail", detailStartedAt, "id", id)
		if movieDetail == nil {
			filmsnapshot.DeleteActiveSnapshotsByMids(snapshot.Mid)
			storeFilmPlayInfoCache(cacheKey, "{}", 60*time.Second, playGen)
			return model.MovieDetailVo{}, nil
		}

		playList, downloadList := loadPlayAndDownloadSourcesByMid(int64(id))
		movieDetail.DownloadList = downloadList
		res := model.MovieDetailVo{
			MovieDetail:     *movieDetail,
			LocalUpdateTime: localUpdateTime,
			UpdateReason:    resolveUpdateReason(snapshot.UpdateReason, *snapshot, *movieDetail),
			List:            playList,
		}
		res.PlayList, res.PlayFrom = playGroupsFromPlayLinkVos(playList)

		logSlowIndexServiceStep("GetFilmDetail.total", startedAt, "id", id)

		if raw, err := json.Marshal(res); err == nil {
			jitter := time.Duration(rand.Intn(1800)) * time.Second
			storeFilmPlayInfoCache(cacheKey, string(raw), 12*time.Hour+jitter, playGen)
		}
		return res, nil
	})

	if err != nil || val == nil {
		return model.MovieDetailVo{}, err
	}
	res, ok := val.(model.MovieDetailVo)
	if !ok {
		return model.MovieDetailVo{}, nil
	}
	return cloneMovieDetailVo(res), nil
}

// GetFilmDetailWithPreferred 根据指定偏好站点组织播放线路与缺集降级。
func (i *IndexService) GetFilmDetailWithPreferred(id int, preferredSource string) (model.MovieDetailVo, error) {
	detail, err := i.GetFilmDetail(id)
	if err != nil {
		return model.MovieDetailVo{}, err
	}
	if preferredSource != "" && len(detail.List) > 0 {
		detail.List = OrganizePlaySources(detail.List, preferredSource)
		detail.PlayList, detail.PlayFrom = playGroupsFromPlayLinkVos(detail.List)
	}
	return detail, nil
}

// OrganizePlaySources 根据偏好站重排播放线路并做缺集降级补齐。
func OrganizePlaySources(playSources []model.PlayLinkVo, preferredSource string) []model.PlayLinkVo {
	preferredSource = strings.TrimSpace(preferredSource)
	if preferredSource == "" || len(playSources) == 0 {
		return playSources
	}

	var preferredLines []model.PlayLinkVo
	var otherLines []model.PlayLinkVo

	for _, item := range playSources {
		cp := item
		if cp.LinkList != nil {
			cp.LinkList = make([]model.MovieUrlInfo, len(item.LinkList))
			copy(cp.LinkList, item.LinkList)
		}
		if cp.SourceId == preferredSource {
			cp.IsPreferred = true
			preferredLines = append(preferredLines, cp)
		} else {
			cp.IsPreferred = false
			otherLines = append(otherLines, cp)
		}
	}

	if len(preferredLines) == 0 {
		return playSources
	}

	// 缺集降级补齐：若首选站第一条线路缺少某集，从其它站线路对应补齐
	if len(preferredLines) > 0 && len(otherLines) > 0 {
		existingEps := make(map[string]struct{})
		for _, ep := range preferredLines[0].LinkList {
			existingEps[strings.TrimSpace(ep.Episode)] = struct{}{}
		}

		for _, other := range otherLines {
			for _, ep := range other.LinkList {
				epName := strings.TrimSpace(ep.Episode)
				if epName == "" {
					continue
				}
				if _, ok := existingEps[epName]; !ok {
					existingEps[epName] = struct{}{}
					fallbackEp := model.MovieUrlInfo{
						Episode:    ep.Episode,
						Link:       ep.Link,
						IsFallback: true,
						SourceId:   other.SourceId,
						SourceName: other.Name,
					}
					preferredLines[0].LinkList = append(preferredLines[0].LinkList, fallbackEp)
				}
			}
		}
	}

	result := make([]model.PlayLinkVo, 0, len(preferredLines)+len(otherLines))
	result = append(result, preferredLines...)
	result = append(result, otherLines...)
	return result
}

func playGroupsFromPlayLinkVos(sources []model.PlayLinkVo) ([][]model.MovieUrlInfo, []string) {
	groups := make([][]model.MovieUrlInfo, 0, len(sources))
	names := make([]string, 0, len(sources))
	for _, s := range sources {
		groups = append(groups, s.LinkList)
		names = append(names, s.Name)
	}
	return groups, names
}

func loadPlayAndDownloadSourcesByMid(mid int64) ([]model.PlayLinkVo, [][]model.MovieUrlInfo) {
	if mid <= 0 {
		return nil, nil
	}
	var rows []model.FilmSourcePlaylist
	if err := db.Mdb.Where("mid = ?", mid).Order("line_kind ASC, group_index ASC").Find(&rows).Error; err != nil || len(rows) == 0 {
		return nil, nil
	}

	sources := repository.GetEnabledCollectSourceList()
	sourcesByID := make(map[string]model.FilmSource, len(sources))
	for _, s := range sources {
		sourcesByID[s.Id] = s
	}

	sourcePlayGroupCounts := make(map[string]int)
	for _, r := range rows {
		if r.LineKind == "play" {
			sourcePlayGroupCounts[r.SourceId]++
		}
	}

	var playList []model.PlayLinkVo
	var downloadList [][]model.MovieUrlInfo

	for _, r := range rows {
		source, hasSource := sourcesByID[r.SourceId]
		var rules []utils.DomainReplaceRule
		if hasSource && source.DomainReplaceRules != "" {
			rules = utils.ParseDomainReplaceRules(source.DomainReplaceRules)
		}

		var links []model.MovieUrlInfo
		if err := json.Unmarshal([]byte(r.Content), &links); err != nil {
			continue
		}
		if len(rules) > 0 {
			links = rewriteURLGroup(links, rules)
		}

		if r.LineKind == "play" {
			siteName := source.Name
			if siteName == "" {
				siteName = r.SourceId
			}
			displayName := filmshared.BuildDisplaySourceName(siteName, r.GroupName, r.GroupIndex, sourcePlayGroupCounts[r.SourceId])
			groupID := fmt.Sprintf("%s#%d", r.SourceId, r.GroupIndex)
			playList = append(playList, model.PlayLinkVo{
				Id:       groupID,
				SourceId: r.SourceId,
				Name:     displayName,
				LinkList: links,
			})
		} else if r.LineKind == "download" {
			downloadList = append(downloadList, links)
		}
	}

	primarySource := repository.GetActiveCollectSource()
	primaryID := ""
	if primarySource != nil {
		primaryID = primarySource.Id
	}

	sort.SliceStable(playList, func(i, j int) bool {
		if primaryID != "" {
			if playList[i].SourceId == primaryID && playList[j].SourceId != primaryID {
				return true
			}
			if playList[j].SourceId == primaryID && playList[i].SourceId != primaryID {
				return false
			}
		}
		wI := sourcesByID[playList[i].SourceId].Weight
		wJ := sourcesByID[playList[j].SourceId].Weight
		if wI != wJ {
			return wI > wJ
		}
		return playList[i].Id < playList[j].Id
	})

	return playList, downloadList
}

// BatchGetPlayPlaylistsByMids 批量加载影片播放列表（TVBox 批量组装，无 N+1）。
func BatchGetPlayPlaylistsByMids(mids []int64) map[int64][]model.PlayLinkVo {
	result := make(map[int64][]model.PlayLinkVo, len(mids))
	if len(mids) == 0 {
		return result
	}

	var rows []model.FilmSourcePlaylist
	if err := db.Mdb.Where("mid IN ? AND line_kind = 'play'", mids).
		Order("mid ASC, group_index ASC").
		Find(&rows).Error; err != nil || len(rows) == 0 {
		return result
	}

	sources := repository.GetEnabledCollectSourceList()
	sourcesByID := make(map[string]model.FilmSource, len(sources))
	for _, s := range sources {
		sourcesByID[s.Id] = s
	}

	sourcePlayGroupCounts := make(map[string]int)
	for _, r := range rows {
		key := fmt.Sprintf("%d#%s", r.Mid, r.SourceId)
		sourcePlayGroupCounts[key]++
	}

	for _, r := range rows {
		source, hasSource := sourcesByID[r.SourceId]
		var rules []utils.DomainReplaceRule
		if hasSource && source.DomainReplaceRules != "" {
			rules = utils.ParseDomainReplaceRules(source.DomainReplaceRules)
		}

		var links []model.MovieUrlInfo
		if err := json.Unmarshal([]byte(r.Content), &links); err != nil {
			continue
		}
		if len(rules) > 0 {
			links = rewriteURLGroup(links, rules)
		}

		siteName := source.Name
		if siteName == "" {
			siteName = r.SourceId
		}
		key := fmt.Sprintf("%d#%s", r.Mid, r.SourceId)
		displayName := filmshared.BuildDisplaySourceName(siteName, r.GroupName, r.GroupIndex, sourcePlayGroupCounts[key])
		groupID := fmt.Sprintf("%s#%d", r.SourceId, r.GroupIndex)

		result[r.Mid] = append(result[r.Mid], model.PlayLinkVo{
			Id:       groupID,
			SourceId: r.SourceId,
			Name:     displayName,
			LinkList: links,
		})
	}

	primarySource := repository.GetActiveCollectSource()
	primaryID := ""
	if primarySource != nil {
		primaryID = primarySource.Id
	}

	for mid := range result {
		lines := result[mid]
		sort.SliceStable(lines, func(i, j int) bool {
			if primaryID != "" {
				if lines[i].SourceId == primaryID && lines[j].SourceId != primaryID {
					return true
				}
				if lines[j].SourceId == primaryID && lines[i].SourceId != primaryID {
					return false
				}
			}
			wI := sourcesByID[lines[i].SourceId].Weight
			wJ := sourcesByID[lines[j].SourceId].Weight
			if wI != wJ {
				return wI > wJ
			}
			return lines[i].Id < lines[j].Id
		})
		result[mid] = lines
	}

	return result
}

func storeFilmPlayInfoCache(cacheKey, payload string, ttl time.Duration, gen int64) {
	if db.Rdb == nil {
		return
	}
	if filmsnapshot.PlayInfoGeneration() != gen {
		return
	}
	_ = db.Rdb.Set(db.Cxt, cacheKey, payload, ttl).Err()
	if filmsnapshot.PlayInfoGeneration() != gen {
		_ = db.Rdb.Del(db.Cxt, cacheKey).Err()
	}
}

// GetFilmDetailOnly 读取影片详情主体，不聚合播放源。
func (i *IndexService) GetFilmDetailOnly(id int) (model.MovieDetail, error) {
	startedAt := time.Now()
	version := filmsnapshot.GetActiveReadModelVersion()
	snapshotStartedAt := time.Now()
	snapshot := filmsnapshot.GetSnapshotByMid(version, int64(id))
	logSlowIndexServiceStep("GetFilmDetailOnly.snapshot", snapshotStartedAt, "id", id)
	if snapshot == nil {
		return model.MovieDetail{}, nil
	}
	detailStartedAt := time.Now()
	movieDetail, _ := filmsnapshot.GetMovieDetailBySnapshot(*snapshot)
	logSlowIndexServiceStep("GetFilmDetailOnly.detail", detailStartedAt, "id", id)
	if movieDetail == nil {
		filmsnapshot.DeleteActiveSnapshotsByMids(snapshot.Mid)
		return model.MovieDetail{}, nil
	}
	logSlowIndexServiceStep("GetFilmDetailOnly.total", startedAt, "id", id)
	return *movieDetail, nil
}

// RelateMovie 根据当前影片快照匹配相关的影片
func (i *IndexService) RelateMovie(mid int64, page *dto.Page) []model.MovieBasicInfo {
	if mid <= 0 {
		return []model.MovieBasicInfo{}
	}
	startedAt := time.Now()
	page = normalizeIndexPage(page)
	version := filmsnapshot.GetActiveReadModelVersion()
	if version == "" {
		version = filmsnapshot.GetActiveSnapshotVersion()
	}
	if version == "" {
		return []model.MovieBasicInfo{}
	}

	cacheKey := fmt.Sprintf("%s:v%s:%d:p%d:s%d", config.FilmRelateVOCachePrefix, version, mid, page.Current, page.PageSize)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			if data == "[]" {
				return []model.MovieBasicInfo{}
			}
			var cached []model.MovieBasicInfo
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached
			}
		}
	}

	val, err, _ := relateMovieSfGroup.Do(cacheKey, func() (any, error) {
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				if data == "[]" {
					return []model.MovieBasicInfo{}, nil
				}
				var cached []model.MovieBasicInfo
				if json.Unmarshal([]byte(data), &cached) == nil {
					return cached, nil
				}
			}
		}

		snapshotStartedAt := time.Now()
		snapshot := filmsnapshot.GetSnapshotByMid(version, mid)
		logSlowIndexServiceStep("RelateMovie.snapshot", snapshotStartedAt, "id", mid)
		if snapshot == nil {
			if db.Rdb != nil {
				_ = db.Rdb.Set(db.Cxt, cacheKey, "[]", 60*time.Second).Err()
			}
			return []model.MovieBasicInfo{}, nil
		}
		if !filmsnapshot.HasMovieDetail(snapshot.Mid) {
			filmsnapshot.DeleteActiveSnapshotsByMids(snapshot.Mid)
			if db.Rdb != nil {
				_ = db.Rdb.Set(db.Cxt, cacheKey, "[]", 60*time.Second).Err()
			}
			return []model.MovieBasicInfo{}, nil
		}
		listStartedAt := time.Now()
		list := filmsnapshot.ListRelatedSnapshotsReadModel(version, *snapshot, page)
		logSlowIndexServiceStep("RelateMovie.list", listStartedAt, "id", mid)
		buildStartedAt := time.Now()
		result := filmshared.BuildMovieBasicInfosFromSnapshots(list...)
		logSlowIndexServiceStep("RelateMovie.build", buildStartedAt, "id", mid)
		logSlowIndexServiceStep("RelateMovie.total", startedAt, "id", mid)

		if result == nil {
			result = []model.MovieBasicInfo{}
		}

		if db.Rdb != nil {
			if len(result) == 0 {
				_ = db.Rdb.Set(db.Cxt, cacheKey, "[]", 60*time.Second).Err()
			} else {
				if raw, err := json.Marshal(result); err == nil {
					_ = db.Rdb.Set(db.Cxt, cacheKey, string(raw), time.Hour).Err()
				}
			}
		}
		return result, nil
	})

	if err != nil || val == nil {
		return []model.MovieBasicInfo{}
	}
	result, ok := val.([]model.MovieBasicInfo)
	if !ok {
		return []model.MovieBasicInfo{}
	}
	res := make([]model.MovieBasicInfo, len(result))
	copy(res, result)
	return res
}

func rewriteURLGroup(links []model.MovieUrlInfo, rules []utils.DomainReplaceRule) []model.MovieUrlInfo {
	if len(rules) == 0 || links == nil {
		return links
	}
	out := make([]model.MovieUrlInfo, len(links))
	for i, link := range links {
		out[i] = model.MovieUrlInfo{
			Episode:    link.Episode,
			Link:       utils.ApplyDomainReplaceRules(link.Link, rules),
			IsFallback: link.IsFallback,
			SourceId:   link.SourceId,
			SourceName: link.SourceName,
		}
	}
	return out
}

func resolveUpdateReason(reason string, snapshot model.FilmListSnapshot, detail model.MovieDetail) string {
	return strings.TrimSpace(reason)
}
