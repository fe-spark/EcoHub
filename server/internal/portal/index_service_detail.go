package portal

import (
	"encoding/json"
	"errors"
	"fmt"
	"math/rand"
	"strings"
	"time"

	"golang.org/x/sync/singleflight"
	"server/internal/config"
	filmrepo "server/internal/film"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/taxonomy"
)

var (
	filmDetailSfGroup singleflight.Group
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

const filmDetailMissingSentinel = "__MISSING__"
const filmDetailMissingTTL = 45 * time.Second

// GetFilmDetail 影片详情信息页面处理（基础聚合版，按采集站权重排序）。
func (i *IndexService) GetFilmDetail(id int) (model.MovieDetailVo, error) {
	if id <= 0 {
		return model.MovieDetailVo{}, nil
	}

	cacheKey := fmt.Sprintf("%s:%d", config.FilmPlayInfoKey, id)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" && data != "{}" {
			if data == filmDetailMissingSentinel {
				return model.MovieDetailVo{}, nil
			}
			var cached model.MovieDetailVo
			if json.Unmarshal([]byte(data), &cached) == nil {
				if !taxonomy.IsCategoryVisible(cached.Cid, cached.Pid) {
					return model.MovieDetailVo{}, errors.New("影片所在分类已下架或停用")
				}
				return cached, nil
			}
		}
	}

	val, err, _ := filmDetailSfGroup.Do(cacheKey, func() (any, error) {
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" && data != "{}" {
				if data == filmDetailMissingSentinel {
					return model.MovieDetailVo{}, nil
				}
				var cached model.MovieDetailVo
				if json.Unmarshal([]byte(data), &cached) == nil {
					if !taxonomy.IsCategoryVisible(cached.Cid, cached.Pid) {
						return model.MovieDetailVo{}, errors.New("影片所在分类已下架或停用")
					}
					return cached, nil
				}
			}
		}

		playGen := filmsnapshot.PlayInfoGeneration()
		startedAt := time.Now()
		version := filmsnapshot.GetActiveReadModelVersion()
		snapshotStartedAt := time.Now()
		snapshot, snapErr := filmsnapshot.GetSnapshotByMid(version, int64(id))
		logSlowIndexServiceStep("GetFilmDetail.snapshot", snapshotStartedAt, "id", id)
		if snapErr != nil {
			return model.MovieDetailVo{}, snapErr
		}
		if snapshot == nil {
			storeFilmPlayInfoCache(cacheKey, filmDetailMissingSentinel, filmDetailMissingTTL, playGen)
			return model.MovieDetailVo{}, nil
		}
		if !taxonomy.IsCategoryVisible(snapshot.Cid, snapshot.Pid) {
			return model.MovieDetailVo{}, errors.New("影片所在分类已下架或停用")
		}
		detailStartedAt := time.Now()
		movieDetail, localUpdateTime := filmsnapshot.GetMovieDetailBySnapshot(*snapshot)
		logSlowIndexServiceStep("GetFilmDetail.detail", detailStartedAt, "id", id)
		if movieDetail == nil {
			filmsnapshot.DeleteActiveSnapshotsByMids(snapshot.Mid)
			storeFilmPlayInfoCache(cacheKey, filmDetailMissingSentinel, filmDetailMissingTTL, playGen)
			return model.MovieDetailVo{}, nil
		}

		playList, downloadList, err := filmrepo.LoadPlayAndDownloadSourcesByMid(int64(id))
		if err != nil {
			return nil, err
		}
		movieDetail.DownloadList = downloadList
		res := model.MovieDetailVo{
			MovieDetail:     *movieDetail,
			LocalUpdateTime: localUpdateTime,
			UpdateReason:    strings.TrimSpace(snapshot.UpdateReason),
			List:            playList,
		}
		res.PlayList, res.PlayFrom = filmrepo.PlayGroupsFromPlayLinkVos(playList)

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

// GetFilmDetailWithPreferred 按采集站顺序组织播放线路；preferredSource 仅重排，不混拼剧集。
func (i *IndexService) GetFilmDetailWithPreferred(id int, preferredSource string) (model.MovieDetailVo, error) {
	detail, err := i.GetFilmDetail(id)
	if err != nil {
		return model.MovieDetailVo{}, err
	}
	if preferredSource != "" && len(detail.List) > 0 {
		detail.List = OrganizePlaySources(detail.List, preferredSource)
		detail.PlayList, detail.PlayFrom = filmrepo.PlayGroupsFromPlayLinkVos(detail.List)
	}
	return detail, nil
}

// OrganizePlaySources 根据偏好站重排播放线路。
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
		if cp.SourceId == preferredSource || cp.Id == preferredSource {
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

	result := make([]model.PlayLinkVo, 0, len(preferredLines)+len(otherLines))
	result = append(result, preferredLines...)
	result = append(result, otherLines...)
	return result
}
