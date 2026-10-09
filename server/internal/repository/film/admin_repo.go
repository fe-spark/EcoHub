package film

import (
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/film/cache"
	"server/internal/repository/film/snapshot"
	"server/internal/repository/film/writer"
	"server/internal/repository/support"
	"server/internal/utils"

	"gorm.io/gorm"
)

func DelFilmSearch(id int64) error {
	info := GetFilmIndexById(id)
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("mid = ?", id).Delete(&model.FilmSourcePlaylist{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("mid = ?", id).Delete(&model.FilmIndex{}).Error; err != nil {
			return err
		}
		if err := tx.Where("mid = ?", id).Delete(&model.MovieMatchKey{}).Error; err != nil {
			return err
		}
		if err := tx.Where("global_mid = ?", id).Delete(&model.MovieSourceMapping{}).Error; err != nil {
			return err
		}
		if err := tx.Where("mid = ?", id).Delete(&model.Banner{}).Error; err != nil {
			return err
		}
		return nil
	})

	if err == nil {
		snapshot.DeleteActiveSnapshotsByMids(id)
		cache.ClearTVBoxListCache()
		if info != nil {
			cache.ClearSearchTagsCache(info.Pid)
		}
	}
	return err
}

func ClearMasterDataBySourceIDsFast(sourceIDs ...string) error {
	ids := normalizeSourceIDs(sourceIDs...)
	if len(ids) == 0 {
		return nil
	}

	startedAt := time.Now()
	if err := clearMasterDataBySourceIDs(db.Mdb, ids); err != nil {
		return err
	}
	clearCost := time.Since(startedAt)

	cacheStartedAt := time.Now()
	InvalidateMasterSwitchCaches()
	log.Printf("[Collect] 主站切换数据重置完成 sources=%d clear=%s cache=%s total=%s", len(ids), clearCost, time.Since(cacheStartedAt), time.Since(startedAt))
	return nil
}

func normalizeSourceIDs(sourceIDs ...string) []string {
	ids := make([]string, 0, len(sourceIDs))
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" {
			continue
		}
		if _, ok := seen[sourceID]; ok {
			continue
		}
		seen[sourceID] = struct{}{}
		ids = append(ids, sourceID)
	}
	return ids
}

func clearMasterDataBySourceIDs(conn *gorm.DB, sourceIDs []string) error {
	for _, table := range masterDataResetTables() {
		startedAt := time.Now()
		if err := truncateTable(conn, table); err != nil {
			return err
		}
		if cost := time.Since(startedAt); cost > time.Second {
			log.Printf("[Collect] 源重置清表较慢 table=%s cost=%s", table, cost)
		}
	}
	return repository.DeleteCollectSourceStatsTx(conn, sourceIDs...)
}

func masterDataResetTables() []string {
	return []string{
		model.TableFilmIndex,
		model.TableFilmSourcePlaylist,
		model.TableMovieMatchKey,
		model.TableMovieSourceMapping,
		model.TableSearchTag,
		model.TableCategory,
		model.TableCategoryMapping,
		model.TableSourceCategory,
		model.TableBanners,
	}
}

func truncateTable(conn *gorm.DB, table string) error {
	return support.TruncateTable(conn, table)
}

// FilmZero 删除所有库存数据 (包含 MySQL 持久化表)
func FilmZero() error {
	// 关键节点：清空影视库存
	ReportResetProgress(20, "正在清空影视库存")
	for _, t := range []string{
		model.TableFilmIndex,
		model.TableFilmSourcePlaylist,
		model.TableMovieSourceMapping,
		model.TableMovieMatchKey,
		model.TableMoviePoster,
	} {
		if err := truncateTable(db.Mdb, t); err != nil {
			return fmt.Errorf("truncate %s failed: %w", t, err)
		}
	}

	// 关键节点：清空采集派生与依赖运营数据（快照/筛选/搜索标签/统计/失败记录/轮播图）
	ReportResetProgress(45, "正在清空派生数据")
	for _, t := range []string{
		model.TableCollectSourceStats,
		model.TableSearchTag,
		model.TableFailureRecord,
		model.TableBanners,
	} {
		if err := truncateTable(db.Mdb, t); err != nil {
			return fmt.Errorf("truncate %s failed: %w", t, err)
		}
	}
	// 图库元数据与本地文件一并清理，避免重置后 DB 有记录、磁盘无文件导致素材中心黑块
	if err := truncateTable(db.Mdb, model.TableFileInfo); err != nil {
		return fmt.Errorf("truncate files failed: %w", err)
	}
	if err := utils.ClearGalleryDir(); err != nil {
		return fmt.Errorf("clear gallery dir failed: %w", err)
	}

	// 关键节点：清空分类与映射（统一使用 truncateTable 物理清空，规避软删除残留引发唯一键冲突）
	ReportResetProgress(70, "正在清空分类与映射")
	for _, t := range []string{
		model.TableCategory,
		model.TableMovieSourceMapping,
		model.TableCategoryMapping,
		model.TableSourceCategory,
	} {
		if err := truncateTable(db.Mdb, t); err != nil {
			return fmt.Errorf("truncate %s failed: %w", t, err)
		}
	}
	time.Sleep(100 * time.Millisecond)

	// 关键节点：清理缓存。ClearSnapshotState 只丢掉内存快照版本和首页/TVBox 等入口缓存；
	// 详情 PlayInfo、分类列表、搜索、相关推荐仍按 TTL 活着，必须再扫一遍动态缓存前缀。
	ReportResetProgress(90, "正在清理缓存")
	snapshot.ClearSnapshotState()
	snapshot.ClearAllSnapshotDynamicCaches()
	snapshot.BumpSearchCacheVersion()
	RefreshMasterDataCaches()
	ReportResetProgress(95, "数据清空完成")
	return nil
}

func RefreshMasterDataCaches() {
	markCategoryChanged()
	if db.Rdb != nil {
		db.Rdb.Del(db.Cxt, config.BannersKey, config.DailyUpdatesV2CatCacheKey)
	}
	cache.ClearPatterns(fmt.Sprintf("%s:*", config.DailyUpdatesV2CachePrefix))
	cache.ClearTVBoxListCache()
	cache.ClearTVBoxConfigCache()
}

func InvalidateMasterSwitchCaches() {
	snapshot.ClearActiveFilmReadModel()
	snapshot.ClearActiveSnapshotVersion()
	snapshot.ClearAllSnapshotDynamicCaches()
	snapshot.BumpSearchCacheVersion()
	support.RefreshCategoryCache()
	support.InitMappingEngine()
	support.TouchCategoryVersion()
	if db.Rdb != nil {
		db.Rdb.Del(
			db.Cxt,
			config.SnapshotActiveVersionKey,
			config.ActiveCategoryTreeKey,
			config.CategoryTreeKey,
			config.TVBoxConfigCacheKey,
			config.BannersKey,
		)
	}
}

const filmCleanBatchSize = 500

type filmCleanRow struct {
	Mid int64
	Pid int64
}

// CleanEmptyFilms 分批硬删片名为空或无法识别大类（pid=0）的档案。
func CleanEmptyFilms() int64 {
	return cleanFilmBatches("CleanEmptyFilms", func(q *gorm.DB) *gorm.DB {
		return q.Where("name = ? OR name IS NULL OR pid = 0", "")
	})
}

// CleanPlaylessFilms 清理无任何播放线路且超过宽限期的幽灵影片（例如源站全部删除后残留的影片）
func CleanPlaylessFilms(gracePeriod time.Duration) int64 {
	if gracePeriod <= 0 {
		gracePeriod = 7 * 24 * time.Hour
	}
	cutoff := time.Now().Add(-gracePeriod)

	return cleanFilmBatches("CleanPlaylessFilms", func(q *gorm.DB) *gorm.DB {
		return q.Joins("LEFT JOIN film_source_playlists ON film_source_playlists.mid = film_index.mid AND film_source_playlists.line_kind = 'play'").
			Where("film_source_playlists.mid IS NULL AND film_index.updated_at < ? AND film_index.created_at < ?", cutoff, cutoff)
	})
}

func cleanFilmBatches(logName string, scope func(*gorm.DB) *gorm.DB) int64 {
	if db.Mdb == nil {
		return 0
	}
	var cleaned int64
	for {
		var rows []filmCleanRow
		q := scope(db.Mdb.Model(&model.FilmIndex{}).Select("film_index.mid, film_index.pid")).Limit(filmCleanBatchSize)
		if err := q.Scan(&rows).Error; err != nil {
			log.Printf("%s Error: %v", logName, err)
			return cleaned
		}
		if len(rows) == 0 {
			return cleaned
		}
		mids := make([]int64, 0, len(rows))
		pidSet := make(map[int64]struct{}, len(rows))
		for _, row := range rows {
			if row.Mid <= 0 {
				continue
			}
			mids = append(mids, row.Mid)
			if row.Pid > 0 {
				pidSet[row.Pid] = struct{}{}
			}
		}
		if len(mids) == 0 {
			log.Printf("%s 跳过无效 mid，停止本轮", logName)
			return cleaned
		}
		if err := deleteFilmCascadeBatch(mids, pidSet); err != nil {
			log.Printf("%s Delete Error: %v", logName, err)
			return cleaned
		}
		cleaned += int64(len(mids))
		if len(rows) < filmCleanBatchSize {
			return cleaned
		}
	}
}

// CleanOrphanMatchKeysAndMappings 清理关联影片已被删除的悬空匹配键与源映射
func CleanOrphanMatchKeysAndMappings() int64 {
	var cleaned int64
	res1 := db.Mdb.Where("mid NOT IN (?)", db.Mdb.Model(&model.FilmIndex{}).Select("mid")).Delete(&model.MovieMatchKey{})
	if res1.Error == nil {
		cleaned += res1.RowsAffected
	}
	res2 := db.Mdb.Where("global_mid NOT IN (?)", db.Mdb.Model(&model.FilmIndex{}).Select("mid")).Delete(&model.MovieSourceMapping{})
	if res2.Error == nil {
		cleaned += res2.RowsAffected
	}
	return cleaned
}

func deleteFilmCascadeBatch(mids []int64, pidSet map[int64]struct{}) error {
	if len(mids) == 0 {
		return nil
	}
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("mid IN ?", mids).Delete(&model.FilmSourcePlaylist{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("mid IN ?", mids).Delete(&model.FilmIndex{}).Error; err != nil {
			return err
		}
		if err := tx.Where("mid IN ?", mids).Delete(&model.MovieMatchKey{}).Error; err != nil {
			return err
		}
		if err := tx.Where("global_mid IN ?", mids).Delete(&model.MovieSourceMapping{}).Error; err != nil {
			return err
		}
		if err := tx.Where("mid IN ?", mids).Delete(&model.Banner{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return err
	}

	if len(pidSet) > 0 {
		pids := make([]int64, 0, len(pidSet))
		for pid := range pidSet {
			pids = append(pids, pid)
		}
		if rebuildErr := writer.RefreshSearchTagsByPids(pids...); rebuildErr != nil {
			log.Printf("RebuildSearchTagsByPids Error: %v", rebuildErr)
		}
	}
	writer.ClearFilmIndexCachesByPidSet(pidSet)
	snapshot.DeleteActiveSnapshotsByMids(mids...)
	cache.ClearTVBoxListCache()
	return nil
}
