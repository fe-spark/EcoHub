package handler

import (
	"errors"
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

func normalizeMediaURL(raw, baseURL string) string {
	if raw == "" || baseURL == "" {
		return raw
	}
	if strings.HasPrefix(raw, "http://") || strings.HasPrefix(raw, "https://") {
		return raw
	}
	if strings.HasPrefix(raw, "//") {
		if strings.HasPrefix(baseURL, "https://") {
			return "https:" + raw
		}
		return "http:" + raw
	}
	if strings.HasPrefix(raw, "/") {
		return baseURL + raw
	}
	return baseURL + "/" + raw
}

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

	classList, filters := service.ProvideSvc.GetClassList(sourceId)
	if classList == nil {
		classList = []model.FilmClass{}
	}

	baseURL, _ := resolveProvideBaseURL(c)

	normalizeVodListPic := func(list []model.FilmList) []model.FilmList {
		if len(list) == 0 {
			return list
		}
		cloned := make([]model.FilmList, len(list))
		copy(cloned, list)
		if baseURL == "" {
			return cloned
		}
		for i := range cloned {
			cloned[i].VodPic = normalizeMediaURL(cloned[i].VodPic, baseURL)
		}
		return cloned
	}

	normalizeDetailListPic := func(list []model.FilmDetail) []model.FilmDetail {
		if len(list) == 0 {
			return list
		}
		cloned := make([]model.FilmDetail, len(list))
		copy(cloned, list)
		if baseURL == "" {
			return cloned
		}
		for i := range cloned {
			cloned[i].VodPic = normalizeMediaURL(cloned[i].VodPic, baseURL)
		}
		return cloned
	}

	switch ac {
	case "list":
		page, pagecount, total, vodList, err := service.ProvideSvc.GetVodList(sourceId, t, cid, pg, wd, h_param, year, area, lang, plot, sort, limit)
		if err != nil {
			c.JSON(200, gin.H{"code": 0, "msg": err.Error()})
			return
		}
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
			"list":        normalizeVodListPic(vodList),
			"class":       classList,
			"filters":     filters,
		})
	case "videolist", "detail":
		var idsArr []string
		if ids != "" {
			idsArr = strings.Split(ids, ",")
			vodList := service.ProvideSvc.GetVodDetail(idsArr, sourceId)
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
				"list":      normalizeDetailListPic(vodList),
				"class":     classList,
				"filters":   filters,
			})
		} else {
			page, pagecount, total, vodListSimple, err := service.ProvideSvc.GetVodList(sourceId, t, cid, pg, wd, h_param, year, area, lang, plot, sort, limit)
			if err != nil {
				c.JSON(200, gin.H{"code": 0, "msg": err.Error()})
				return
			}
			var _idsArr []string
			for _, v := range vodListSimple {
				_idsArr = append(_idsArr, strconv.FormatInt(v.VodID, 10))
			}
			detailList := service.ProvideSvc.GetVodDetail(_idsArr, sourceId)
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
				"list":        normalizeDetailListPic(detailList),
				"class":       classList,
				"filters":     filters,
			})
		}

	default:
		page, pagecount, total, vodList, err := service.ProvideSvc.GetVodList(sourceId, t, cid, pg, wd, h_param, year, area, lang, plot, sort, limit)
		if err != nil {
			c.JSON(200, gin.H{"code": 0, "msg": err.Error()})
			return
		}
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
			"list":        normalizeVodListPic(vodList),
			"class":       classList,
			"filters":     filters,
		})
	}
}

// HandleProvideConfig 提供给 TVBox/影视仓 的一键网络配置 (config.json)。
// 对应路由：
//   - GET /api/provide/tvbox (推荐：语义明确的标准端点)
//   - GET /api/provide/config (Deprecated: 早期路径别名，后续主版本计划移除)
//
// 注意：EcoHub 官方原生客户端（OHOS / Android）请走专属软件源端点 HandleProvideApp (/api/provide/app)。
func (h *ProvideHandler) HandleProvideConfig(c *gin.Context) {
	baseURL, err := resolveProvideBaseURL(c)
	if err != nil {
		c.JSON(500, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	key := strings.TrimSpace(c.Query("key"))
	cfg, err := service.ProvideSvc.GetTVBoxConfig(baseURL, key)
	if err != nil {
		c.JSON(500, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	c.JSON(200, cfg)
}

// HandleProvideApp 提供给 EcoHub 官方原生客户端（OHOS / Android）的软件源描述配置
func (h *ProvideHandler) HandleProvideApp(c *gin.Context) {
	baseURL, err := resolveProvideBaseURL(c)
	if err != nil {
		c.JSON(500, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	res, err := service.ProvideSvc.GetAppSourceConfig(baseURL)
	if err != nil {
		c.JSON(500, gin.H{"code": 0, "msg": err.Error()})
		return
	}
	c.JSON(200, res)
}
