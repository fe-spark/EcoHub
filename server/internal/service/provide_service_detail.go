package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	filmsnapshot "server/internal/repository/film/snapshot"

	"github.com/redis/go-redis/v9"
)

const maxProvideVodDetailBatch = 100

// GetVodDetail 获取视频详情（带播放列表）
func (p *ProvideService) GetVodDetail(ids []string, sourceId string) ([]model.FilmDetail, error) {
	if len(ids) == 0 {
		return []model.FilmDetail{}, nil
	}

	mids := make([]int64, 0, len(ids))
	for _, idStr := range ids {
		idInt, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
		if err != nil || idInt <= 0 {
			continue
		}
		mids = append(mids, idInt)
	}
	if len(mids) == 0 {
		return []model.FilmDetail{}, nil
	}
	sourceId = resolveProvideSourceID(sourceId)
	if len(mids) > maxProvideVodDetailBatch {
		mids = mids[:maxProvideVodDetailBatch]
	}

	version := filmsnapshot.GetActiveReadModelVersion()
	snapshots, err := filmsnapshot.GetProjectedSnapshotsByMidsOrdered(version, mids)
	if err != nil {
		return nil, err
	}
	if len(snapshots) == 0 {
		return []model.FilmDetail{}, nil
	}

	cachedVos := make(map[int64]*model.MovieDetailVo, len(snapshots))
	if db.Rdb != nil {
		pipe := db.Rdb.Pipeline()
		cmds := make(map[int64]*redis.StringCmd, len(snapshots))
		seenPiped := make(map[int64]struct{}, len(snapshots))
		for _, s := range snapshots {
			if _, ok := seenPiped[s.Mid]; ok {
				continue
			}
			seenPiped[s.Mid] = struct{}{}
			cmds[s.Mid] = pipe.Get(db.Cxt, fmt.Sprintf("%s:%d", config.FilmPlayInfoKey, s.Mid))
		}
		_, _ = pipe.Exec(db.Cxt)

		for mid, cmd := range cmds {
			val, err := cmd.Result()
			if err == nil && val != "" {
				if val == "{}" {
					cachedVos[mid] = &model.MovieDetailVo{}
				} else {
					var vo model.MovieDetailVo
					if json.Unmarshal([]byte(val), &vo) == nil {
						cachedVos[mid] = &vo
					}
				}
			}
		}
	}

	var uncachedMids []int64
	for _, s := range snapshots {
		if _, ok := cachedVos[s.Mid]; !ok {
			uncachedMids = append(uncachedMids, s.Mid)
		}
	}

	var batchPlaylists map[int64][]model.PlayLinkVo
	if len(uncachedMids) > 0 {
		var err error
		batchPlaylists, err = BatchGetPlayPlaylistsByMids(uncachedMids)
		if err != nil {
			return nil, err
		}
	}

	detailList := make([]model.FilmDetail, 0, len(snapshots))
	var toCacheVo map[int64]model.MovieDetailVo
	if db.Rdb != nil && len(uncachedMids) > 0 {
		toCacheVo = make(map[int64]model.MovieDetailVo, len(uncachedMids))
	}
	for _, s := range snapshots {
		voPtr, ok := cachedVos[s.Mid]
		var vo model.MovieDetailVo
		if ok && voPtr != nil {
			vo = *voPtr
		} else {
			movieDetail, _ := filmsnapshot.GetMovieDetailBySnapshot(s)
			if movieDetail != nil {
				vo.MovieDetail = *movieDetail
			}
			vo.List = batchPlaylists[s.Mid]
			if toCacheVo != nil {
				toCacheVo[s.Mid] = vo
			}
		}

		if len(vo.List) == 0 && s.Name == "" {
			continue
		}

		detail, err := formatProvideFilmDetail(s, vo, sourceId)
		if err != nil {
			return nil, err
		}
		detailList = append(detailList, detail)
	}

	if len(toCacheVo) > 0 && db.Rdb != nil {
		gen := filmsnapshot.PlayInfoGeneration()
		for mid, v := range toCacheVo {
			if b, err := json.Marshal(v); err == nil {
				storeFilmPlayInfoCache(fmt.Sprintf("%s:%d", config.FilmPlayInfoKey, mid), string(b), 12*time.Hour, gen)
			}
		}
	}

	return detailList, nil
}

func formatProvideFilmDetail(snapshot model.FilmListSnapshot, movieDetailVo model.MovieDetailVo, sourceId string) (model.FilmDetail, error) {
	typeID, typeName, err := resolveProvideListedType(snapshot, sourceId)
	if err != nil {
		return model.FilmDetail{}, err
	}
	rootTypeID, err := resolveProvideListedRootType(snapshot, sourceId)
	if err != nil {
		return model.FilmDetail{}, err
	}

	var playFromList []string
	var playUrlList []string

	for _, source := range movieDetailVo.List {
		playFromList = append(playFromList, providePlayFromName(source))

		var linkStrs []string
		for _, link := range source.LinkList {
			playLink := link.Link
			linkStrs = append(linkStrs, fmt.Sprintf("%s$%s", link.Episode, strings.ReplaceAll(playLink, "$", "")))
		}
		playUrlList = append(playUrlList, strings.Join(linkStrs, "#"))
	}

	vodPic := movieDetailVo.Picture
	if vodPic == "" {
		vodPic = snapshot.DisplayPicture()
	}

	vodYear := movieDetailVo.Year
	if vodYear == "" && snapshot.Year > 0 {
		vodYear = strconv.FormatInt(snapshot.Year, 10)
	}

	vodRemarks := snapshot.Remarks
	if vodRemarks == "" {
		vodRemarks = movieDetailVo.Remarks
	}

	return model.FilmDetail{
		VodID:       snapshot.Mid,
		TypeID:      typeID,
		TypeID1:     rootTypeID,
		TypeName:    typeName,
		VodName:     snapshot.Name,
		VodEn:       snapshot.Initial,
		VodTime:     resolveProvideSnapshotVodTime(snapshot),
		VodRemarks:  vodRemarks,
		VodPlayFrom: strings.Join(playFromList, "$$$"),
		VodPlayURL:  strings.Join(playUrlList, "$$$"),
		VodPic:      vodPic,
		VodSub:      movieDetailVo.SubTitle,
		VodClass:    movieDetailVo.ClassTag,
		VodActor:    movieDetailVo.Actor,
		VodDirector: movieDetailVo.Director,
		VodWriter:   movieDetailVo.Writer,
		VodBlurb:    movieDetailVo.Blurb,
		VodPubDate:  movieDetailVo.ReleaseDate,
		VodArea:     movieDetailVo.Area,
		VodLang:     movieDetailVo.Language,
		VodYear:     vodYear,
		VodState:    movieDetailVo.State,
		VodHits:     snapshot.Hits,
		VodScore:    movieDetailVo.DbScore,
		VodContent:  movieDetailVo.Content,
	}, nil
}
