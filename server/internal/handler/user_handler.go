package handler

import (
	"net/http"
	"strconv"
	"strings"

	"server/internal/config"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

type UserHandler struct{}

var UserHd = new(UserHandler)

func useSecureCookie(c *gin.Context) bool {
	return c.Request.TLS != nil || c.GetHeader("X-Forwarded-Proto") == "https"
}

func setAuthCookie(c *gin.Context, token string) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(config.AuthCookieName, token, config.AuthTokenExpires*3600, "/", "", useSecureCookie(c), true)
}

func clearAuthCookie(c *gin.Context) {
	c.SetSameSite(http.SameSiteLaxMode)
	c.SetCookie(config.AuthCookieName, "", -1, "/", "", useSecureCookie(c), true)
}

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
	setAuthCookie(c, token)
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
	service.UserSvc.UserLogout(uc.UserID)
	clearAuthCookie(c)
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
	info := service.UserSvc.GetUserInfo(uc.UserID)
	dto.Success(info, "成功获取用户信息", c)
}

// UserListPage 用户列表分页
func (h *UserHandler) UserListPage(c *gin.Context) {
	paging := dto.GetPageParams(c)
	userName := c.DefaultQuery("userName", "")

	list := service.UserSvc.GetUserPage(paging, userName)
	dto.Success(gin.H{
		"list":  list,
		"total": paging.Total,
	}, "用户列表获取成功", c)
}

// UserAdd 添加用户
func (h *UserHandler) UserAdd(c *gin.Context) {
	var u model.User
	if err := c.ShouldBindJSON(&u); err != nil {
		dto.Failed("参数校验失败!!!", c)
		return
	}
	if u.UserName == "" || u.Password == "" {
		dto.Failed("用户名和密码必填!!!", c)
		return
	}
	if err := service.UserSvc.AddUser(u); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("用户添加成功", c)
}

// UserUpdate 更新用户
func (h *UserHandler) UserUpdate(c *gin.Context) {
	var u model.User
	if err := c.ShouldBindJSON(&u); err != nil {
		dto.Failed("参数校验失败!!!", c)
		return
	}
	if u.ID == 0 {
		dto.Failed("用户ID缺失!!!", c)
		return
	}
	// 非超级管理员不可修改超级管理员信息
	if u.ID == config.UserIdInitialVal {
		v, ok := c.Get(config.AuthUserClaims)
		if !ok {
			dto.Failed("鉴权失败，请重新登录", c)
			return
		}
		uc, _ := v.(*utils.UserClaims)
		if uc.UserID != config.UserIdInitialVal {
			dto.Failed("权限不足，仅超级管理员可修改超级管理员信息", c)
			return
		}
	}
	if err := service.UserSvc.UpdateUser(u); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("用户信息更新成功", c)
}

// UserDelete 删除用户
func (h *UserHandler) UserDelete(c *gin.Context) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.Failed("鉴权失败，请重新登录", c)
		return
	}
	uc, _ := v.(*utils.UserClaims)
	if uc.UserID != config.UserIdInitialVal {
		dto.Failed("权限不足，仅超级管理员可删除用户", c)
		return
	}

	var req struct {
		Id string `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}
	idStr := strings.TrimSpace(req.Id)
	if idStr == "" {
		dto.Failed("用户ID缺失!!!", c)
		return
	}
	id, _ := strconv.Atoi(idStr)
	if uint(id) == config.UserIdInitialVal {
		dto.Failed("默认超级管理员账号不允许删除!!!", c)
		return
	}
	if err := service.UserSvc.DeleteUser(uint(id)); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("用户删除成功", c)
}
