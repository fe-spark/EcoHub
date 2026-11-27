package repobase

import (
	"errors"
	"fmt"
	"log"
	"sync"
	"sync/atomic"

	"server/internal/config"
	"server/internal/infra/db"

	"github.com/redis/go-redis/v9"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

var (
	configCacheSF  singleflight.Group
	configCacheGen sync.Map // key -> *atomic.Uint64
)

func getConfigCacheGen(key string) *atomic.Uint64 {
	if v, ok := configCacheGen.Load(key); ok {
		return v.(*atomic.Uint64)
	}
	counter := &atomic.Uint64{}
	actual, _ := configCacheGen.LoadOrStore(key, counter)
	return actual.(*atomic.Uint64)
}

func bumpConfigCacheGen(key string) {
	if key != "" {
		getConfigCacheGen(key).Add(1)
	}
}

// ResetConfigCacheGens 递增全部已追踪配置缓存世代，阻止在途回填覆盖新配置或重置后状态。
func ResetConfigCacheGens() {
	configCacheGen.Range(func(_, v any) bool {
		if c, ok := v.(*atomic.Uint64); ok {
			c.Add(1)
		}
		return true
	})
}

func LoadCachedConfig[T any](
	key string,
	decode func(string) (T, error),
	loadDB func() (val T, payload string, shouldCache bool, err error),
) (T, error) {
	var zero T
	if data, ok, err := readCache(key); err != nil {
		log.Printf("[Config] 读取缓存失败，降级查库 key=%s err=%v", key, err)
	} else if ok && data != "" {
		if val, decErr := decode(data); decErr != nil {
			if delErr := DropConfigCache(key); delErr != nil {
				log.Printf("[Config] 清理损坏缓存失败 key=%s err=%v", key, delErr)
			}
		} else {
			return val, nil
		}
	}

	res, err, _ := configCacheSF.Do(key, func() (any, error) {
		if data, ok, err := readCache(key); err == nil && ok && data != "" {
			if val, decErr := decode(data); decErr == nil {
				return val, nil
			}
		}
		gen := getConfigCacheGen(key).Load()
		val, payload, shouldCache, dbErr := loadDB()
		if dbErr != nil {
			return zero, dbErr
		}
		if shouldCache && payload != "" && getConfigCacheGen(key).Load() == gen {
			_ = FillConfigCache(key, payload)
		}
		return val, nil
	})
	if err != nil {
		return zero, err
	}
	return res.(T), nil
}

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
func WriteConfigCache(key, payload string) error {
	bumpConfigCacheGen(key)
	if db.Rdb == nil || key == "" {
		return nil
	}
	if err := db.Rdb.Set(db.Cxt, key, payload, config.ConfigCacheTTL).Err(); err != nil {
		if delErr := DropConfigCache(key); delErr != nil {
			log.Printf("[Config] 写入缓存失败且旧键删除失败 key=%s set=%v del=%v", key, err, delErr)
			return fmt.Errorf("写入缓存失败: %v; 删除缓存失败: %w", err, delErr)
		}
		log.Printf("[Config] 写入缓存失败，已删除旧键 key=%s err=%v", key, err)
		return nil
	}
	return nil
}

// fillCache 用数据库结果回填缓存。写入失败只记日志，不挡住这次已经读到的结果。
func FillConfigCache(key, payload string) error {
	if db.Rdb == nil || key == "" {
		return nil
	}
	if err := db.Rdb.Set(db.Cxt, key, payload, config.ConfigCacheTTL).Err(); err != nil {
		log.Printf("[Config] 回填缓存失败 key=%s err=%v", key, err)
		return nil
	}
	return nil
}

func DropConfigCache(key string) error {
	bumpConfigCacheGen(key)
	if db.Rdb == nil || key == "" {
		return nil
	}
	if err := db.Rdb.Del(db.Cxt, key).Err(); err != nil && !errors.Is(err, redis.Nil) {
		return err
	}
	return nil
}

func RecordNotFound(err error) bool {
	return errors.Is(err, gorm.ErrRecordNotFound)
}
