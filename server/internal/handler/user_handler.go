package handler

import (
	"encoding/json"
	"strconv"

	"server/internal/config"
	"server/internal/middleware"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

type UserHandler struct{}

var UserHd = new(UserHandler)

// Login 管理员登录接口
func (h *UserHandler) Login(c *gin.Context) {
	var u model.User
	if err := c.ShouldBindJSON(&u); err != nil {
		dto.Failed("登录信息异常!!!", c)
		return
	}
	if len(u.UserName) <= 0 || len(u.Password) <= 0 {
		dto.Failed("用户名和密码信息不能为空", c)
		return
	}
	token, err := service.UserSvc.UserLogin(u.UserName, u.Password)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	middleware.SetAuthCookie(c, token)
	dto.SuccessOnlyMsg("登录成功!!!", c)
}

// Logout 退出登录
func (h *UserHandler) Logout(c *gin.Context) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.Failed("请求失败,登录信息获取异常!!!", c)
		return
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok {
		dto.Failed("注销失败, 身份信息格式化异常!!!", c)
		return
	}
	if err := service.UserSvc.UserLogout(uc.UserID); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	middleware.ClearAuthCookie(c)
	dto.SuccessOnlyMsg("已退出登录!!!", c)
}

// UserInfo 获取用户信息
func (h *UserHandler) UserInfo(c *gin.Context) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.Failed("用户信息获取失败, 未获取到用户授权信息", c)
		return
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok {
		dto.Failed("用户信息获取失败, 户授权信息异常", c)
		return
	}
	info, err := service.UserSvc.GetUserInfo(uc.UserID)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(info, "成功获取用户信息", c)
}

// UserListPage 用户列表分页
func (h *UserHandler) UserListPage(c *gin.Context) {
	paging := dto.GetPageParams(c)
	userName := c.DefaultQuery("userName", "")
	role, _ := strconv.Atoi(c.DefaultQuery("role", "-1"))
	status, _ := strconv.Atoi(c.DefaultQuery("status", "-1"))

	list, err := service.UserSvc.GetUserPage(paging, userName, role, status)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(gin.H{
		"list":  list,
		"total": paging.Total,
	}, "用户列表获取成功", c)
}

// UserSave 保存用户（新增或更新）
func (h *UserHandler) UserSave(c *gin.Context) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.Failed("鉴权失败，请重新登录", c)
		return
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok || uc == nil {
		dto.Failed("鉴权失败，请重新登录", c)
		return
	}

	bodyBytes, err := c.GetRawData()
	if err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	var raw map[string]any
	if err := json.Unmarshal(bodyBytes, &raw); err != nil {
		dto.Failed("参数校验失败!!!", c)
		return
	}

	var userID uint
	if idVal, exists := raw["id"]; exists {
		switch v := idVal.(type) {
		case float64:
			userID = uint(v)
		case int:
			userID = uint(v)
		}
	}

	if userID > 0 {
		var req model.UserUpdatePayload
		if err := json.Unmarshal(bodyBytes, &req); err != nil {
			dto.Failed("参数校验失败!!!", c)
			return
		}
		req.ID = userID
		if err := service.UserSvc.UpdateUser(uc.UserID, uc.Role, req); err != nil {
			dto.Failed(err.Error(), c)
			return
		}
		dto.SuccessOnlyMsg("用户信息更新成功", c)
		return
	}

	var u model.User
	if err := json.Unmarshal(bodyBytes, &u); err != nil {
		dto.Failed("参数校验失败!!!", c)
		return
	}
	if u.UserName == "" || u.Password == "" {
		dto.Failed("用户名和密码必填!!!", c)
		return
	}
	operatorIsAdmin := model.IsAdmin(uc.UserID, uc.Role)
	u.Role, u.Status = model.ClampNewUser(operatorIsAdmin, u.Role, u.Status)

	if err := service.UserSvc.AddUser(u); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("用户添加成功", c)
}

// UserDelete 删除用户（统一批量/单用户删除，接收整型数组 ids: []uint）
func (h *UserHandler) UserDelete(c *gin.Context) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.Failed("鉴权失败，请重新登录", c)
		return
	}
	uc, ok := v.(*utils.UserClaims)
	if !ok || uc == nil {
		dto.Failed("鉴权失败，请重新登录", c)
		return
	}

	var req struct {
		Ids []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	cleanIds := make([]uint, 0, len(req.Ids))
	for _, id := range req.Ids {
		if id > 0 {
			cleanIds = append(cleanIds, id)
		}
	}
	if len(cleanIds) == 0 {
		dto.Failed("用户ID缺失!!!", c)
		return
	}
	for _, id := range cleanIds {
		if err := service.UserSvc.DeleteUser(uc.UserID, uc.Role, id); err != nil {
			dto.Failed(err.Error(), c)
			return
		}
	}
	dto.SuccessOnlyMsg("用户删除成功", c)
}
