package portal

import (
	"server/internal/mediaproxy"
	"server/internal/film"
	"server/internal/repobase"
	"net/url"
	"strconv"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
)

func resolvePlayableSourceID(playSources []model.PlayLinkVo, playFrom, primarySourceID string) string {
	if playFrom != "" {
		for _, source := range playSources {
			if source.Id == playFrom && len(source.LinkList) > 0 {
				return source.Id
			}
		}

		for _, source := range playSources {
			if source.SourceId == playFrom && len(source.LinkList) > 0 {
				return source.Id
			}
		}
	}

	// 没有指定线路，或指定线路已不存在：播主站。主站没有可播剧集时再退回第一条。
	if primarySourceID != "" {
		for _, source := range playSources {
			if source.SourceId == primarySourceID && len(source.LinkList) > 0 {
				return source.Id
			}
		}
	}

	for _, source := range playSources {
		if len(source.LinkList) > 0 {
			return source.Id
		}
	}

	if len(playSources) > 0 {
		return playSources[0].Id
	}

	return ""
}

// FilmPlayInfo 影视播放页数据（统一聚合片库，按采集站顺序呈现独立线路）
func (h *IndexHandler) FilmPlayInfo(c *gin.Context) {
	totalStartedAt := time.Now()
	id, err := strconv.Atoi(c.DefaultQuery("id", "0"))
	if err != nil || id <= 0 {
		dto.Failed("请求异常,暂无影片信息!!!", c)
		return
	}
	playFrom := strings.TrimSpace(c.Query("playFrom"))
	if playFrom == "" {
		playFrom = strings.TrimSpace(c.Query("source"))
	}
	preferredSource := strings.TrimSpace(c.Query("preferredSource"))
	if preferredSource == "" {
		preferredSource = playFrom
	}
	episode, err := strconv.Atoi(c.DefaultQuery("episode", "0"))
	if err != nil || episode < 0 {
		episode = 0
	}
	detailStartedAt := time.Now()
	detail, err := IndexSvc.GetFilmDetailWithPreferred(id, preferredSource)
	logSlowIndexStep("FilmPlayInfo.GetFilmDetail", detailStartedAt, "id", id)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	if detail.Id <= 0 {
		dto.Failed("暂无影片信息", c)
		return
	}
	if c.Query("stored") == "1" {
		if err := film.UseStoredFilm(&detail); err != nil {
			dto.Failed(err.Error(), c)
			return
		}
	}
	var playableList []model.PlayLinkVo
	for _, src := range detail.List {
		var valid []model.MovieUrlInfo
		for _, ep := range src.LinkList {
			if isWebPlayableVideo(ep.Link) {
				valid = append(valid, ep)
			}
		}
		if len(valid) > 0 {
			src.LinkList = valid
			playableList = append(playableList, src)
		}
	}
	detail.List = playableList
	if len(detail.List) > 0 {
		detail.PlayList = make([][]model.MovieUrlInfo, len(detail.List))
		detail.PlayFrom = make([]string, len(detail.List))
		for i, v := range detail.List {
			detail.PlayList[i] = v.LinkList
			detail.PlayFrom[i] = v.Name
		}
	} else {
		detail.PlayList = nil
		detail.PlayFrom = nil
	}
	if len(detail.List) > 0 {
		primarySourceID := ""
		if primary := repobase.CurrentPrimarySource(); primary != nil {
			primarySourceID = primary.Id
		}
		playFrom = resolvePlayableSourceID(detail.List, playFrom, primarySourceID)
	}
	var currentPlay model.MovieUrlInfo
	for _, v := range detail.List {
		if v.Id == playFrom {
			if len(v.LinkList) > 0 {
				if episode >= 0 && episode < len(v.LinkList) {
					currentPlay = v.LinkList[episode]
				} else {
					currentPlay = v.LinkList[0]
					episode = 0
				}
			}
			break
		}
	}

	logSlowIndexStep("FilmPlayInfo.total", totalStartedAt, "id", id)
	dto.Success(gin.H{
		"detail":          mediaproxy.WrapMovieDetailVo(detail),
		"current":         currentPlay,
		"currentPlayFrom": playFrom,
		"currentEpisode":  episode,
		"relate":          []model.MovieBasicInfo{},
	}, "影片播放信息获取成功", c)
}

// FilmRelate 影视播放页相关推荐数据
func (h *IndexHandler) FilmRelate(c *gin.Context) {
	startedAt := time.Now()
	id, err := strconv.Atoi(c.DefaultQuery("id", "0"))
	if err != nil || id <= 0 {
		dto.Failed("请求异常,暂无影片信息!!!", c)
		return
	}

	source := strings.TrimSpace(c.Query("source"))
	if source == "" {
		source = strings.TrimSpace(c.Query("playFrom"))
	}
	if source == "" {
		source = strings.TrimSpace(c.Query("preferredSource"))
	}

	page := dto.Page{Current: 0, PageSize: 14}
	var relateMovie []model.MovieBasicInfo
	var relateErr error
	if source != "" {
		relateMovie, relateErr = IndexSvc.RelateMovie(int64(id), &page, source)
	} else {
		relateMovie, relateErr = IndexSvc.RelateMovie(int64(id), &page)
	}
	if relateErr != nil {
		dto.Failed(relateErr.Error(), c)
		return
	}
	logSlowIndexStep("FilmRelate.total", startedAt, "id", id)
	dto.Success(mediaproxy.WrapMovieBasicInfos(relateMovie), "相关推荐获取成功", c)
}

func isWebPlayableVideo(link string) bool {
	raw := strings.TrimSpace(link)
	if raw == "" {
		return false
	}
	u, err := url.Parse(raw)
	if err != nil {
		return false
	}
	if u.Scheme != "" {
		scheme := strings.ToLower(u.Scheme)
		if scheme != "http" && scheme != "https" {
			return false
		}
	}
	path := strings.ToLower(u.Path)
	return strings.HasSuffix(path, ".m3u8") ||
		strings.HasSuffix(path, ".mp4") ||
		strings.HasSuffix(path, ".flv") ||
		strings.HasSuffix(path, ".webm")
}
