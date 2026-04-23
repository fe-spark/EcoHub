package handler

import (
	"fmt"

	"server/internal/config"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

type SpiderHandler struct{}

var SpiderHd = new(SpiderHandler)

// StarSpider 开启并执行采集任务
func (h *SpiderHandler) StarSpider(c *gin.Context) {
	var cp model.CollectParams
	if err := c.ShouldBindJSON(&cp); err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	if cp.Time == 0 {
		dto.Failed("采集开启失败,采集时长不能为0", c)
		return
	}

	if cp.Batch {
		if len(cp.Ids) <= 0 {
			dto.Failed("批量采集开启失败, 关联的资源站信息为空", c)
			return
		}
		if err := service.SpiderSvc.BatchCollect(cp.Time, cp.Ids); err != nil {
			dto.Failed(err.Error(), c)
			return
		}
	} else {
		if len(cp.Id) <= 0 {
			dto.Failed("批量采集开启失败, 资源站Id获取失败", c)
			return
		}
		if err := service.SpiderSvc.StartCollect(cp.Id, cp.Time); err != nil {
			dto.Failed(fmt.Sprint("采集任务开启失败: ", err.Error()), c)
			return
		}
	}
	dto.SuccessOnlyMsg("采集任务已成功开启!!!", c)
}

// ClearAllFilm 删除所有film信息
func (h *SpiderHandler) ClearAllFilm(c *gin.Context) {
	var req struct {
		Password string `json:"password"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	pwd := req.Password
	if !verifyPassword(c, pwd) {
		dto.Failed("重置失败, 密钥校验失败!!!", c)
		return
	}
	service.SpiderSvc.ClearFilms()
	dto.SuccessOnlyMsg("影视数据已删除!!!", c)
}

// CoverFilmClass 重置覆盖影片分类信息
func (h *SpiderHandler) CoverFilmClass(c *gin.Context) {
	if err := service.SpiderSvc.FilmClassCollect(); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("影视分类信息重置成功, 请稍等片刻后刷新页面", c)
}

// SingleUpdateSpider 单一影片主站更新采集
func (h *SpiderHandler) SingleUpdateSpider(c *gin.Context) {
	var req struct {
		Ids string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	ids := req.Ids
	if ids == "" {
		dto.Failed("参数异常, 资源标识ID信息缺失", c)
		return
	}
	service.SpiderSvc.SyncCollect(ids)
	dto.SuccessOnlyMsg("主站影片更新任务已成功开启!!!", c)
}

// StopAllTasks 一键终止所有采集中任务
func (h *SpiderHandler) StopAllTasks(c *gin.Context) {
	service.SpiderSvc.StopAllTasks()
	dto.SuccessOnlyMsg("已发送终止指令，所有采集任务正在停止", c)
}

func verifyPassword(c *gin.Context, password string) bool {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.Failed("操作失败,登录信息异常!!!", c)
		return false
	}
	uc := v.(*utils.UserClaims)
	return service.UserSvc.VerifyUserPassword(uc.UserID, password)
}
