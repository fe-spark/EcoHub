package handler

import (
	"net/url"
	"strconv"
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/service"

	"github.com/gin-gonic/gin"
)

type IndexHandler struct{}

var IndexHd = new(IndexHandler)

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

func wrapProxyLink(link string) string {
	if link == "" {
		return link
	}
	if strings.HasPrefix(link, "/api/proxy/video?url=") {
		return link
	}
	if strings.HasPrefix(link, "http://") || strings.HasPrefix(link, "https://") {
		return "/api/proxy/video?url=" + url.QueryEscape(link)
	}
	return link
}

// Index 首页数据
func (h *IndexHandler) Index(c *gin.Context) {
	data := service.IndexSvc.IndexPage()
	dto.Success(data, "首页数据获取成功", c)
}

// CategoriesInfo 分类信息获取
func (h *IndexHandler) CategoriesInfo(c *gin.Context) {
	data := service.IndexSvc.GetNavCategory()
	if len(data) <= 0 {
		dto.Failed("暂无分类信息", c)
		return
	}
	dto.Success(data, "分类信息获取成功", c)
}

// FilmPlayInfo 影视播放页数据
func (h *IndexHandler) FilmPlayInfo(c *gin.Context) {
	id, err := strconv.Atoi(c.DefaultQuery("id", "0"))
	if err != nil {
		dto.Failed("请求异常,暂无影片信息!!!", c)
		return
	}
	playFrom := c.DefaultQuery("playFrom", "")
	episode, err := strconv.Atoi(c.DefaultQuery("episode", "0"))
	if err != nil {
		dto.Failed("请求异常,暂无影片信息!!!", c)
		return
	}
	detail, err := service.IndexSvc.GetFilmDetail(id)
	if err != nil {
		dto.Failed("影片详情数据异常", c)
		return
	}
	if detail.Id == 0 {
		dto.Failed("暂无影片信息", c)
		return
	}
	useProxy := repository.GetSiteBasic().IsVideoProxy
	for i := range detail.List {
		var valid []model.MovieUrlInfo
		for _, ep := range detail.List[i].LinkList {
			if ep.Link != "" {
				if useProxy {
					ep.Link = wrapProxyLink(ep.Link)
				}
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
				if episode < len(v.LinkList) {
					currentPlay = v.LinkList[episode]
				} else {
					currentPlay = v.LinkList[0]
					episode = 0
				}
			}
			break
		}
	}

	page := dto.Page{Current: 0, PageSize: 14}
	relateMovie := service.IndexSvc.RelateMovie(detail.MovieDetail, &page)
	dto.Success(gin.H{
		"detail":          detail,
		"current":         currentPlay,
		"currentPlayFrom": playFrom,
		"currentEpisode":  episode,
		"relate":          relateMovie,
	}, "影片播放信息获取成功", c)
}

// SearchFilm 通过片名模糊匹配库存中的信息
func (h *IndexHandler) SearchFilm(c *gin.Context) {
	keyword := c.DefaultQuery("keyword", "")
	page := dto.GetPageParams(c)
	page.PageSize = 10
	bl := service.IndexSvc.SearchFilmInfo(strings.TrimSpace(keyword), page)
	if page.Total <= 0 {
		dto.Failed("暂无相关影片信息", c)
		return
	}

	dto.Success(gin.H{"list": bl, "page": page}, "影片搜索成功", c)
}

// FilmTagSearch 通过tag获取满足条件的对应影片
func (h *IndexHandler) FilmTagSearch(c *gin.Context) {
	params := model.SearchTagsVO{}
	pidStr := c.DefaultQuery("Pid", "")
	cidStr := c.DefaultQuery("Category", "")
	yStr := c.DefaultQuery("Year", "")
	if pidStr == "" {
		dto.Failed("缺少分类信息", c)
		return
	}
	params.Pid, _ = strconv.ParseInt(pidStr, 10, 64)
	params.Cid, _ = strconv.ParseInt(cidStr, 10, 64)
	params.Plot = c.DefaultQuery("Plot", "")
	params.Area = c.DefaultQuery("Area", "")
	params.Language = c.DefaultQuery("Language", "")
	params.Year = yStr
	params.Sort = c.DefaultQuery("Sort", "latest_source_stamp")

	page := dto.GetPageParams(c)
	page.PageSize = 49

	cat := service.IndexSvc.GetPidCategory(params.Pid)

	list := service.IndexSvc.GetFilmsByTags(params, page)
	if list == nil {
		list = make([]model.MovieBasicInfo, 0)
	}
	searchTags := service.IndexSvc.SearchTags(params)

	var titleObj *model.Category
	if cat != nil {
		titleObj = &model.Category{
			Id:        cat.Id,
			Pid:       cat.Pid,
			Name:      cat.Name,
			Alias:     cat.Alias,
			Show:      cat.Show,
			Sort:      cat.Sort,
			CreatedAt: cat.CreatedAt,
			UpdatedAt: cat.UpdatedAt,
		}
	}

	dto.Success(gin.H{
		"title":  titleObj,
		"list":   list,
		"search": searchTags,
		"params": map[string]string{
			"Pid":      pidStr,
			"Category": cidStr,
			"Plot":     params.Plot,
			"Area":     params.Area,
			"Language": params.Language,
			"Year":     yStr,
			"Sort":     params.Sort,
		},
		"page": page,
	}, "分类影片数据获取成功", c)
}

// FilmClassify  影片分类首页数据展示
func (h *IndexHandler) FilmClassify(c *gin.Context) {
	pidStr := c.DefaultQuery("Pid", "")
	if pidStr == "" {
		dto.Failed("主分类信息获取异常", c)
		return
	}
	pid, _ := strconv.ParseInt(pidStr, 10, 64)
	title := service.IndexSvc.GetPidCategory(pid)
	page := dto.GetPageParams(c)
	page.PageSize = 21
	dto.Success(gin.H{
		"title":   title,
		"content": service.IndexSvc.GetFilmClassify(pid, page),
	}, "分类影片信息获取成功", c)
}
