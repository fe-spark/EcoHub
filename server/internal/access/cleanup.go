package access

import (
	"fmt"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// AccessDataStats 数据分析数据积累统计
type AccessDataStats struct {
	DailyStatsCount int64  `json:"dailyStatsCount"`
	DailyTopCount   int64  `json:"dailyTopCount"`
	RedisKeyCount   int64  `json:"redisKeyCount"`
	EarliestDay     string `json:"earliestDay,omitempty"`
	LatestDay       string `json:"latestDay,omitempty"`
	TotalPV         int64  `json:"totalPv"`
	TotalUV         int64  `json:"totalUv"`
}

// ClearAccessResult 数据清理结果
type ClearAccessResult struct {
	DeletedDailyStats int64 `json:"deletedDailyStats"`
	DeletedDailyTop   int64 `json:"deletedDailyTop"`
	DeletedRedisKeys  int64 `json:"deletedRedisKeys"`
}

// GetAccessDataStats 统计当前数据分析积累的数据体量
func GetAccessDataStats() AccessDataStats {
	var stats AccessDataStats
	if db.Mdb != nil {
		_ = db.Mdb.Model(&model.AccessDailyStats{}).Count(&stats.DailyStatsCount).Error
		_ = db.Mdb.Model(&model.AccessDailyTop{}).Count(&stats.DailyTopCount).Error

		if stats.DailyStatsCount > 0 {
			type statsAgg struct {
				Earliest string `gorm:"column:earliest"`
				Latest   string `gorm:"column:latest"`
				TotalPV  int64  `gorm:"column:total_pv"`
				TotalUV  int64  `gorm:"column:total_uv"`
			}
			var agg statsAgg
			_ = db.Mdb.Model(&model.AccessDailyStats{}).
				Select("MIN(day) AS earliest, MAX(day) AS latest, COALESCE(SUM(pv), 0) AS total_pv, COALESCE(SUM(uv), 0) AS total_uv").
				Scan(&agg).Error
			stats.EarliestDay = agg.Earliest
			stats.LatestDay = agg.Latest
			stats.TotalPV = agg.TotalPV
			stats.TotalUV = agg.TotalUV
		} else if stats.DailyTopCount > 0 {
			type topAgg struct {
				Earliest string `gorm:"column:earliest"`
				Latest   string `gorm:"column:latest"`
			}
			var tagg topAgg
			_ = db.Mdb.Model(&model.AccessDailyTop{}).
				Select("MIN(day) AS earliest, MAX(day) AS latest").
				Scan(&tagg).Error
			stats.EarliestDay = tagg.Earliest
			stats.LatestDay = tagg.Latest
		}
	}

	if db.Rdb != nil {
		ctx := db.Cxt
		lockKey := rollupLockKey()
		var cursor uint64
		for {
			keys, nextCursor, err := db.Rdb.Scan(ctx, cursor, config.AccessKeyPrefix+"*", 500).Result()
			if err != nil {
				break
			}
			for _, k := range keys {
				if k != lockKey {
					stats.RedisKeyCount++
				}
			}
			cursor = nextCursor
			if cursor == 0 {
				break
			}
		}
	}

	return stats
}

// ClearAccessData 清理数据分析积累的数据。
// retentionDays <= 0 表示全量清理；
// retentionDays > 0 表示保留最近 retentionDays 天，删除更早的数据。
// 先拿进程内 rollupMu 再拿 Redis 集群锁，避免其它节点边滚边写把已清数据写回。
func ClearAccessData(retentionDays int) (ClearAccessResult, error) {
	var res ClearAccessResult
	if retentionDays < 0 {
		return res, fmt.Errorf("保留天数不能为负数: %d", retentionDays)
	}

	rollupMu.Lock()
	defer rollupMu.Unlock()

	lockToken, locked, lockErr := acquireRollupClusterLock()
	if lockErr != nil {
		return res, fmt.Errorf("获取集群滚动锁失败: %w", lockErr)
	}
	if !locked {
		return res, fmt.Errorf("数据分析正在滚动落库，请稍后再试")
	}
	defer releaseRollupClusterLock(lockToken)

	if db.Mdb != nil {
		err := db.Mdb.Transaction(func(tx *gorm.DB) error {
			if retentionDays <= 0 {
				t1 := tx.Where("1 = 1").Delete(&model.AccessDailyStats{})
				if t1.Error != nil {
					return t1.Error
				}
				res.DeletedDailyStats = t1.RowsAffected

				t2 := tx.Where("1 = 1").Delete(&model.AccessDailyTop{})
				if t2.Error != nil {
					return t2.Error
				}
				res.DeletedDailyTop = t2.RowsAffected
			} else {
				now := time.Now().In(time.Local)
				cutoff := startOfLocalDay(now).AddDate(0, 0, -retentionDays)
				cutoffDay := cutoff.Format("2006-01-02")

				t1 := tx.Where("day < ?", cutoffDay).Delete(&model.AccessDailyStats{})
				if t1.Error != nil {
					return t1.Error
				}
				res.DeletedDailyStats = t1.RowsAffected

				t2 := tx.Where("day < ?", cutoffDay).Delete(&model.AccessDailyTop{})
				if t2.Error != nil {
					return t2.Error
				}
				res.DeletedDailyTop = t2.RowsAffected
			}
			return nil
		})
		if err != nil {
			return res, err
		}
	}

	if db.Rdb != nil {
		var cutoffRedisDay string
		if retentionDays > 0 {
			now := time.Now().In(time.Local)
			cutoff := startOfLocalDay(now).AddDate(0, 0, -retentionDays)
			cutoffRedisDay = cutoff.Format("20060102")
		}
		deleted, err := deleteMatchingAccessRedisKeys(retentionDays, cutoffRedisDay, rollupLockKey())
		res.DeletedRedisKeys = deleted
		if err != nil {
			return res, fmt.Errorf("清理 Redis 访问分析缓存失败: %w", err)
		}
	}

	return res, nil
}

func deleteMatchingAccessRedisKeys(retentionDays int, cutoffRedisDay, skipKey string) (int64, error) {
	ctx := db.Cxt
	var toDelete []string
	var cursor uint64
	for {
		keys, nextCursor, err := db.Rdb.Scan(ctx, cursor, config.AccessKeyPrefix+"*", 500).Result()
		if err != nil {
			return 0, err
		}
		for _, k := range keys {
			if k == skipKey {
				continue
			}
			if retentionDays <= 0 || isAccessKeyOlderThan(k, cutoffRedisDay) {
				toDelete = append(toDelete, k)
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}

	var deleted int64
	const batchSize = 500
	for i := 0; i < len(toDelete); i += batchSize {
		end := i + batchSize
		if end > len(toDelete) {
			end = len(toDelete)
		}
		n, err := db.Rdb.Del(ctx, toDelete[i:end]...).Result()
		deleted += n
		if err != nil {
			return deleted, err
		}
	}
	return deleted, nil
}

// isAccessKeyOlderThan 判断 Redis 访问分析 Key 是否早于指定日期（YYYYMMDD）
func isAccessKeyOlderThan(key string, cutoffRedisDay string) bool {
	if key == rollupLockKey() {
		return false
	}
	suffix := strings.TrimPrefix(key, config.AccessKeyPrefix)
	if strings.HasPrefix(suffix, "min:") {
		minStr := strings.TrimPrefix(suffix, "min:")
		if len(minStr) >= 8 && isDigits(minStr[:8]) {
			return minStr[:8] < cutoffRedisDay
		}
		return false
	}
	lastColon := strings.LastIndex(suffix, ":")
	if lastColon >= 0 && lastColon < len(suffix)-1 {
		dayPart := suffix[lastColon+1:]
		if len(dayPart) == 8 && isDigits(dayPart) {
			return dayPart < cutoffRedisDay
		}
	}
	return false
}

func isDigits(s string) bool {
	if len(s) == 0 {
		return false
	}
	for i := 0; i < len(s); i++ {
		if s[i] < '0' || s[i] > '9' {
			return false
		}
	}
	return true
}
