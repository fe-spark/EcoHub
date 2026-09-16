package service

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	filmsnapshot "server/internal/repository/film/snapshot"

	"github.com/redis/go-redis/v9"
)

const maxProvideVodDetailBatch = 100

// GetVodDetail 获取视频详情（带播放列表）
func (p *ProvideService) GetVodDetail(ids []string) []model.FilmDetail {
	if len(ids) == 0 {
		return []model.FilmDetail{}
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
		return []model.FilmDetail{}
	}
	if len(mids) > maxProvideVodDetailBatch {
		mids = mids[:maxProvideVodDetailBatch]
	}

	version := filmsnapshot.GetActiveReadModelVersion()
	snapshots := filmsnapshot.GetProjectedSnapshotsByMidsOrdered(version, mids)
	if len(snapshots) == 0 {
		return []model.FilmDetail{}
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

	detailList := make([]model.FilmDetail, 0, len(snapshots))
	for _, s := range snapshots {
		voPtr, ok := cachedVos[s.Mid]
		var vo model.MovieDetailVo
		if ok && voPtr != nil {
			vo = *voPtr
		} else {
			fetchedVo, err := IndexSvc.GetFilmDetail(int(s.Mid))
			if err != nil {
				continue
			}
			vo = fetchedVo
		}

		if vo.Id == 0 && vo.Name == "" {
			continue
		}

		detailList = append(detailList, formatProvideFilmDetail(s, vo))
	}

	return detailList
}

func formatProvideFilmDetail(snapshot model.FilmListSnapshot, movieDetailVo model.MovieDetailVo) model.FilmDetail {
	typeID, typeName := resolveProvideTypeFromSnapshot(snapshot)

	var playFromList []string
	var playUrlList []string

	for _, source := range movieDetailVo.List {
		playFromList = append(playFromList, source.Name)

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
		TypeID1:     resolveProvideCurrentRootCategoryIDFromSnapshot(snapshot),
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
	}
}
