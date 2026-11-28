package store

import (
	"fmt"
	"strings"
	"sync"
	"time"

	"server/internal/analytics/types"
	appcfg "server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// GetDataStats 统计当前数据分析积累的数据库与 Redis 体量
func GetDataStats() (types.DataStats, error) {
	var stats types.DataStats
	if db.Mdb != nil {
		if err := db.Mdb.Model(&model.AccessDailyStats{}).Count(&stats.DailyStatsCount).Error; err != nil {
			return stats, err
		}
		if err := db.Mdb.Model(&model.AccessDailyTop{}).Count(&stats.DailyTopCount).Error; err != nil {
			return stats, err
		}
		if stats.DailyStatsCount > 0 {
			type statsAgg struct {
				Earliest string `gorm:"column:earliest"`
				Latest   string `gorm:"column:latest"`
				TotalPV  int64  `gorm:"column:total_pv"`
				TotalUV  int64  `gorm:"column:total_uv"`
			}
			var agg statsAgg
			if err := db.Mdb.Model(&model.AccessDailyStats{}).
				Select("MIN(day) AS earliest, MAX(day) AS latest, COALESCE(SUM(pv), 0) AS total_pv, COALESCE(SUM(uv), 0) AS total_uv").
				Scan(&agg).Error; err != nil {
				return stats, err
			}
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
			if err := db.Mdb.Model(&model.AccessDailyTop{}).
				Select("MIN(day) AS earliest, MAX(day) AS latest").
				Scan(&tagg).Error; err != nil {
				return stats, err
			}
			stats.EarliestDay = tagg.Earliest
			stats.LatestDay = tagg.Latest
		}
	}

	if db.Rdb != nil {
		ctx := db.Cxt
		var cursor uint64
		for {
			keys, nextCursor, err := db.Rdb.Scan(ctx, cursor, appcfg.AnalyticsKeyPrefix+"*", 500).Result()
			if err != nil {
				return stats, err
			}
			stats.RedisKeyCount += int64(len(keys))
			cursor = nextCursor
			if cursor == 0 {
				break
			}
		}
	}
	return stats, nil
}

// ClearData 清理数据分析积累的 Redis 缓存与 MySQL 历史汇总。
// retentionDays <= 0 表示全量清空；retentionDays > 0 表示保留最近 N 天。
func ClearData(retentionDays int, mu *sync.Mutex, afterClear func()) (types.ClearResult, error) {
	var res types.ClearResult
	if retentionDays < 0 {
		return res, fmt.Errorf("保留天数不能为负数: %d", retentionDays)
	}
	if mu != nil {
		mu.Lock()
		defer mu.Unlock()
	}

	if db.Rdb != nil {
		if err := db.Rdb.Ping(db.Cxt).Err(); err != nil {
			return res, fmt.Errorf("Redis 不可用，取消清理: %w", err)
		}
		var cutoffRedisDay string
		if retentionDays > 0 {
			now := time.Now().In(time.Local)
			cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -retentionDays)
			cutoffRedisDay = cutoff.Format("20060102")
		}
		deleted, err := deleteMatchingRedisKeys(retentionDays, cutoffRedisDay)
		res.DeletedRedisKeys = deleted
		if err != nil {
			return res, fmt.Errorf("清理 Redis 访问分析缓存失败: %w", err)
		}
		if retentionDays <= 0 {
			secondPass, err := deleteMatchingRedisKeys(retentionDays, cutoffRedisDay)
			if err != nil {
				return res, fmt.Errorf("二次清理 Redis 访问分析缓存失败: %w", err)
			}
			res.DeletedRedisKeys += secondPass
		}
	}

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
				return nil
			}
			now := time.Now().In(time.Local)
			cutoff := time.Date(now.Year(), now.Month(), now.Day(), 0, 0, 0, 0, time.Local).AddDate(0, 0, -retentionDays)
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
			return nil
		})
		if err != nil {
			return res, err
		}
	}

	if afterClear != nil {
		afterClear()
	}
	return res, nil
}

func deleteMatchingRedisKeys(retentionDays int, cutoffRedisDay string) (int64, error) {
	ctx := db.Cxt
	const batchSize = 500
	var (
		deleted int64
		cursor  uint64
		batch   = make([]string, 0, batchSize)
	)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		n, err := db.Rdb.Unlink(ctx, batch...).Result()
		deleted += n
		batch = batch[:0]
		return err
	}
	for {
		keys, nextCursor, err := db.Rdb.Scan(ctx, cursor, appcfg.AnalyticsKeyPrefix+"*", 500).Result()
		if err != nil {
			return deleted, err
		}
		for _, k := range keys {
			if retentionDays <= 0 || IsKeyOlderThan(k, cutoffRedisDay) {
				batch = append(batch, k)
				if len(batch) >= batchSize {
					if err := flush(); err != nil {
						return deleted, err
					}
				}
			}
		}
		cursor = nextCursor
		if cursor == 0 {
			break
		}
	}
	if err := flush(); err != nil {
		return deleted, err
	}
	return deleted, nil
}

// IsKeyOlderThan 判断分析缓存 Key 是否早于指定日期 (YYYYMMDD)
func IsKeyOlderThan(key, cutoffRedisDay string) bool {
	suffix := strings.TrimPrefix(key, appcfg.AnalyticsKeyPrefix)
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
