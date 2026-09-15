package handler

import (
	"strconv"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/spider"

	"github.com/gin-gonic/gin"
)

// WebdavReport 查询 WebDAV 扫描报告与明细项
func (h *CollectHandler) WebdavReport(c *gin.Context) {
	sourceId := strings.TrimSpace(c.Query("sourceId"))
	if sourceId == "" {
		dto.Failed("资源站标识不能为空", c)
		return
	}

	source := service.CollectSvc.GetFilmSource(sourceId)
	if source == nil || source.SourceType != model.SourceTypeWebdav {
		dto.Failed("未找到对应的 WebDAV 资源站", c)
		return
	}

	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	if page < 1 {
		page = 1
	}
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", "20"))
	if pageSize < 1 || pageSize > 100 {
		pageSize = 20
	}
	status := strings.TrimSpace(c.Query("status"))

	// 查询最新扫描报告
	var report model.WebdavScanReport
	_ = db.Mdb.Where("source_id = ?", sourceId).Order("id DESC").First(&report).Error

	// 分页查询明细项
	query := db.Mdb.Model(&model.WebdavScanItem{}).Where("source_id = ?", sourceId)
	if status != "" && status != "all" {
		query = query.Where("status = ?", status)
	}

	var total int64
	query.Count(&total)

	var items []model.WebdavScanItem
	offset := (page - 1) * pageSize
	query.Order("id DESC").Offset(offset).Limit(pageSize).Find(&items)

	var reportOut any
	if report.ID > 0 {
		reportOut = report
	}
	dto.Success(gin.H{
		"report":   reportOut,
		"progress": spider.SnapshotCollectProgress(sourceId),
		"items":    items,
		"total":    total,
		"page":     page,
		"pageSize": pageSize,
	}, "获取扫描报告成功", c)
}

// WebdavBind 手动将待处理文件绑定到 TMDB 条目
func (h *CollectHandler) WebdavBind(c *gin.Context) {
	var req model.WebdavBindRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数解析失败: "+err.Error(), c)
		return
	}

	if req.SourceId == "" || len(req.ItemIds) == 0 || req.TmdbId <= 0 {
		dto.Failed("请求参数不完整，请提供 sourceId、itemIds 和 tmdbId", c)
		return
	}
	req.MediaType = strings.ToLower(strings.TrimSpace(req.MediaType))
	if req.MediaType != "movie" && req.MediaType != "tv" {
		dto.Failed("媒体类型无效，必须为 movie 或 tv", c)
		return
	}

	group, affectedMids, err := spider.BindWebDAVItems(req)
	if err != nil {
		dto.Failed("绑定失败: "+err.Error(), c)
		return
	}

	dto.Success(gin.H{
		"group":        group,
		"affectedMids": affectedMids,
	}, "绑定成功", c)
}

// WebdavRescrape 重新刮削指定文件或未匹配项
func (h *CollectHandler) WebdavRescrape(c *gin.Context) {
	var req model.WebdavRescrapeRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数解析失败: "+err.Error(), c)
		return
	}

	if req.SourceId == "" {
		dto.Failed("资源站标识不能为空", c)
		return
	}

	count, err := spider.RescrapeWebDAV(req)
	if err != nil {
		dto.Failed("重新刮削失败: "+err.Error(), c)
		return
	}

	dto.Success(gin.H{
		"successCount": count,
	}, "重新刮削完成", c)
}
