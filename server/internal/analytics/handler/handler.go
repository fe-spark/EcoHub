package handler

import (
	"net/http"
	"strconv"
	"strings"

	"server/internal/analytics/config"
	"server/internal/analytics/ingest"
	"server/internal/analytics/query"
	"server/internal/analytics/rollup"
	"server/internal/analytics/store"
	"server/internal/analytics/types"
	"server/internal/httpx"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/user"

	"github.com/gin-gonic/gin"
)

const trackViewMaxBody = 4 << 10

// AnalyticsHandler 提供公开埋点上报与超级管理员分析看板 HTTP 接口
type AnalyticsHandler struct{}

var Handler = new(AnalyticsHandler)

// TrackView 公开页面埋点上报接口
func (h *AnalyticsHandler) TrackView(c *gin.Context) {
	if !config.IsEnabled() {
		dto.SuccessOnlyMsg("ok", c)
		return
	}
	if c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(nil, c.Request.Body, trackViewMaxBody)
	}
	var body types.TrackViewPayload
	if err := c.ShouldBindJSON(&body); err != nil {
		dto.SuccessOnlyMsg("ok", c)
		return
	}
	ingest.TrackPagePayload(c, body)
	dto.SuccessOnlyMsg("ok", c)
}

// Status 查询数据分析开关状态与历史落库统计
func (h *AnalyticsHandler) Status(c *gin.Context) {
	hasData, totalRows, err := rollup.HasPersistedData()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(types.StatusInfo{
		Enabled:   config.IsEnabled(),
		HasData:   hasData,
		TotalRows: totalRows,
	}, "数据分析状态获取成功", c)
}

// GetConfig 读取数据分析配置
func (h *AnalyticsHandler) GetConfig(c *gin.Context) {
	cfg, err := config.GetAndSync()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(cfg, "数据分析配置获取成功", c)
}

// UpdateConfigRequest 更新数据分析开关请求体
type UpdateConfigRequest struct {
	Enabled bool `json:"enabled"`
}

// UpdateConfig 更新数据分析配置并热生效
func (h *AnalyticsHandler) UpdateConfig(c *gin.Context) {
	var req UpdateConfigRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if err := config.UpdateAndSync(model.AnalyticsConfig{Enabled: req.Enabled}); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(model.AnalyticsConfig{Enabled: req.Enabled}, "数据分析配置更新成功", c)
}

// Dashboard 聚合返回看板所需的全量与分端指标、片源分布、榜单、首屏流水与可用日期
func (h *AnalyticsHandler) Dashboard(c *gin.Context) {
	payload, err := query.QueryDashboard(
		c.Query("day"),
		c.Query("module"),
		c.Query("platform"),
		c.Query("action"),
		c.Query("q"),
	)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(payload, "数据分析看板获取成功", c)
}

// Overview 查询单日概览数据
func (h *AnalyticsHandler) Overview(c *gin.Context) {
	data, err := query.QueryOverviewScope(c.Query("day"), c.Query("module"), c.Query("platform"))
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(data, "数据分析概览获取成功", c)
}

// Sources 查询单日采集站片源调用分布
func (h *AnalyticsHandler) Sources(c *gin.Context) {
	list, err := query.QuerySourceCalls(c.Query("day"))
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(gin.H{"list": list}, "采集站调用次数获取成功", c)
}

// Tops 查询分页与搜索榜单
func (h *AnalyticsHandler) Tops(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", c.DefaultQuery("limit", "20")))
	kind := c.DefaultQuery("kind", "play")
	res, err := query.QueryTopsPage(
		c.Query("day"),
		kind,
		c.Query("module"),
		c.Query("platform"),
		c.Query("q"),
		page,
		pageSize,
	)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(res, "数据分析榜单获取成功", c)
}

// Logs 查询分端访问流水（支持行为/关键词筛选与分页）
func (h *AnalyticsHandler) Logs(c *gin.Context) {
	page, _ := strconv.Atoi(c.DefaultQuery("page", "1"))
	pageSize, _ := strconv.Atoi(c.DefaultQuery("pageSize", c.DefaultQuery("limit", "20")))
	res, err := query.QueryLogsPage(query.LogsQuery{
		Day:      c.Query("day"),
		Module:   c.Query("module"),
		Platform: c.Query("platform"),
		Action:   c.Query("action"),
		Q:        c.Query("q"),
		Page:     page,
		PageSize: pageSize,
	})
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(res, "数据流转日志获取成功", c)
}

// DataStats 查询数据分析数据体量统计
func (h *AnalyticsHandler) DataStats(c *gin.Context) {
	if _, ok := httpx.RequireAdminClaims(c, "权限不足，仅超级管理员可查看数据分析统计"); !ok {
		return
	}
	stats, err := store.GetDataStats()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(stats, "数据分析统计获取成功", c)
}

// CleanRequest 清理数据分析请求体
type CleanRequest struct {
	Password      string `json:"password"`
	RetentionDays int    `json:"retentionDays"`
}

// CleanData 清理数据分析积累的数据（校验管理密码）
func (h *AnalyticsHandler) CleanData(c *gin.Context) {
	uc, ok := httpx.RequireAdminClaims(c, "权限不足，仅超级管理员可清理数据分析数据")
	if !ok {
		return
	}
	var req CleanRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if req.RetentionDays < 0 {
		dto.Failed("保留天数参数异常", c)
		return
	}
	if strings.TrimSpace(req.Password) == "" {
		dto.Failed("清理失败, 密钥校验失败!!!", c)
		return
	}
	if !httpx.VerifyCurrentPassword(c, req.Password, user.UserSvc.VerifyUserPassword) {
		dto.Failed("清理失败, 密钥校验失败!!!", c)
		return
	}
	res, err := store.ClearData(req.RetentionDays, rollup.Mutex(), query.ClearOverviewCache)
	if err != nil {
		syslog.Errorf("[Analytics] 清理数据分析数据失败: %v", err)
		dto.Failed("清理数据分析数据失败: "+err.Error(), c)
		return
	}
	syslog.Infof("[Analytics] 超管 user=%d 清理数据分析数据完成: stats=%d, tops=%d, redisKeys=%d, retentionDays=%d",
		uc.UserID, res.DeletedDailyStats, res.DeletedDailyTop, res.DeletedRedisKeys, req.RetentionDays)
	dto.Success(res, "数据分析数据清理成功", c)
}
