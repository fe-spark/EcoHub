package repository

import (
	"errors"
	"fmt"

	"server/internal/config"
	"server/internal/infra/db"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
)

func readCache(key string) (string, bool, error) {
	if db.Rdb == nil || key == "" {
		return "", false, nil
	}
	data, err := db.Rdb.Get(db.Cxt, key).Result()
	if errors.Is(err, redis.Nil) {
		return "", false, nil
	}
	if err != nil {
		return "", false, err
	}
	return data, true, nil
}

// writeCache 写入配置缓存。写入失败时删除该键，删除也失败则返回错误。
func writeCache(key, payload string) error {
	if db.Rdb == nil || key == "" {
		return nil
	}
	if err := db.Rdb.Set(db.Cxt, key, payload, config.ConfigCacheTTL).Err(); err != nil {
		if delErr := db.Rdb.Del(db.Cxt, key).Err(); delErr != nil {
			return fmt.Errorf("写入缓存失败: %v; 删除缓存失败: %v", err, delErr)
		}
		return fmt.Errorf("写入缓存失败: %w", err)
	}
	return nil
}

// fillCache 用数据库结果回填缓存。写入失败且旧键已删除时不阻断本次读取。
func fillCache(key, payload string) error {
	if db.Rdb == nil || key == "" {
		return nil
	}
	if err := db.Rdb.Set(db.Cxt, key, payload, config.ConfigCacheTTL).Err(); err != nil {
		if delErr := db.Rdb.Del(db.Cxt, key).Err(); delErr != nil {
			return fmt.Errorf("回填缓存失败: %v; 删除缓存失败: %v", err, delErr)
		}
	}
	return nil
}

func dropCache(key string) error {
	if db.Rdb == nil || key == "" {
		return nil
	}
	if err := db.Rdb.Del(db.Cxt, key).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}

func recordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
