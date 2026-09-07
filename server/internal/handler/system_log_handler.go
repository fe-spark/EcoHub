package handler

import (
	"fmt"
	"net/http"
	"strconv"

	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

type SystemLogHandler struct{}

var SystemLogHd = new(SystemLogHandler)

func (h *SystemLogHandler) Delta(c *gin.Context) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
		return
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok || uc == nil || !model.IsAdmin(uc.UserID, uc.Role) {
		dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, "权限不足，仅超级管理员可查看系统日志", c)
		return
	}

	after, _ := strconv.ParseInt(c.DefaultQuery("after", "0"), 10, 64)
	limit, _ := strconv.Atoi(c.DefaultQuery("limit", "10000"))
	if after <= 0 {
		lines, _ := strconv.Atoi(c.DefaultQuery("lines", "500"))
		entries, nextSeq, err := syslog.RecentEntries(lines)
		if err != nil {
			dto.Failed(fmt.Sprintf("系统日志读取失败: %v", err), c)
			return
		}
		dto.Success(gin.H{"entries": entries, "nextSeq": nextSeq, "expired": false}, "系统日志增量获取成功", c)
		return
	}

	result := syslog.DeltaAfter(after, limit)
	dto.Success(gin.H{
		"entries": result.Entries,
		"nextSeq": result.NextSeq,
		"minSeq":  result.MinSeq,
		"expired": result.Expired,
	}, "系统日志增量获取成功", c)
}
