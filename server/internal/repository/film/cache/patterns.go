package cache

import (
	"log"
	"path"
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
)

// ClearPatterns 批量清除匹配的 Redis 缓存键。
// 只要模式数量 > 1，统一合并为单次 SCAN 遍历，客户端内存过滤，坚决避免串行发起 N 轮全库扫描。
func ClearPatterns(patterns ...string) {
	if db.Rdb == nil || len(patterns) == 0 {
		return
	}

	validPatterns := make([]string, 0, len(patterns))
	for _, p := range patterns {
		p = strings.TrimSpace(p)
		if p != "" {
			validPatterns = append(validPatterns, p)
		}
	}
	if len(validPatterns) == 0 {
		return
	}

	// 单一模式直接走单个 Redis SCAN
	if len(validPatterns) == 1 {
		scanAndDelPattern(validPatterns[0])
		return
	}

	cp := commonScanPrefix(validPatterns)
	scanPattern := "*"
	if cp != "" {
		scanPattern = cp + "*"
	} else if config.RedisKeyPrefix != "" {
		scanPattern = config.RedisKeyPrefix + ":*"
	}

	iter := db.Rdb.Scan(db.Cxt, 0, scanPattern, 1000).Iterator()
	var batch []string
	for iter.Next(db.Cxt) {
		key := iter.Val()
		if matchAnyPattern(key, validPatterns) {
			batch = append(batch, key)
			if len(batch) >= 100 {
				if err := db.Rdb.Del(db.Cxt, batch...).Err(); err != nil {
					log.Printf("clearCachePatterns Batch Del Error: count=%d err=%v", len(batch), err)
				}
				batch = batch[:0]
			}
		}
	}
	if len(batch) > 0 {
		if err := db.Rdb.Del(db.Cxt, batch...).Err(); err != nil {
			log.Printf("clearCachePatterns Final Batch Del Error: count=%d err=%v", len(batch), err)
		}
	}
	if err := iter.Err(); err != nil {
		log.Printf("clearCachePatterns Scan Error: pattern=%s err=%v", scanPattern, err)
	}
}

func scanAndDelPattern(pattern string) {
	iter := db.Rdb.Scan(db.Cxt, 0, pattern, config.MaxScanCount).Iterator()
	var batch []string
	for iter.Next(db.Cxt) {
		batch = append(batch, iter.Val())
		if len(batch) >= 100 {
			if err := db.Rdb.Del(db.Cxt, batch...).Err(); err != nil {
				log.Printf("scanAndDelPattern Batch Del Error: count=%d err=%v", len(batch), err)
			}
			batch = batch[:0]
		}
	}
	if len(batch) > 0 {
		if err := db.Rdb.Del(db.Cxt, batch...).Err(); err != nil {
			log.Printf("scanAndDelPattern Final Batch Del Error: count=%d err=%v", len(batch), err)
		}
	}
	if err := iter.Err(); err != nil {
		log.Printf("scanAndDelPattern Scan Error: pattern=%s err=%v", pattern, err)
	}
}

func commonScanPrefix(patterns []string) string {
	if len(patterns) == 0 {
		return ""
	}
	prefix := cleanPrefixBeforeGlob(patterns[0])
	for _, p := range patterns[1:] {
		clean := cleanPrefixBeforeGlob(p)
		for !strings.HasPrefix(clean, prefix) {
			if len(prefix) == 0 {
				return ""
			}
			prefix = prefix[:len(prefix)-1]
		}
	}
	return prefix
}

func cleanPrefixBeforeGlob(p string) string {
	if idx := strings.IndexAny(p, "*?["); idx != -1 {
		return p[:idx]
	}
	return p
}

func matchAnyPattern(key string, patterns []string) bool {
	for _, p := range patterns {
		if matchPattern(p, key) {
			return true
		}
	}
	return false
}

func matchPattern(pattern, key string) bool {
	if pattern == key {
		return true
	}
	if strings.HasSuffix(pattern, "*") && strings.Count(pattern, "*") == 1 && !strings.ContainsAny(pattern, "?[") {
		return strings.HasPrefix(key, pattern[:len(pattern)-1])
	}
	matched, _ := path.Match(pattern, key)
	if !matched && strings.Contains(key, "/") {
		escapedPattern := strings.ReplaceAll(pattern, "/", "\x00")
		escapedKey := strings.ReplaceAll(key, "/", "\x00")
		m, _ := path.Match(escapedPattern, escapedKey)
		return m
	}
	return matched
}
