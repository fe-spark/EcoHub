package repository

import (
	"errors"
	"fmt"
	"log"
	"server/internal/config"
	"server/internal/infra/db"
	"time"

	"github.com/redis/go-redis/v9"
)

// SaveUserToken 保存用户单点登录 Token（后登录覆盖前登录）
func SaveUserToken(token string, userId uint) error {
	if db.Rdb == nil || token == "" || userId == 0 {
		return nil
	}
	return db.Rdb.Set(db.Cxt, fmt.Sprintf(config.UserTokenKey, userId), token, config.AuthTokenExpires*time.Hour).Err()
}

// GetUserToken 获取用户当前有效 Token
func GetUserToken(userId uint) string {
	if db.Rdb == nil || userId == 0 {
		return ""
	}
	token, err := db.Rdb.Get(db.Cxt, fmt.Sprintf(config.UserTokenKey, userId)).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			log.Printf("[AuthRepo] 获取用户 Token 失败 userId=%d: %v", userId, err)
		}
		return ""
	}
	return token
}

// ClearUserToken 清除指定用户的登录状态（登出/改密/封禁）
func ClearUserToken(userId uint) error {
	if db.Rdb == nil || userId == 0 {
		return nil
	}
	return db.Rdb.Del(db.Cxt, fmt.Sprintf(config.UserTokenKey, userId)).Err()
}
