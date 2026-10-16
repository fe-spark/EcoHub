package handler

import (
	"fmt"
	"strings"

	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/spider"

	"github.com/gin-gonic/gin"
)

// FilmSourceSyncCategory 重新获取单站分类，支持变更确认与数据清空
func (h *CollectHandler) FilmSourceSyncCategory(c *gin.Context) {
	var req struct {
		Id      string `json:"id"`
		Confirm string `json:"confirm"`
	}
	if err := c.ShouldBindJSON(&req); err != nil || strings.TrimSpace(req.Id) == "" {
		dto.Failed("参数异常, 资源站标识不能为空", c)
		return
	}
	req.Id = strings.TrimSpace(req.Id)
	req.Confirm = strings.TrimSpace(req.Confirm)

	fs := service.CollectSvc.GetFilmSource(req.Id)
	if fs == nil {
		dto.Failed("数据异常, 资源站信息不存在", c)
		return
	}
	if spider.IsTaskRunning(req.Id) {
		dto.Failed("站点正在采集, 请先停止采集后再尝试操作", c)
		return
	}

	result, err := service.SpiderSvc.SyncSourceCategories(req.Id, req.Confirm)
	if err != nil {
		dto.Failed(fmt.Sprintf("获取分类失败: %s", err.Error()), c)
		return
	}

	if result.NeedConfirm {
		dto.Success(result, "分类树已变化", c)
		return
	}

	msg := result.Message
	if msg == "" {
		msg = "分类获取成功"
	}
	dto.SuccessOnlyMsg(msg, c)
}
