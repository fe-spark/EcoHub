package handler

import (
	"fmt"
	"strconv"

	"server/internal/infra/syslog"
	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
)

type SystemLogHandler struct{}

var SystemLogHd = new(SystemLogHandler)

func (h *SystemLogHandler) Delta(c *gin.Context) {
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
