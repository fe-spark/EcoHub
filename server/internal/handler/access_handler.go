package handler

import (
	"net/http"
	"strconv"
	"strings"

	"server/internal/access"
	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

const trackViewMaxBody = 4 << 10

type AccessHandler struct{}

var AccessHd = new(AccessHandler)

func (h *AccessHandler) isAccessible() bool {
	if config.AccessLogEnabled {
		return true
	}
	hasData, _ := access.HasPersistedData()
	return hasData
}

// Status 查询数据分析功能开启状态与落库数据状态
func (h *AccessHandler) Status(c *gin.Context) {
	hasData, totalRows := access.HasPersistedData()
	dto.Success(gin.H{
		"enabled":   config.AccessLogEnabled,
		"hasData":   hasData,
		"totalRows": totalRows,
	}, "数据分析状态获取成功", c)
}

func (h *AccessHandler) Overview(c *gin.Context) {
	if !h.isAccessible() {
		dto.Failed("数据分析功能未开启", c)
		return
	}
	data, err := access.QueryOverviewScope(c.Query("day"), c.Query("module"), c.Query("platform"))
	if err != nil {
		dto.Failed("数据分析暂不可用", c)
		return
	}
	dto.Success(data, "数据分析概览获取成功", c)
}

func (h *AccessHandler) Tops(c *gin.Context) {
	if !h.isAccessible() {
		dto.Failed("数据分析功能未开启", c)
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10"))
	kind := c.DefaultQuery("kind", "path")
	items, err := access.QueryTopsScope(c.Query("day"), kind, c.Query("module"), c.Query("platform"), limit)
	if err != nil {
		dto.Failed("数据分析暂不可用", c)
		return
	}
	dto.Success(gin.H{"kind": kind, "items": items}, "数据分析榜单获取成功", c)
}

func (h *AccessHandler) TrackView(c *gin.Context) {
	if !config.AccessLogEnabled {
		dto.SuccessOnlyMsg("ok", c)
		return
	}
	if c.Request.Body != nil {
		c.Request.Body = http.MaxBytesReader(nil, c.Request.Body, trackViewMaxBody)
	}
	var body access.TrackViewPayload
	if err := c.ShouldBindJSON(&body); err != nil {
		dto.SuccessOnlyMsg("ok", c)
		return
	}
	access.TrackPagePayload(c, body)
	dto.SuccessOnlyMsg("ok", c)
}

func (h *AccessHandler) Logs(c *gin.Context) {
	if !h.isAccessible() {
		dto.Failed("数据分析功能未开启", c)
		return
	}
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "0"))
	list, err := access.QueryLogsScope(
		c.Query("day"),
		c.DefaultQuery("source", "recent"),
		c.Query("status"),
		c.Query("client"),
		c.Query("q"),
		c.Query("module"),
		c.Query("platform"),
		limit,
	)
	if err != nil {
		dto.Failed("数据分析暂不可用", c)
		return
	}
	dto.Success(gin.H{"list": list}, "数据流转日志获取成功", c)
}

// DataStats 查询数据分析积累数据体量统计（仅超级管理员）
func (h *AccessHandler) DataStats(c *gin.Context) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
		return
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok || uc == nil || !model.IsAdmin(uc.UserID, uc.Role) {
		dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, "权限不足，仅超级管理员可查看数据分析统计", c)
		return
	}
	stats := access.GetAccessDataStats()
	dto.Success(stats, "数据分析统计获取成功", c)
}

// CleanAccessDataRequest 清理数据分析数据请求
type CleanAccessDataRequest struct {
	Password      string `json:"password"`
	RetentionDays int    `json:"retentionDays"` // 0 为全部清理，> 0 为保留最近 N 天
}

// CleanData 清理数据分析积累的数据（仅超级管理员，需校验管理密码）
func (h *AccessHandler) CleanData(c *gin.Context) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
		return
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok || uc == nil || !model.IsAdmin(uc.UserID, uc.Role) {
		dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, "权限不足，仅超级管理员可清理数据分析数据", c)
		return
	}

	var req CleanAccessDataRequest
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
	if !verifyManagePassword(c, req.Password) {
		dto.Failed("清理失败, 密钥校验失败!!!", c)
		return
	}

	res, err := access.ClearAccessData(req.RetentionDays)
	if err != nil {
		syslog.Errorf("[Access] 清理数据分析数据失败: %v", err)
		dto.Failed("清理数据分析数据失败: "+err.Error(), c)
		return
	}
	syslog.Infof("[Access] 超管 user=%d 清理数据分析数据完成: stats=%d, tops=%d, redisKeys=%d, retentionDays=%d",
		uc.UserID, res.DeletedDailyStats, res.DeletedDailyTop, res.DeletedRedisKeys, req.RetentionDays)
	dto.Success(res, "数据分析数据清理成功", c)
}

