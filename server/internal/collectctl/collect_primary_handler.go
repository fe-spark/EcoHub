package collectctl

import (
	"github.com/gin-gonic/gin"
	"net/http"
	"server/internal/httpx"
	"server/internal/model"
	"server/internal/model/dto"
)

func collectAdminStatus(c *gin.Context) (isAdmin bool, hasClaims bool) {
	uc, ok := httpx.CurrentClaims(c)
	if !ok {
		return false, false
	}
	return model.IsAdmin(uc.UserID, uc.Role), true
}

func respondPrimarySwitchAuthError(c *gin.Context, hasClaims bool) {
	if !hasClaims {
		dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
		return
	}
	dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, ErrPrimarySwitchForbidden.Error(), c)
}

// PrimaryState GET /manage/primary 当前主站。无主站时 data 为 null；主站删除中附带 deletingSource。
func (h *CollectHandler) PrimaryState(c *gin.Context) {
	primary := CollectSvc.CurrentPrimary()
	if primary != nil {
		dto.Success(gin.H{"id": primary.Id, "name": primary.Name, "state": primary.State}, "获取主站成功", c)
		return
	}
	if deleting := CollectSvc.DeletingPrimary(); deleting != nil {
		dto.Success(gin.H{
			"deletingSource": gin.H{"id": deleting.Id, "name": deleting.Name},
		}, "主站删除中", c)
		return
	}
	dto.Success(nil, "暂无主站", c)
}

// SwitchPrimary PUT /manage/primary 切换主站（仅超管，D4）。
func (h *CollectHandler) SwitchPrimary(c *gin.Context) {
	var req struct {
		Id string `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if err := CollectSvc.SwitchPrimary(req.Id); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	primary := CollectSvc.CurrentPrimary()
	if primary == nil {
		dto.Success(nil, "主站已置空", c)
		return
	}
	dto.Success(gin.H{"id": primary.Id, "name": primary.Name, "state": primary.State}, "主站已切换", c)
}
