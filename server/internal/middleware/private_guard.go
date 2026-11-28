package middleware

import (
	"crypto/subtle"
	"net/http"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/httpx"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repobase"
	"server/internal/user"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

const (
	provideKeyMaxFailures = 10
	provideKeyWindow      = 5 * time.Minute
	provideKeyLockDur     = 60 * time.Second
	provideKeyMaxEntries  = 4096
	provideKeyEvictBatch  = provideKeyMaxEntries / 8
)

type provideKeyAttemptState struct {
	fails     int
	lockUntil time.Time
	lastSeen  time.Time
}

var (
	provideKeyFailMu    sync.Mutex
	provideKeyFailState = map[string]provideKeyAttemptState{}
)

func extractProvideKey(c *gin.Context) string {
	key := strings.TrimSpace(c.Query("key"))
	if key == "" {
		key = strings.TrimSpace(c.GetHeader("X-Provide-Key"))
	}
	return key
}

func rateLimitClientKey(c *gin.Context) string {
	return utils.NormalizeClientIPKey(realClientIP(c))
}

func isProvideKeyAllowed(clientIP string) bool {
	now := time.Now()
	provideKeyFailMu.Lock()
	defer provideKeyFailMu.Unlock()
	st, ok := provideKeyFailState[clientIP]
	if !ok {
		return true
	}
	if now.Before(st.lockUntil) {
		return false
	}
	if now.Sub(st.lastSeen) > provideKeyWindow {
		delete(provideKeyFailState, clientIP)
	}
	return true
}

func recordProvideKeyFailure(clientIP string) bool {
	now := time.Now()
	provideKeyFailMu.Lock()
	defer provideKeyFailMu.Unlock()
	if _, exists := provideKeyFailState[clientIP]; !exists && len(provideKeyFailState) >= provideKeyMaxEntries {
		cutoff := now.Add(-provideKeyWindow)
		for k, st := range provideKeyFailState {
			if st.lastSeen.Before(cutoff) && !now.Before(st.lockUntil) {
				delete(provideKeyFailState, k)
			}
		}
		// 窗口期内大量独立 IP 涌入时强制淘汰一批，保证 map 规模受控且不每次退化为全表扫描
		if len(provideKeyFailState) >= provideKeyMaxEntries {
			evict := provideKeyEvictBatch
			if overflow := len(provideKeyFailState) - provideKeyMaxEntries + 1; overflow > evict {
				evict = overflow
			}
			for k, st := range provideKeyFailState {
				if evict <= 0 {
					break
				}
				if now.Before(st.lockUntil) {
					continue
				}
				delete(provideKeyFailState, k)
				evict--
			}
			for k := range provideKeyFailState {
				if evict <= 0 {
					break
				}
				delete(provideKeyFailState, k)
				evict--
			}
		}
	}
	st := provideKeyFailState[clientIP]
	if !st.lastSeen.IsZero() && now.Sub(st.lastSeen) > provideKeyWindow {
		st = provideKeyAttemptState{}
	}
	st.fails++
	st.lastSeen = now
	locked := false
	if st.fails >= provideKeyMaxFailures {
		st.lockUntil = now.Add(provideKeyLockDur)
		locked = true
	}
	provideKeyFailState[clientIP] = st
	return locked
}

func clearProvideKeyFailures(clientIP string) {
	provideKeyFailMu.Lock()
	defer provideKeyFailMu.Unlock()
	delete(provideKeyFailState, clientIP)
}

func resetProvideKeyRateLimit() {
	provideKeyFailMu.Lock()
	defer provideKeyFailMu.Unlock()
	provideKeyFailState = map[string]provideKeyAttemptState{}
}

func clearAuthCookieIfPresent(c *gin.Context) {
	if cookieToken, err := c.Cookie(config.AuthCookieName); err == nil && cookieToken != "" {
		httpx.ClearAuthCookie(c)
	}
}

// checkProvideKeyWithLimit 统一执行订阅密钥的限流检查、恒定时间比对与失败计数。
func checkProvideKeyWithLimit(c *gin.Context, key, expectedKey string) (matched bool, rateLimited bool, missingExpected bool) {
	clientKey := rateLimitClientKey(c)
	if !isProvideKeyAllowed(clientKey) {
		return false, true, false
	}
	if expectedKey == "" {
		return false, false, true
	}
	if subtle.ConstantTimeCompare([]byte(key), []byte(expectedKey)) == 1 {
		clearProvideKeyFailures(clientKey)
		return true, false, false
	}
	if recordProvideKeyFailure(clientKey) {
		return false, true, false
	}
	return false, false, false
}

// HasValidPrivateRequestAuth 校验当前请求是否携带有效订阅密钥（含失败限流）或有效登录态，不写响应。
func HasValidPrivateRequestAuth(c *gin.Context, expectedKey string) bool {
	if c == nil || c.Request == nil {
		return false
	}
	expectedKey = strings.TrimSpace(expectedKey)
	if key := extractProvideKey(c); key != "" {
		matched, _, _ := checkProvideKeyWithLimit(c, key, expectedKey)
		return matched
	}
	authToken := httpx.ReadAuthToken(c)
	if authToken == "" {
		return false
	}
	uc, err := utils.ParseToken(authToken)
	if err != nil || uc == nil || uc.UserID == 0 {
		return false
	}
	tokenStatus, tokenErr := user.UserSvc.VerifyToken(uc.UserID, uc.Role, authToken)
	if tokenErr != nil || tokenStatus != user.TokenValid {
		return false
	}
	status, role, statusErr := user.UserSvc.AccountGate(uc.UserID)
	return statusErr == nil && status == 0 && model.UserAllowsMultiLogin(uc.Role) == model.UserAllowsMultiLogin(role)
}

// PrivateAccessGuard 动态私有化守卫
// 当后台开启「私有化访问」时，前台业务接口必须携带有效登录态或有效的订阅密钥（X-Provide-Key / ?key=）
func PrivateAccessGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		privateAccess, expectedKey, err := repobase.AccessGuardConfig()
		if err != nil {
			dto.Failed("站点配置读取失败", c)
			c.Abort()
			return
		}
		if !privateAccess {
			c.Next()
			return
		}
		key := extractProvideKey(c)

		// 1. 携带订阅密钥时：先校验限流与配置完整性，再使用恒定时间比对
		if key != "" {
			matched, rateLimited, missingExpected := checkProvideKeyWithLimit(c, key, expectedKey)
			if matched {
				c.Next()
				return
			}
			if rateLimited {
				dto.CustomResult(http.StatusTooManyRequests, dto.FAILED, gin.H{"private_access": true}, "订阅密钥尝试过于频繁，请稍后再试", c)
				c.Abort()
				return
			}
			if missingExpected {
				dto.CustomResult(http.StatusForbidden, dto.FAILED, gin.H{"private_access": true}, "该软件源已开启私有化访问，但管理员未设置订阅密钥，请联系管理员", c)
				c.Abort()
				return
			}
			dto.CustomResult(http.StatusForbidden, dto.FAILED, gin.H{"private_access": true}, "私有化订阅密钥错误，请核对后重试", c)
			c.Abort()
			return
		}

		// 2. 检查 Web 端用户 Token
		authToken := httpx.ReadAuthToken(c)

		if authToken != "" {
			uc, err := utils.ParseToken(authToken)
			if err == nil && uc != nil && uc.UserID > 0 {
				tokenStatus, tokenErr := user.UserSvc.VerifyToken(uc.UserID, uc.Role, authToken)
				if tokenErr != nil {
					dto.CustomResult(http.StatusServiceUnavailable, dto.FAILED, nil, "登录状态暂不可用", c)
					c.Abort()
					return
				}
				if tokenStatus == user.TokenValid {
					status, role, statusErr := user.UserSvc.AccountGate(uc.UserID)
					if statusErr != nil {
						dto.CustomResult(http.StatusServiceUnavailable, dto.FAILED, nil, "登录状态暂不可用", c)
						c.Abort()
						return
					}
					if status != 0 {
						_ = user.UserSvc.ClearUserSessions(uc.UserID)
						clearAuthCookieIfPresent(c)
						dto.CustomResult(http.StatusUnauthorized, dto.FAILED, gin.H{"private_access": true}, "账号已被禁用", c)
						c.Abort()
						return
					}
					if model.UserAllowsMultiLogin(uc.Role) != model.UserAllowsMultiLogin(role) {
						_ = user.UserSvc.UserLogout(uc.UserID, uc.Role, authToken)
						clearAuthCookieIfPresent(c)
						dto.CustomResult(http.StatusUnauthorized, dto.FAILED, gin.H{"private_access": true}, "身份验证信息已失效,请重新登录!!!", c)
						c.Abort()
						return
					}
					uc.Role = role
					c.Set(config.AuthUserClaims, uc)
					c.Set("user_id", uc.UserID)
					c.Set("role", role)
					c.Next()
					return
				}
			}
			// 仅当请求携带了登录 Cookie 且校验失效时清理残留 Cookie，避免无 Cookie 的 Header 调用多出 Set-Cookie
			clearAuthCookieIfPresent(c)
		}

		// 3. 既未提供有效 key，又无有效登录态，明确提示私有化
		dto.CustomResult(http.StatusUnauthorized, dto.FAILED, gin.H{"private_access": true}, "该站点已开启私有化访问，请登录账号或使用有效订阅密钥", c)
		c.Abort()
	}
}

// ProvideKeyGuard TVBox/影视仓/客户端专属访问密钥校验
// 当开启私有化时：
// 1. 若未配置 ProvideKey，拒绝访问
// 2. 若未带 key，提示需要提供订阅密钥
// 3. 若配置了 ProvideKey，使用恒定时间校验 ?key= 或 Header X-Provide-Key，并限制错误频次
func ProvideKeyGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		privateAccess, expectedKey, err := repobase.AccessGuardConfig()
		if err != nil {
			dto.Failed("站点配置读取失败", c)
			c.Abort()
			return
		}
		if !privateAccess {
			c.Next()
			return
		}
		if expectedKey == "" {
			dto.CustomResult(http.StatusForbidden, dto.FAILED, gin.H{"private_access": true}, "该软件源已开启私有化访问，但管理员未设置订阅密钥，请联系管理员", c)
			c.Abort()
			return
		}

		key := extractProvideKey(c)
		if key == "" {
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, gin.H{"private_access": true}, "该软件源已开启私有化访问，请输入订阅密钥", c)
			c.Abort()
			return
		}

		matched, rateLimited, _ := checkProvideKeyWithLimit(c, key, expectedKey)
		if !matched {
			if rateLimited {
				dto.CustomResult(http.StatusTooManyRequests, dto.FAILED, gin.H{"private_access": true}, "订阅密钥尝试过于频繁，请稍后再试", c)
				c.Abort()
				return
			}
			dto.CustomResult(http.StatusForbidden, dto.FAILED, gin.H{"private_access": true}, "私有化订阅密钥错误，请核对后重试", c)
			c.Abort()
			return
		}

		c.Next()
	}
}
