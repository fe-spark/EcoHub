package portal

import (
	"encoding/json"
	"fmt"
	"strconv"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/film"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/taxonomy"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
)

const maxProvideVodDetailBatch = 100

var provideVodDetailSfGroup singleflight.Group

// GetVodDetail 获取视频详情（带播放列表）
func (p *ProvideService) GetVodDetail(ids []string) ([]model.FilmDetail, error) {
	if len(ids) == 0 {
		return []model.FilmDetail{}, nil
	}
	mids := make([]int64, 0, min(len(ids), maxProvideVodDetailBatch))
	seen := make(map[int64]struct{}, min(len(ids), maxProvideVodDetailBatch))
	for _, idStr := range ids {
		idInt, err := strconv.ParseInt(strings.TrimSpace(idStr), 10, 64)
		if err != nil || idInt <= 0 {
			continue
		}
		if _, dup := seen[idInt]; dup {
			continue
		}
		seen[idInt] = struct{}{}
		mids = append(mids, idInt)
		if len(mids) >= maxProvideVodDetailBatch {
			break
		}
	}
	if len(mids) == 0 {
		return []model.FilmDetail{}, nil
	}
	sourceId := primaryProvideSourceID()
	all := make([]model.FilmDetail, 0, len(mids))
	for start := 0; start < len(mids); start += maxProvideVodDetailBatch {
		end := start + maxProvideVodDetailBatch
		if end > len(mids) {
			end = len(mids)
		}
		part, err := p.getVodDetailMids(mids[start:end], sourceId)
		if err != nil {
			return nil, err
		}
		all = append(all, part...)
	}
	return all, nil
}

func (p *ProvideService) getVodDetailMids(mids []int64, sourceId string) ([]model.FilmDetail, error) {
	var sfKey strings.Builder
	sfKey.WriteString(sourceId)
	for _, mid := range mids {
		sfKey.WriteByte(':')
		sfKey.WriteString(strconv.FormatInt(mid, 10))
	}
	val, err, _ := provideVodDetailSfGroup.Do(sfKey.String(), func() (any, error) {
		return p.loadVodDetailMids(mids, sourceId)
	})
	if err != nil {
		return nil, err
	}
	return val.([]model.FilmDetail), nil
}

func (p *ProvideService) loadVodDetailMids(mids []int64, sourceId string) ([]model.FilmDetail, error) {
	playGen := filmsnapshot.PlayInfoGeneration()
	version := filmsnapshot.GetActiveReadModelVersion()
	snapshots, err := filmsnapshot.GetProjectedSnapshotsByMidsOrdered(version, mids)
	if err != nil {
		return nil, err
	}
	if len(snapshots) == 0 {
		return []model.FilmDetail{}, nil
	}

	vodCacheKey := func(mid int64) string {
		return fmt.Sprintf("%s:vod:%d", config.FilmPlayInfoKey, mid)
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
			cmds[s.Mid] = pipe.Get(db.Cxt, vodCacheKey(s.Mid))
		}
		_, _ = pipe.Exec(db.Cxt)

		for mid, cmd := range cmds {
			val, err := cmd.Result()
			if err != nil || val == "" || val == "{}" {
				continue
			}
			var vo model.MovieDetailVo
			if json.Unmarshal([]byte(val), &vo) == nil {
				cachedVos[mid] = &vo
			}
		}
	}

	categoryVisible := filmsnapshot.SourceCategoryVisibility(sourceId)
	isVisible := func(s model.FilmListSnapshot) bool {
		return taxonomy.IsCategoryVisible(s.Cid, s.Pid) && categoryVisible(s.CategoryKey, s.RootCategoryKey, s.Cid, s.Pid)
	}
	var uncachedMids []int64
	for _, s := range snapshots {
		if !isVisible(s) {
			continue
		}
		if _, ok := cachedVos[s.Mid]; !ok {
			uncachedMids = append(uncachedMids, s.Mid)
		}
	}

	var batchPlaylists map[int64][]model.PlayLinkVo
	var broken map[int64]struct{}
	if len(uncachedMids) > 0 {
		var err error
		batchPlaylists, broken, err = film.BatchGetPlayPlaylistsByMids(uncachedMids)
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
		if !isVisible(s) {
			continue
		}
		if _, bad := broken[s.Mid]; bad {
			continue
		}
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
			if movieDetail != nil && toCacheVo != nil {
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
		batchCache := make(map[string]model.MovieDetailVo, len(toCacheVo))
		for mid, v := range toCacheVo {
			batchCache[vodCacheKey(mid)] = v
		}
		storeFilmPlayInfoBatchCache(batchCache, 12*time.Hour, playGen)
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

	vodPlayFrom, vodPlayURL := formatProvidePlayLists(movieDetailVo.List)

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
		VodPlayFrom: vodPlayFrom,
		VodPlayURL:  vodPlayURL,
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

// formatProvidePlayLists 组装电视端 vod_play_from 与 vod_play_url：手动线路置顶，全量下发所有线路。
func formatProvidePlayLists(sources []model.PlayLinkVo) (string, string) {
	if len(sources) == 0 {
		return "", ""
	}
	var manualFrom, manualURL []string
	var normalFrom, normalURL []string

	for _, source := range sources {
		isManual := source.IsManual || source.SourceId == "manual"
		var linkStrs []string
		for _, link := range source.LinkList {
			linkStrs = append(linkStrs, providePlayItem(link.Episode, link.Link))
		}
		name := strings.TrimSpace(source.Name)
		joined := strings.Join(linkStrs, "#")
		if isManual {
			manualFrom = append(manualFrom, name)
			manualURL = append(manualURL, joined)
		} else {
			normalFrom = append(normalFrom, name)
			normalURL = append(normalURL, joined)
		}
	}
	allFrom := append(manualFrom, normalFrom...)
	allURL := append(manualURL, normalURL...)

	return strings.Join(allFrom, "$$$"), strings.Join(allURL, "$$$")
}

// providePlayItem 把一集拼成电视端的「集名$地址」。集名里的分隔符改成全角，地址里的分隔符改成百分号编码。
func providePlayItem(episode, link string) string {
	episode = strings.NewReplacer("$", "＄", "#", "＃").Replace(episode)
	link = strings.NewReplacer("$", "%24", "#", "%23").Replace(link)
	return episode + "$" + link
}
