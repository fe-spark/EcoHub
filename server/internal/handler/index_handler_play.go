package handler

import (
	"strconv"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"

	"github.com/gin-gonic/gin"
)

func parseOptionalQueryInt(raw string) (int, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.Atoi(raw)
	if err != nil {
		return 0, false
	}
	return value, true
}

func parseOptionalQueryInt64(raw string) (int64, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0, true
	}
	value, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0, false
	}
	return value, true
}

func hasPlayableFilmDetail(id int, detail model.MovieDetailVo) bool {
	if id > 0 {
		return detail.Id > 0
	}
	return strings.TrimSpace(detail.Name) != "" && len(detail.List) > 0
}

func resolvePlayableSourceID(playSources []model.PlayLinkVo, preferred string) string {
	if preferred != "" {
		for _, source := range playSources {
			if source.Id == preferred && len(source.LinkList) > 0 {
				return source.Id
			}
		}

		for _, source := range playSources {
			if source.SourceId == preferred && len(source.LinkList) > 0 {
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

// FilmPlayInfo 影视播放页数据（本地数据库入库影片）
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
	episode, err := strconv.Atoi(c.DefaultQuery("episode", "0"))
	if err != nil || episode < 0 {
		episode = 0
	}
	detailStartedAt := time.Now()
	detail, err := service.IndexSvc.GetFilmDetail(id)
	logSlowIndexStep("FilmPlayInfo.GetFilmDetail", detailStartedAt, "id", id)
	if err != nil {
		dto.Failed("影片详情数据异常", c)
		return
	}
	if detail.Id <= 0 {
		dto.Failed("暂无影片信息", c)
		return
	}
	for i := range detail.List {
		var valid []model.MovieUrlInfo
		for _, ep := range detail.List[i].LinkList {
			if ep.Link != "" {
				valid = append(valid, ep)
			}
		}
		detail.List[i].LinkList = valid
	}
	if len(detail.List) > 0 {
		playFrom = resolvePlayableSourceID(detail.List, playFrom)
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
		"detail":          detail,
		"current":         currentPlay,
		"currentPlayFrom": playFrom,
		"currentEpisode":  episode,
		"relate":          []model.MovieBasicInfo{},
	}, "影片播放信息获取成功", c)
}

// LiveFilmPlayInfo 现场播放页数据（独立直连第三方采集源，不写库、不走 mid 缓存）
func (h *IndexHandler) LiveFilmPlayInfo(c *gin.Context) {
	totalStartedAt := time.Now()
	source := strings.TrimSpace(c.Query("source"))
	if source == "" {
		source = strings.TrimSpace(c.Query("playFrom"))
	}
	if source == "" {
		dto.Failed("缺少采集源参数", c)
		return
	}

	sid, err := strconv.ParseInt(strings.TrimSpace(c.Query("sid")), 10, 64)
	if err != nil || sid <= 0 {
		dto.Failed("请求异常,暂无影片信息!!!", c)
		return
	}

	episode, err := strconv.Atoi(c.DefaultQuery("episode", "0"))
	if err != nil || episode < 0 {
		episode = 0
	}

	detailStartedAt := time.Now()
	detail, err := service.IndexSvc.GetLiveFilmDetail(source, sid)
	logSlowIndexStep("LiveFilmPlayInfo.GetLiveFilmDetail", detailStartedAt, "source", source, "sid", sid)
	if err != nil {
		dto.Failed("影片详情数据异常", c)
		return
	}
	if strings.TrimSpace(detail.Name) == "" || len(detail.List) == 0 {
		dto.Failed("暂无影片信息", c)
		return
	}

	for i := range detail.List {
		var valid []model.MovieUrlInfo
		for _, ep := range detail.List[i].LinkList {
			if ep.Link != "" {
				valid = append(valid, ep)
			}
		}
		detail.List[i].LinkList = valid
	}

	if len(detail.List) > 0 {
		source = resolvePlayableSourceID(detail.List, source)
	}

	var currentPlay model.MovieUrlInfo
	for _, v := range detail.List {
		if v.Id == source {
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

	logSlowIndexStep("LiveFilmPlayInfo.total", totalStartedAt, "source", source, "sid", sid)
	dto.Success(gin.H{
		"detail":          detail,
		"current":         currentPlay,
		"currentPlayFrom": source,
		"currentEpisode":  episode,
		"relate":          []model.MovieBasicInfo{},
	}, "现场影片播放信息获取成功", c)
}

// FilmRelate 影视播放页相关推荐数据
func (h *IndexHandler) FilmRelate(c *gin.Context) {
	startedAt := time.Now()
	id, err := strconv.Atoi(c.DefaultQuery("id", "0"))
	if err != nil || id <= 0 {
		dto.Failed("请求异常,暂无影片信息!!!", c)
		return
	}

	page := dto.Page{Current: 0, PageSize: 14}
	relateMovie := service.IndexSvc.RelateMovie(int64(id), &page)
	logSlowIndexStep("FilmRelate.total", startedAt, "id", id)
	dto.Success(relateMovie, "相关推荐获取成功", c)
}

// LiveFilmRelate 现场播放同类相关推荐数据
func (h *IndexHandler) LiveFilmRelate(c *gin.Context) {
	startedAt := time.Now()
	source := strings.TrimSpace(c.Query("source"))
	if source == "" {
		source = strings.TrimSpace(c.Query("playFrom"))
	}
	if source == "" {
		dto.Failed("缺少采集源参数", c)
		return
	}

	cid, _ := strconv.ParseInt(strings.TrimSpace(c.Query("cid")), 10, 64)
	excludeSid, _ := strconv.ParseInt(strings.TrimSpace(c.Query("sid")), 10, 64)
	if excludeSid <= 0 {
		excludeSid, _ = strconv.ParseInt(strings.TrimSpace(c.Query("excludeSid")), 10, 64)
	}

	list, err := service.IndexSvc.GetLiveRelateFilms(source, cid, excludeSid)
	logSlowIndexStep("LiveFilmRelate.total", startedAt, "source", source, "cid", cid)
	if err != nil {
		dto.Failed("获取相关推荐失败: "+err.Error(), c)
		return
	}

	dto.Success(list, "相关推荐获取成功", c)
}

