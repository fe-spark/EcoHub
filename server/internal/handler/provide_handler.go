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
		if i := strings.Index(forwardedProto, ","); i >= 0 {
			forwardedProto = strings.TrimSpace(forwardedProto[:i])
		}
		if forwardedProto != "http" && forwardedProto != "https" {
			return "", errors.New("无法解析当前请求协议")
		}
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

	classList, filters, classErr := service.ProvideSvc.GetClassList(sourceId)
	if classErr != nil {
		c.JSON(200, gin.H{"code": 0, "msg": classErr.Error()})
		return
	}
	if classList == nil {
		classList = []model.FilmClass{}
	}

	baseURL, err := resolveProvideBaseURL(c)
	if err != nil {
		c.JSON(200, gin.H{"code": 0, "msg": err.Error()})
		return
	}

	writeProvide := func(page, pagecount, total int, list any, withRecord bool) {
		body := gin.H{
			"code":      1,
			"msg":       "数据列表",
			"page":      page,
			"pagecount": pagecount,
			"limit":     strconv.Itoa(limit),
			"total":     total,
			"list":      list,
			"class":     classList,
			"filters":   filters,
		}
		if withRecord {
			body["recordcount"] = total
		}
		c.JSON(200, body)
	}

	switch ac {
	case "videolist", "detail":
		if ids != "" {
			vodList, err := service.ProvideSvc.GetVodDetail(strings.Split(ids, ","), sourceId)
			if err != nil {
				c.JSON(200, gin.H{"code": 0, "msg": err.Error()})
				return
			}
			writeProvide(1, 1, len(vodList), prefixDetailPics(vodList, baseURL), false)
			return
		}
		page, pagecount, total, vodListSimple, err := service.ProvideSvc.GetVodList(sourceId, t, cid, pg, wd, h_param, year, area, lang, plot, sort, limit)
		if err != nil {
			c.JSON(200, gin.H{"code": 0, "msg": err.Error()})
			return
		}
		idList := make([]string, 0, len(vodListSimple))
		for _, v := range vodListSimple {
			idList = append(idList, strconv.FormatInt(v.VodID, 10))
		}
		detailList, err := service.ProvideSvc.GetVodDetail(idList, sourceId)
		if err != nil {
			c.JSON(200, gin.H{"code": 0, "msg": err.Error()})
			return
		}
		writeProvide(page, pagecount, total, prefixDetailPics(detailList, baseURL), true)
	default:
		page, pagecount, total, vodList, err := service.ProvideSvc.GetVodList(sourceId, t, cid, pg, wd, h_param, year, area, lang, plot, sort, limit)
		if err != nil {
			c.JSON(200, gin.H{"code": 0, "msg": err.Error()})
			return
		}
		writeProvide(page, pagecount, total, prefixListPics(vodList, baseURL), true)
	}
}

func prefixListPics(list []model.FilmList, baseURL string) []model.FilmList {
	if list == nil {
		return []model.FilmList{}
	}
	if baseURL == "" || len(list) == 0 {
		return list
	}
	cloned := append([]model.FilmList(nil), list...)
	for i := range cloned {
		cloned[i].VodPic = normalizeMediaURL(cloned[i].VodPic, baseURL)
	}
	return cloned
}

func prefixDetailPics(list []model.FilmDetail, baseURL string) []model.FilmDetail {
	if list == nil {
		return []model.FilmDetail{}
	}
	if baseURL == "" || len(list) == 0 {
		return list
	}
	cloned := append([]model.FilmDetail(nil), list...)
	for i := range cloned {
		cloned[i].VodPic = normalizeMediaURL(cloned[i].VodPic, baseURL)
	}
	return cloned
}

// HandleProvideConfig 提供给 TVBox/影视仓 的一键网络配置 (config.json)。
// 对应端点：GET /api/provide/tvbox
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
