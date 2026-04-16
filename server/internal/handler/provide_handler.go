package handler

import (
	"errors"
	"net/url"
	"strconv"
	"strings"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"

	"github.com/gin-gonic/gin"
)

type ProvideHandler struct{}

var ProvideHd = new(ProvideHandler)

func resolveProvideBaseURL(c *gin.Context) (string, error) {
	scheme := "http"
	if c.Request.TLS != nil {
		scheme = "https"
	}
	if forwardedProto := strings.TrimSpace(c.GetHeader("X-Forwarded-Proto")); forwardedProto != "" {
		scheme = forwardedProto
	}

	host := strings.TrimSpace(c.GetHeader("X-Forwarded-Host"))
	if host != "" {
		host = strings.TrimSpace(strings.Split(host, ",")[0])
	}
	if host == "" {
		host = strings.TrimSpace(c.Request.Host)
	}
	if host != "" {
		return scheme + "://" + host, nil
	}

	return "", errors.New("无法解析当前请求域名，请检查反向代理 Host/X-Forwarded-Host 配置")
}

// HandleProvide 提供给外界采集的 MacCMS 兼容接口
func (h *ProvideHandler) HandleProvide(c *gin.Context) {
	ac := c.Query("ac")
	t, _ := strconv.Atoi(c.DefaultQuery("t", "0"))
	// 通用分页参数提取
	paging := dto.GetPageParams(c)
	pg := paging.Current
	limit := paging.PageSize

	wd := c.Query("wd")
	h_param, _ := strconv.Atoi(c.DefaultQuery("h", "0"))
	ids := c.Query("ids")
	sourceId := c.Query("source")

	tid, err := strconv.Atoi(c.Query("tid"))
	if err == nil && tid > 0 {
		t = tid
	}
	cid, _ := strconv.ParseInt(c.Query("cid"), 10, 64)

	year := c.Query("year")
	area := c.Query("area")
	lang := c.Query("language")
	if lang == "" {
		lang = c.Query("lang")
	}
	plot := c.Query("plot")
	sort := c.Query("sort")

	// 选中采集站时，优先直连该采集站返回原始数据
	if sourceId != "" {
		directYear, _ := strconv.Atoi(year)
		raw, err := service.ProvideSvc.GetVodDirectBySource(sourceId, ac, t, pg, wd, h_param, ids, directYear, area, lang, plot, sort)
		if err != nil {
			c.JSON(200, gin.H{"code": 0, "msg": "采集站直连失败: " + err.Error()})
			return
		}
		c.Data(200, "application/json; charset=utf-8", raw)
		return
	}

	classList, filters := service.ProvideSvc.GetClassList()
	if classList == nil {
		classList = []model.FilmClass{}
	}
	if t <= 0 && wd == "" && ids == "" && len(classList) > 0 {
		t = int(classList[0].ID)
	}

	switch ac {
	case "list":
		page, pagecount, total, vodList := service.ProvideSvc.GetVodList(t, cid, pg, wd, h_param, year, area, lang, plot, sort, limit)
		if vodList == nil {
			vodList = []model.FilmList{}
		}
		c.JSON(200, gin.H{
			"code":        1,
			"msg":         "数据列表",
			"page":        page,
			"pagecount":   pagecount,
			"limit":       limit,
			"total":       total,
			"recordcount": total,
			"list":        vodList,
			"class":       classList,
			"filters":     filters,
		})
	case "videolist", "detail":
		var idsArr []string
		if ids != "" {
			idsArr = strings.Split(ids, ",")
			vodList := service.ProvideSvc.GetVodDetail(idsArr)
			if vodList == nil {
				vodList = []model.FilmDetail{}
			}
			c.JSON(200, gin.H{
				"code":      1,
				"msg":       "数据列表",
				"page":      1,
				"pagecount": 1,
				"limit":     "20",
				"total":     len(vodList),
				"list":      vodList,
				"class":     classList,
				"filters":   filters,
			})
		} else {
			page, pagecount, total, vodListSimple := service.ProvideSvc.GetVodList(t, cid, pg, wd, h_param, year, area, lang, plot, sort, limit)
			var _idsArr []string
			for _, v := range vodListSimple {
				_idsArr = append(_idsArr, strconv.FormatInt(v.VodID, 10))
			}
			detailList := service.ProvideSvc.GetVodDetail(_idsArr)
			if detailList == nil {
				detailList = []model.FilmDetail{}
			}
			c.JSON(200, gin.H{
				"code":        1,
				"msg":         "数据列表",
				"page":        page,
				"pagecount":   pagecount,
				"limit":       limit,
				"total":       total,
				"recordcount": total,
				"list":        detailList,
				"class":       classList,
				"filters":     filters,
			})
		}

	default:
		page, pagecount, total, vodList := service.ProvideSvc.GetVodList(t, cid, pg, wd, h_param, year, area, lang, plot, sort, limit)
		if vodList == nil {
			vodList = []model.FilmList{}
		}
		c.JSON(200, gin.H{
			"code":        1,
			"msg":         "数据列表",
			"page":        page,
			"pagecount":   pagecount,
			"limit":       limit,
			"total":       total,
			"recordcount": total,
			"list":        vodList,
			"class":       classList,
			"filters":     filters,
		})
	}
}

// HandleProvideConfig 提供给 TVBox/影视仓 的一键网络配置 (config.json)
func (h *ProvideHandler) HandleProvideConfig(c *gin.Context) {
	baseURL, err := resolveProvideBaseURL(c)
	if err != nil {
		c.JSON(500, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	apiPath := baseURL + "/api/provide/vod"

	sites := []gin.H{
		{
			"key":         "EcoHub",
			"name":        "🌟 EcoHub 私人影视库全量",
			"type":        1,
			"api":         apiPath,
			"searchable":  1,
			"quickSearch": 1,
			"filterable":  1,
		},
	}

	for _, source := range service.CollectSvc.GetFilmSourceList() {
		if !source.State {
			continue
		}
		sites = append(sites, gin.H{
			"key":         "source_" + source.Id,
			"name":        "📡 " + source.Name,
			"type":        1,
			"api":         apiPath + "?source=" + url.QueryEscape(source.Id),
			"searchable":  1,
			"quickSearch": 1,
			"filterable":  1,
		})
	}

	configJson := gin.H{
		"spider":    "",
		"wallpaper": "",
		"logo":      "",
		"sites":     sites,
	}

	c.JSON(200, configJson)
}
