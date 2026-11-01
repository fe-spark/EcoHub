package repository

import (
	"errors"
	"fmt"
	"server/internal/config"
	"server/internal/infra/db"
	"time"

	"github.com/redis/go-redis/v9"
)

// SaveUserToken 保存用户单点登录 Token（后登录覆盖前登录）
func SaveUserToken(token string, userId uint) error {
	if db.Rdb == nil || token == "" || userId == 0 {
		return errors.New("登录状态不可用")
	}
	return db.Rdb.Set(db.Cxt, fmt.Sprintf(config.UserTokenKey, userId), token, config.AuthTokenExpires*time.Hour).Err()
}

// GetUserToken 获取用户当前有效 Token。Redis 没有该键时返回空字符串。
func GetUserToken(userId uint) (string, error) {
	if db.Rdb == nil || userId == 0 {
		return "", errors.New("登录状态不可用")
	}
	token, err := db.Rdb.Get(db.Cxt, fmt.Sprintf(config.UserTokenKey, userId)).Result()
	if errors.Is(err, redis.Nil) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	return token, nil
}

// ClearUserToken 清除指定用户的登录状态（登出/改密/封禁）
func ClearUserToken(userId uint) error {
	if db.Rdb == nil || userId == 0 {
		return errors.New("登录状态不可用")
	}
	return db.Rdb.Del(db.Cxt, fmt.Sprintf(config.UserTokenKey, userId)).Err()
}
