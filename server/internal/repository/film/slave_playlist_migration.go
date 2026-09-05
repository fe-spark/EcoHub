package film

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var migrateLegacyMu sync.Mutex

const (
	migrateLegacyLockTTL = 15 * time.Minute
)

// MigrateLegacyMoviePlaylistsTx 幂等平滑割接：将老表 movie_playlist 中附属站数据迁移至 slave_movie_playlists。
// 采用批次双写事务：将老表数据批量写入新表，写入成功后在同一事务内物理删除老表对应批次。
// 当老表数据量为 0 时自然退出。即使割接过程被中断或有新爬虫写入，也能安全断点续传，不漏一条数据。
func MigrateLegacyMoviePlaylistsTx(tx *gorm.DB) error {
	migrateLegacyMu.Lock()
	defer migrateLegacyMu.Unlock()

	// Redis 分布式排他锁（规避多 Pod 滚动部署启动时并发争抢老表割接导致死锁）
	if db.Rdb != nil {
		token := fmt.Sprintf("%d-%d", os.Getpid(), time.Now().UnixNano())
		acquired, err := db.Rdb.SetNX(db.Cxt, config.MigrateLegacyPlaylistsLockKey, token, migrateLegacyLockTTL).Result()
		if err != nil {
			log.Printf("[Migration] 获取割接分布式排他锁失败: %v, 降级执行", err)
		} else if !acquired {
			log.Println("[Migration] 集群中其它节点正在执行附属站播放列表割接迁移，本节点跳过")
			return nil
		} else {
			defer func() {
				releaseScript := redis.NewScript(`
					if redis.call("get", KEYS[1]) == ARGV[1] then
						return redis.call("del", KEYS[1])
					else
						return 0
					end
				`)
				_ = releaseScript.Run(db.Cxt, db.Rdb, []string{config.MigrateLegacyPlaylistsLockKey}, token).Err()
			}()
		}
	}

	if tx == nil {
		tx = db.Mdb
	}
	if tx == nil {
		return nil
	}

	// 1. 若老表无数据，说明已割接完毕或为全新部署，直接跳过
	var legacyCount int64
	if err := tx.Model(&model.MoviePlaylist{}).Count(&legacyCount).Error; err != nil {
		return err
	}
	if legacyCount == 0 {
		return nil
	}

	log.Printf("[Migration] 开始割接附属站播放列表: 老表待迁移记录数=%d...", legacyCount)
	startedAt := time.Now()

	const batchSize = 2000
	var migratedTotal int64
	var lastLogCount int64

	for {
		var rows []model.MoviePlaylist
		if err := tx.Order("id ASC").Limit(batchSize).Find(&rows).Error; err != nil {
			return err
		}
		if len(rows) == 0 {
			break
		}

		items := make([]model.SlaveMoviePlaylist, 0, len(rows))
		ids := make([]uint, 0, len(rows))
		for _, r := range rows {
			ids = append(ids, r.ID)
			items = append(items, model.SlaveMoviePlaylist{
				CreatedAt:  r.CreatedAt,
				UpdatedAt:  r.UpdatedAt,
				SourceId:   r.SourceId,
				MovieKey:   r.MovieKey,
				GroupIndex: r.GroupIndex,
				GroupName:  r.GroupName,
				Content:    r.Content,
			})
		}

		type groupKey struct {
			SourceId   string
			MovieKey   string
			GroupIndex int
		}
		itemMap := make(map[groupKey]model.SlaveMoviePlaylist, len(rows))
		for _, item := range items {
			gk := groupKey{SourceId: item.SourceId, MovieKey: item.MovieKey, GroupIndex: item.GroupIndex}
			itemMap[gk] = item
		}
		dedupedItems := make([]model.SlaveMoviePlaylist, 0, len(itemMap))
		for _, item := range items {
			gk := groupKey{SourceId: item.SourceId, MovieKey: item.MovieKey, GroupIndex: item.GroupIndex}
			if last, ok := itemMap[gk]; ok {
				dedupedItems = append(dedupedItems, last)
				delete(itemMap, gk)
			}
		}

		// 在单批次事务内：写入新表 -> 物理删除老表该批次记录
		err := tx.Transaction(func(subTx *gorm.DB) error {
			if len(dedupedItems) > 0 {
				if err := subTx.Clauses(clause.OnConflict{
					Columns:   []clause.Column{{Name: "source_id"}, {Name: "movie_key"}, {Name: "group_index"}},
					DoUpdates: clause.AssignmentColumns([]string{"group_name", "content", "updated_at"}),
				}).CreateInBatches(&dedupedItems, 500).Error; err != nil {
					return err
				}
			}

			for i := 0; i < len(ids); i += 500 {
				end := i + 500
				if end > len(ids) {
					end = len(ids)
				}
				if err := subTx.Unscoped().Where("id IN ?", ids[i:end]).Delete(&model.MoviePlaylist{}).Error; err != nil {
					return err
				}
			}
			return nil
		})
		if err != nil {
			return err
		}

		migratedTotal += int64(len(items))
		if migratedTotal-lastLogCount >= 20000 {
			lastLogCount = migratedTotal
			log.Printf("[Migration] 附属站播放列表割接进度: 已迁移=%d/%d (%.1f%%), 耗时=%s",
				migratedTotal, legacyCount, float64(migratedTotal)/float64(legacyCount)*100, time.Since(startedAt))
		}
	}

	log.Printf("[Migration] 附属站播放列表平滑割接完成: 迁移并清理记录=%d, 总耗时=%s", migratedTotal, time.Since(startedAt))
	return nil
}
