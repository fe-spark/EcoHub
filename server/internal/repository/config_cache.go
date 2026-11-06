package repository

import (
	"errors"
	"fmt"
	"log"

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

// writeCache 写入配置缓存。写入失败时删除旧键，避免落库后的新配置继续命中旧值。
// 删除也失败才返回错误。
func writeCache(key, payload string) error {
	if db.Rdb == nil || key == "" {
		return nil
	}
	if err := db.Rdb.Set(db.Cxt, key, payload, config.ConfigCacheTTL).Err(); err != nil {
		if delErr := dropCache(key); delErr != nil {
			log.Printf("[Config] 写入缓存失败且旧键删除失败 key=%s set=%v del=%v", key, err, delErr)
			return fmt.Errorf("写入缓存失败: %v; 删除缓存失败: %w", err, delErr)
		}
		log.Printf("[Config] 写入缓存失败，已删除旧键 key=%s err=%v", key, err)
		return nil
	}
	return nil
}

// fillCache 用数据库结果回填缓存。写入失败只记日志，不挡住这次已经读到的结果。
func fillCache(key, payload string) error {
	if db.Rdb == nil || key == "" {
		return nil
	}
	if err := db.Rdb.Set(db.Cxt, key, payload, config.ConfigCacheTTL).Err(); err != nil {
		log.Printf("[Config] 回填缓存失败 key=%s err=%v", key, err)
		return nil
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
