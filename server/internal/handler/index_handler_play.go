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

func resolvePlayableSourceID(playSources []model.PlayLinkVo, playFrom string) string {
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

// FilmPlayInfo 影视播放页数据（统一聚合片库，支持首选源与缺集降级）
func (h *IndexHandler) FilmPlayInfo(c *gin.Context) {
	totalStartedAt := time.Now()
	id, err := strconv.Atoi(c.DefaultQuery("id", "0"))
	if err != nil || id <= 0 {
		dto.Failed("请求异常,暂无影片信息!!!", c)
		return
	}
	preferredSource := strings.TrimSpace(c.Query("preferredSource"))
	playFrom := strings.TrimSpace(c.Query("playFrom"))
	episode, err := strconv.Atoi(c.DefaultQuery("episode", "0"))
	if err != nil || episode < 0 {
		episode = 0
	}
	detailStartedAt := time.Now()
	detail, err := service.IndexSvc.GetFilmDetailWithPreferred(id, preferredSource)
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
