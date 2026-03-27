package repository

import (
	"fmt"
	"log"
	"server/internal/config"
	"server/internal/infra/db"
	"time"
)

// SaveUserToken 将用户登录成功后的token字符串存放到redis中
func SaveUserToken(token string, userId uint) error {
	// 设置redis中token的过期时间为 token过期时间后的7天
	return db.Rdb.Set(db.Cxt, fmt.Sprintf(config.UserTokenKey, userId), token, (config.AuthTokenExpires+7*24)*time.Hour).Err()
}

// GetUserTokenById 从redis中获取指定userId对应的token
func GetUserTokenById(userId uint) string {
	token, err := db.Rdb.Get(db.Cxt, fmt.Sprintf(config.UserTokenKey, userId)).Result()
	if err != nil {
		log.Println(err)
		return ""
	}
	return token
}

// ClearUserToken 清除指定id的用户的登录信息
func ClearUserToken(userId uint) error {
	return db.Rdb.Del(db.Cxt, fmt.Sprintf(config.UserTokenKey, userId)).Err()
}
