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

// SaveUserToken 将用户登录成功后的token添加到该用户的活跃token集合中（支持多设备同时登录）
func SaveUserToken(token string, userId uint) error {
	if db.Rdb == nil || token == "" || userId == 0 {
		return nil
	}
	key := fmt.Sprintf(config.UserTokenKey, userId)
	expireAt := float64(time.Now().Add(config.AuthTokenExpires * time.Hour).Unix())

	// 若存在旧类型的键（如历史 string 类型），直接删除以保证 ZSet 正常写入
	keyType, err := db.Rdb.Type(db.Cxt, key).Result()
	if err == nil && keyType != "none" && keyType != "zset" {
		_ = db.Rdb.Del(db.Cxt, key).Err()
	}

	pipe := db.Rdb.TxPipeline()
	// 顺手清理集合中已过期的旧 token
	pipe.ZRemRangeByScore(db.Cxt, key, "-inf", fmt.Sprint(time.Now().Unix()))
	pipe.ZAdd(db.Cxt, key, redis.Z{Score: expireAt, Member: token})
	pipe.Expire(db.Cxt, key, config.AuthTokenExpires*time.Hour)
	_, err = pipe.Exec(db.Cxt)
	return err
}

// HasUserToken 检查指定 userId 的活跃 token 集合中是否存在该 token 且未过期
func HasUserToken(userId uint, token string) bool {
	if db.Rdb == nil || token == "" || userId == 0 {
		return false
	}
	key := fmt.Sprintf(config.UserTokenKey, userId)
	score, err := db.Rdb.ZScore(db.Cxt, key, token).Result()
	if err != nil {
		if !errors.Is(err, redis.Nil) {
			log.Printf("[AuthRepo] 检查用户 Token 失败 userId=%d: %v", userId, err)
		}
		return false
	}
	// 校验该 token 是否已经超过其独立的过期时间
	if score <= float64(time.Now().Unix()) {
		_ = db.Rdb.ZRem(db.Cxt, key, token).Err()
		return false
	}
	return true
}

// RemoveUserToken 从指定 userId 的活跃 token 集合中移除单台设备的 token（单点退出）
func RemoveUserToken(userId uint, token string) error {
	if db.Rdb == nil || token == "" || userId == 0 {
		return nil
	}
	key := fmt.Sprintf(config.UserTokenKey, userId)
	return db.Rdb.ZRem(db.Cxt, key, token).Err()
}

// ClearUserToken 清除指定 userId 的全部设备登录信息（修改密码/封禁/全量下线）
func ClearUserToken(userId uint) error {
	if db.Rdb == nil || userId == 0 {
		return nil
	}
	return db.Rdb.Del(db.Cxt, fmt.Sprintf(config.UserTokenKey, userId)).Err()
}
