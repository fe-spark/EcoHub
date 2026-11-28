package user

import (
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"fmt"
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"time"

	"github.com/redis/go-redis/v9"
)

// TokenCheckStatus 表示令牌在 Redis 中的校验结果。
type TokenCheckStatus int

const (
	TokenValid TokenCheckStatus = iota
	TokenMissing
	TokenSuperseded
)

func tokenDigest(token string) string {
	sum := sha256.Sum256([]byte(token))
	return hex.EncodeToString(sum[:])
}

func userTokenGen(userId uint) (int64, error) {
	gen, err := db.Rdb.Get(db.Cxt, fmt.Sprintf(config.UserTokenGenKey, userId)).Int64()
	if errors.Is(err, redis.Nil) {
		return 0, nil
	}
	if err != nil {
		return 0, err
	}
	return gen, nil
}

func visitorTokenKey(userId uint, gen int64, token string) string {
	return fmt.Sprintf(config.UserMultiTokenKey, userId, gen, tokenDigest(token))
}

// SaveUserToken 保存用户登录 Token。
// 访客账号按设备独立存储以支持多点登录；非访客账号单点覆盖（后登录顶掉前登录）。
func SaveUserToken(token string, userId uint, role int) error {
	if db.Rdb == nil || token == "" || userId == 0 {
		return errors.New("登录状态不可用")
	}
	if model.UserAllowsMultiLogin(role) {
		gen, err := userTokenGen(userId)
		if err != nil {
			return err
		}
		return db.Rdb.Set(db.Cxt, visitorTokenKey(userId, gen, token), "1", config.AuthTokenExpires*time.Hour).Err()
	}
	return db.Rdb.Set(db.Cxt, fmt.Sprintf(config.UserTokenKey, userId), token, config.AuthTokenExpires*time.Hour).Err()
}

// CheckUserToken 校验用户 Token 在 Redis 中的状态。
// 访客账号检查当前世代下该设备会话是否存在；非访客账号校验单点 Token 并区分失效与异地顶替。
func CheckUserToken(userId uint, role int, token string) (TokenCheckStatus, error) {
	if db.Rdb == nil || userId == 0 || token == "" {
		return TokenMissing, errors.New("登录状态不可用")
	}
	if model.UserAllowsMultiLogin(role) {
		gen, err := userTokenGen(userId)
		if err != nil {
			return TokenMissing, err
		}
		exists, err := db.Rdb.Exists(db.Cxt, visitorTokenKey(userId, gen, token)).Result()
		if err != nil {
			return TokenMissing, err
		}
		if exists > 0 {
			return TokenValid, nil
		}
		return TokenMissing, nil
	}
	stored, err := db.Rdb.Get(db.Cxt, fmt.Sprintf(config.UserTokenKey, userId)).Result()
	if errors.Is(err, redis.Nil) {
		return TokenMissing, nil
	}
	if err != nil {
		return TokenMissing, err
	}
	if stored == "" {
		return TokenMissing, nil
	}
	if stored != token {
		return TokenSuperseded, nil
	}
	return TokenValid, nil
}

// RevokeUserToken 注销当前登录会话（访客仅失效当前设备 Token，非访客清除单点 Token）。
func RevokeUserToken(userId uint, role int, token string) error {
	if db.Rdb == nil || userId == 0 {
		return errors.New("登录状态不可用")
	}
	if model.UserAllowsMultiLogin(role) {
		if token == "" {
			return errors.New("登录状态不可用")
		}
		gen, err := userTokenGen(userId)
		if err != nil {
			return err
		}
		return db.Rdb.Del(db.Cxt, visitorTokenKey(userId, gen, token)).Err()
	}
	return db.Rdb.Del(db.Cxt, fmt.Sprintf(config.UserTokenKey, userId)).Err()
}

// ClearUserToken 清除指定用户的全部登录状态（改密/封禁/改角色/删除）。
// 同时删除单点 Token 并递增多点会话世代号，使所有访客设备立即失效。
func ClearUserToken(userId uint) error {
	if db.Rdb == nil || userId == 0 {
		return errors.New("登录状态不可用")
	}
	pipe := db.Rdb.TxPipeline()
	pipe.Del(db.Cxt, fmt.Sprintf(config.UserTokenKey, userId))
	pipe.Incr(db.Cxt, fmt.Sprintf(config.UserTokenGenKey, userId))
	_, err := pipe.Exec(db.Cxt)
	return err
}
