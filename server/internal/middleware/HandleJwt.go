package middleware

import (
	"errors"
	"fmt"
	"net/http"
	"server/internal/config"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
	"github.com/golang-jwt/jwt/v5"
	"golang.org/x/sync/singleflight"
)

var sfGroup singleflight.Group

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

// AuthToken 用户登录Token拦截
func AuthToken() gin.HandlerFunc {
	return func(c *gin.Context) {
		authToken, _ := c.Cookie(config.AuthCookieName)
		// 如果没有登录信息则直接清退
		if authToken == "" {
			clearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.SUCCESS, nil, "用户未授权,请先登录", c)
			c.Abort()
			return
		}
		// 解析token中的信息
		uc, err := utils.ParseToken(authToken)
		if err != nil && !errors.Is(err, jwt.ErrTokenExpired) {
			clearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.SUCCESS, nil, "无效的口令,请重新登录!!!", c)
			c.Abort()
			return
		}
		if uc == nil {
			clearAuthCookie(c)
			dto.CustomResult(http.StatusUnauthorized, dto.SUCCESS, nil, "无效的口令,请重新登录!!!", c)
			c.Abort()
			return
		}

		// 处理 token 刷新逻辑（针对过期但 Redis 中有效的 token）
		if err != nil && errors.Is(err, jwt.ErrTokenExpired) {
			// 使用 singleflight 防止并发刷新
			key := fmt.Sprintf("refresh:%d", uc.UserID)
			val, _, _ := sfGroup.Do(key, func() (any, error) {
				// 再次检查 Redis 中的 token
				t := repository.GetUserTokenById(uc.UserID)
				if len(t) <= 0 || t != authToken {
					return nil, errors.New("invalid or expired token in redis")
				}
				// 生成新token
				newToken, _ := utils.GenToken(uc.UserID, uc.UserName, uc.Role)
				_ = repository.SaveUserToken(newToken, uc.UserID)
				return newToken, nil
			})

			if val == nil {
				clearAuthCookie(c)
				dto.CustomResult(http.StatusUnauthorized, dto.SUCCESS, nil, "身份验证信息已失效,请重新登录!!!", c)
				c.Abort()
				return
			}
			newToken := val.(string)
			// 解析出新的 UserClaims
			uc, _ = utils.ParseToken(newToken)
			setAuthCookie(c, newToken)
		} else {
			// 未过期的情况，仅校验 Redis 一致性
			t := repository.GetUserTokenById(uc.UserID)
			if len(t) <= 0 {
				clearAuthCookie(c)
				dto.CustomResult(http.StatusUnauthorized, dto.SUCCESS, nil, "身份验证信息已失效,请重新登录!!!", c)
				c.Abort()
				return
			}
			if t != authToken {
				clearAuthCookie(c)
				dto.CustomResult(http.StatusUnauthorized, dto.SUCCESS, nil, "账号在其它设备登录,身份验证信息失效,请重新登录!!!", c)
				c.Abort()
				return
			}
		}

		// 将UserClaims存放到context中
		c.Set(config.AuthUserClaims, uc)
		c.Next()
	}
}

// WriteAccess 访客账号只允许读取数据
func WriteAccess() gin.HandlerFunc {
	return func(c *gin.Context) {
		switch c.Request.Method {
		case http.MethodGet, http.MethodHead, http.MethodOptions:
			c.Next()
			return
		}

		v, ok := c.Get(config.AuthUserClaims)
		if !ok {
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
			c.Abort()
			return
		}
		uc, ok := v.(*utils.UserClaims)
		if !ok {
			dto.CustomResult(http.StatusUnauthorized, dto.FAILED, nil, "鉴权失败,请重新登录", c)
			c.Abort()
			return
		}
		if !model.UserCanWrite(uc.Role) {
			dto.CustomResult(http.StatusForbidden, dto.FAILED, nil, "访客账号仅允许查看，禁止修改数据", c)
			c.Abort()
			return
		}

		c.Next()
	}
}
