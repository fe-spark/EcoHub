package service

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmplaylist "server/internal/repository/film/playlist"
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

// GetFilmDetail 影片详情信息页面处理
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
		res := model.MovieDetailVo{MovieDetail: *movieDetail, LocalUpdateTime: localUpdateTime}
		multipleStartedAt := time.Now()
		res.List = multipleSource(snapshot, movieDetail)
		logSlowIndexServiceStep("GetFilmDetail.multipleSource", multipleStartedAt, "id", id)
		logSlowIndexServiceStep("GetFilmDetail.total", startedAt, "id", id)

		if snapshot.SourceId != "" {
			if source := repository.FindCollectSourceById(snapshot.SourceId); source != nil && source.DomainReplaceRules != "" {
				if rules := utils.ParseDomainReplaceRules(source.DomainReplaceRules); len(rules) > 0 {
					res.PlayList = rewriteURLGroups(res.PlayList, rules)
					res.DownloadList = rewriteURLGroups(res.DownloadList, rules)
				}
			}
		}

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

// GetFilmDetailOnly 读取影片详情主体，不聚合附属站播放源。
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

func multipleSource(snapshot *model.FilmListSnapshot, detail *model.MovieDetail) []model.PlayLinkVo {
	startedAt := time.Now()
	primaryStartedAt := time.Now()
	playList := buildPrimaryPlaySources(snapshot, detail)
	logSlowIndexServiceStep("multipleSource.primary", primaryStartedAt, "id", snapshot.Mid)
	keysStartedAt := time.Now()
	names := filmshared.LoadMovieMatchKeysBySnapshot(snapshot, detail)
	logSlowIndexServiceStep("multipleSource.matchKeys", keysStartedAt, "id", snapshot.Mid)
	if len(names) == 0 {
		return playList
	}

	sourcesStartedAt := time.Now()
	slaveSources := repository.GetCollectSourceListByGrade(model.SlaveCollect)
	logSlowIndexServiceStep("multipleSource.sources", sourcesStartedAt, "id", snapshot.Mid)
	querySources := make([]model.FilmSource, 0, len(slaveSources))
	seenSourceIDs := make(map[string]struct{}, len(playList))
	for _, item := range playList {
		sourceID := strings.TrimSpace(item.SourceId)
		if sourceID == "" {
			sourceID = strings.TrimSpace(item.Id)
		}
		if sourceID == "" {
			continue
		}
		seenSourceIDs[sourceID] = struct{}{}
	}

	for _, source := range slaveSources {
		if !source.State {
			continue
		}
		if _, ok := seenSourceIDs[source.Id]; ok {
			continue
		}
		querySources = append(querySources, source)
	}

	groupsStartedAt := time.Now()
	groupsBySource := filmplaylist.GetMultiplePlayGroupsBySourcesAndKeys(querySources, names)
	logSlowIndexServiceStep("multipleSource.playlists", groupsStartedAt, "id", snapshot.Mid, "sources", len(querySources), "keys", len(names))
	master := filmshared.IdentityFromFilmListSnapshot(*snapshot)
	if detail != nil {
		incoming := filmshared.IdentityFromMovieDetail(*detail)
		if len(incoming.Episodes) > 0 {
			master.Episodes = incoming.Episodes
		}
	}
	for _, source := range querySources {
		groups := filmshared.FilterPlayGroupsByWorkShape(master, groupsBySource[source.Id])
		if len(groups) > 0 {
			if source.DomainReplaceRules != "" {
				rules := utils.ParseDomainReplaceRules(source.DomainReplaceRules)
				if len(rules) > 0 {
					for gi := range groups {
						for li := range groups[gi].LinkList {
							groups[gi].LinkList[li].Link = utils.ApplyDomainReplaceRules(groups[gi].LinkList[li].Link, rules)
						}
					}
				}
			}
			playList = append(playList, groups...)
		}
	}

	logSlowIndexServiceStep("multipleSource.total", startedAt, "id", snapshot.Mid, "sources", len(querySources), "keys", len(names))
	return playList
}

func buildPrimaryPlaySources(snapshot *model.FilmListSnapshot, detail *model.MovieDetail) []model.PlayLinkVo {
	if detail == nil || len(detail.PlayList) == 0 {
		return make([]model.PlayLinkVo, 0)
	}

	siteName := ""
	var rules []utils.DomainReplaceRule
	sourceID := ""
	if snapshot != nil && snapshot.SourceId != "" {
		sourceID = snapshot.SourceId
		if source := repository.FindCollectSourceById(snapshot.SourceId); source != nil {
			siteName = source.Name
			if source.DomainReplaceRules != "" {
				rules = utils.ParseDomainReplaceRules(source.DomainReplaceRules)
			}
		}
	}

	playList := make([]model.PlayLinkVo, 0, len(detail.PlayList))
	for index, links := range detail.PlayList {
		if len(links) == 0 {
			continue
		}

		rawName := strings.TrimSpace(resolvePrimarySourceName(detail.PlayFrom, index))
		sourceName := filmshared.BuildDisplaySourceName(siteName, rawName, index, len(detail.PlayList))
		groupID := filmshared.BuildPlayGroupID(sourceID, rawName, index, len(detail.PlayList))

		adaptedLinks := rewriteURLGroup(links, rules)

		playList = append(playList, model.PlayLinkVo{
			Id:       groupID,
			SourceId: sourceID,
			Name:     sourceName,
			LinkList: adaptedLinks,
		})
	}

	return playList
}

func rewriteURLGroups(groups [][]model.MovieUrlInfo, rules []utils.DomainReplaceRule) [][]model.MovieUrlInfo {
	if len(rules) == 0 || groups == nil {
		return groups
	}
	out := make([][]model.MovieUrlInfo, len(groups))
	for i, links := range groups {
		out[i] = rewriteURLGroup(links, rules)
	}
	return out
}

func rewriteURLGroup(links []model.MovieUrlInfo, rules []utils.DomainReplaceRule) []model.MovieUrlInfo {
	if len(rules) == 0 || links == nil {
		return links
	}
	out := make([]model.MovieUrlInfo, len(links))
	for i, link := range links {
		out[i] = model.MovieUrlInfo{
			Episode: link.Episode,
			Link:    utils.ApplyDomainReplaceRules(link.Link, rules),
		}
	}
	return out
}

func resolvePrimarySourceName(playFrom []string, index int) string {
	if index < 0 || index >= len(playFrom) {
		return ""
	}
	return playFrom[index]
}
