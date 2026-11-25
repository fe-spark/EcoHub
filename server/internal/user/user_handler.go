package user

import (
	"encoding/json"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/httpx"
	"server/internal/model"
	"server/internal/model/dto"

	"github.com/gin-gonic/gin"
)

type UserHandler struct{}

var UserHd = new(UserHandler)

type loginAttemptState struct {
	fails     int
	lockUntil time.Time
	lastSeen  time.Time
}

var (
	loginFailMu    sync.Mutex
	loginFailState = map[string]loginAttemptState{}
)

func loginLimiterKey(c *gin.Context, username string) string {
	return c.ClientIP() + "|" + strings.ToLower(strings.TrimSpace(username))
}

func loginAccountLimiterKey(username string) string {
	return "acct|" + strings.ToLower(strings.TrimSpace(username))
}

// reserveLoginAttempts 在同一把锁里检查并占一次尝试。并发请求不能都看到「还没锁定」。
func reserveLoginAttempts(keys ...string) bool {
	now := time.Now()
	loginFailMu.Lock()
	defer loginFailMu.Unlock()
	for _, key := range keys {
		st, ok := loginFailState[key]
		if !ok {
			continue
		}
		if now.Before(st.lockUntil) {
			return false
		}
		if now.Sub(st.lastSeen) > 10*time.Minute {
			delete(loginFailState, key)
		}
	}
	if len(loginFailState) > 4096 {
		cutoff := now.Add(-10 * time.Minute)
		for k, st := range loginFailState {
			if st.lastSeen.Before(cutoff) {
				delete(loginFailState, k)
			}
		}
	}
	for _, key := range keys {
		st := loginFailState[key]
		st.fails++
		st.lastSeen = now
		if st.fails >= 5 {
			st.lockUntil = now.Add(30 * time.Second)
		}
		loginFailState[key] = st
	}
	return true
}

func clearLoginAttempts(keys ...string) {
	loginFailMu.Lock()
	defer loginFailMu.Unlock()
	for _, key := range keys {
		delete(loginFailState, key)
	}
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
	limitKey := loginLimiterKey(c, u.UserName)
	acctKey := loginAccountLimiterKey(u.UserName)
	if !reserveLoginAttempts(limitKey, acctKey) {
		dto.Failed("登录尝试过于频繁，请 30 秒后再试", c)
		return
	}
	token, err := UserSvc.UserLogin(u.UserName, u.Password)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	clearLoginAttempts(limitKey, acctKey)
	httpx.SetAuthCookie(c, token)
	dto.SuccessOnlyMsg("登录成功!!!", c)
}

// Logout 退出登录
func (h *UserHandler) Logout(c *gin.Context) {
	uc, ok := httpx.CurrentClaims(c)
	if !ok {
		dto.Failed("注销失败, 身份信息格式化异常!!!", c)
		return
	}
	if err := UserSvc.UserLogout(uc.UserID); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	httpx.ClearAuthCookie(c)
	dto.SuccessOnlyMsg("已退出登录!!!", c)
}

// UserInfo 获取用户信息
func (h *UserHandler) UserInfo(c *gin.Context) {
	uc, ok := httpx.CurrentClaims(c)
	if !ok {
		dto.Failed("用户信息获取失败, 未获取到用户授权信息", c)
		return
	}
	info, err := UserSvc.GetUserInfo(uc.UserID)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(info, "成功获取用户信息", c)
}

// UserProfile 当前登录用户修改个人资料（支持修改密码、昵称、头像、性别、邮箱）
func (h *UserHandler) UserProfile(c *gin.Context) {
	uc, ok := httpx.CurrentClaims(c)
	if !ok {
		dto.Failed("鉴权失败，请重新登录", c)
		return
	}
	var req model.UserUpdatePayload
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("参数校验失败!!!", c)
		return
	}
	if err := UserSvc.UpdateProfile(uc.UserID, req); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("个人信息更新成功", c)
}

// UserListPage 用户列表分页
func (h *UserHandler) UserListPage(c *gin.Context) {
	paging := dto.GetPageParams(c)
	userName := c.DefaultQuery("userName", "")
	role, _ := strconv.Atoi(c.DefaultQuery("role", "-1"))
	status, _ := strconv.Atoi(c.DefaultQuery("status", "-1"))

	list, err := UserSvc.GetUserPage(paging, userName, role, status)
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
	uc, ok := httpx.CurrentClaims(c)
	if !ok {
		dto.Failed("鉴权失败，请重新登录", c)
		return
	}

	bodyBytes, err := c.GetRawData()
	if err != nil {
		dto.Failed("请求参数异常!!!", c)
		return
	}

	var req model.UserUpdatePayload
	if err := json.Unmarshal(bodyBytes, &req); err == nil && req.ID > 0 {
		if err := UserSvc.UpdateUser(uc.UserID, uc.Role, req); err != nil {
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

	if err := UserSvc.AddUser(u); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("用户添加成功", c)
}

// UserDelete 删除用户（统一批量/单用户删除，接收整型数组 ids: []uint）
func (h *UserHandler) UserDelete(c *gin.Context) {
	uc, ok := httpx.CurrentClaims(c)
	if !ok {
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
		if err := UserSvc.DeleteUser(uc.UserID, uc.Role, id); err != nil {
			dto.Failed(err.Error(), c)
			return
		}
	}
	dto.SuccessOnlyMsg("用户删除成功", c)
}
