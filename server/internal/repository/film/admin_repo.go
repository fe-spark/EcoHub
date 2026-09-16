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
	"server/internal/repository/film/playlist"
	"server/internal/repository/film/snapshot"
	"server/internal/repository/film/writer"
	"server/internal/repository/support"
	"server/internal/utils"

	"gorm.io/gorm"
)

func DelFilmSearch(id int64) error {
	info := GetFilmIndexById(id)
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		// 查出该 mid 关联的所有 match_key，在事务内级联物理删除附属站关联播放列表
		var matchKeys []string
		if err := tx.Model(&model.MovieMatchKey{}).Where("mid = ?", id).Pluck("match_key", &matchKeys).Error; err != nil {
			return err
		}
		if len(matchKeys) > 0 {
			if err := tx.Unscoped().Where("movie_key IN ?", matchKeys).Delete(&model.SlaveMoviePlaylist{}).Error; err != nil {
				return err
			}
		}
		if err := tx.Where("mid = ?", id).Delete(&model.FilmIndex{}).Error; err != nil {
			return err
		}
		if err := tx.Where("mid = ?", id).Delete(&model.MovieDetailInfo{}).Error; err != nil {
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
	// 主站切换只重建影片骨架和派生读模型；附属站播放列表是补充信息，继续保留等待新主站匹配键重新挂接。
	for _, table := range masterDataResetTables() {
		startedAt := time.Now()
		if err := truncateTable(conn, table); err != nil {
			return err
		}
		if cost := time.Since(startedAt); cost > time.Second {
			log.Printf("[Collect] 主站切换清表较慢 table=%s cost=%s", table, cost)
		}
	}
	if err := repository.DeleteCollectSourceStatsTx(conn, sourceIDs...); err != nil {
		return err
	}
	playlist.ClearOrphanCleanCursor()
	playlist.SetMasterSwitchProtection(playlist.MasterSwitchColdStartDuration)
	return nil
}

func masterDataResetTables() []string {
	return []string{
		model.TableMovieDetail,
		model.TableFilmIndex,
		model.TableFilmListSnapshot,
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
	// 清库时顺带去掉旧 bulk 迁移遗留的 Redis 公告 key 与孤儿治理游标。
	defer ClearLegacyContentKeyNotices()
	playlist.ClearOrphanCleanCursor()
	playlist.SetMasterSwitchProtection(playlist.MasterSwitchColdStartDuration)

	// 关键节点：清空影视库存
	ReportResetProgress(20, "正在清空影视库存")
	for _, t := range []string{
		model.TableMovieDetail,
		model.TableFilmIndex,
		model.TableSlaveMoviePlaylist,
		model.TableMovieMatchKey,
		model.TableMoviePoster,
	} {
		if err := truncateTable(db.Mdb, t); err != nil {
			return fmt.Errorf("truncate %s failed: %w", t, err)
		}
	}

	// 关键节点：清空采集派生数据（快照/筛选/搜索标签/统计）
	ReportResetProgress(45, "正在清空派生数据")
	for _, t := range []string{
		model.TableFilmListSnapshot,
		model.TableCollectSourceStats,
		model.TableSearchTag,
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

	// 关键节点：清理缓存
	ReportResetProgress(90, "正在清理缓存")
	snapshot.ClearSnapshotState()
	RefreshMasterDataCaches()
	ReportResetProgress(95, "数据清空完成")
	return nil
}

func RefreshMasterDataCaches() {
	markCategoryChanged()
	if db.Rdb != nil {
		db.Rdb.Del(db.Cxt, config.BannersKey)
	}
	cache.ClearTVBoxListCache()
	cache.ClearTVBoxConfigCache()
}

func InvalidateMasterSwitchCaches() {
	snapshot.ClearActiveFilmReadModel()
	snapshot.ClearActiveSnapshotVersion()
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

// CleanEmptyFilms 清理所有片名为空或无法识别大类(Pid=0)的垃圾记录
func CleanEmptyFilms() int64 {
	var infos []model.FilmIndex
	db.Mdb.Where("name = ? OR name IS NULL OR pid = 0", "").Find(&infos)
	if len(infos) == 0 {
		return 0
	}
	for _, info := range infos {
		_ = DelFilmSearch(info.Mid)
		cache.ClearSearchTagsCache(info.Pid)
	}
	return int64(len(infos))
}

// CleanSearchWithoutDetail 清理影片索引存在但 movie_detail_info 缺失的脏记录。
func CleanSearchWithoutDetail() int64 {
	type orphanRecord struct {
		Mid int64
		Pid int64
	}

	var records []orphanRecord
	err := db.Mdb.Model(&model.FilmIndex{}).
		Select("film_index.mid, film_index.pid").
		Joins("LEFT JOIN movie_detail_info ON movie_detail_info.mid = film_index.mid AND movie_detail_info.deleted_at IS NULL").
		Where("movie_detail_info.id IS NULL").
		Scan(&records).Error
	if err != nil {
		log.Printf("CleanSearchWithoutDetail Error: %v", err)
		return 0
	}
	if len(records) == 0 {
		return 0
	}

	mids := make([]int64, 0, len(records))
	pidSet := make(map[int64]struct{}, len(records))
	for _, record := range records {
		if record.Mid <= 0 {
			continue
		}
		mids = append(mids, record.Mid)
		if record.Pid > 0 {
			pidSet[record.Pid] = struct{}{}
		}
	}
	if len(mids) == 0 {
		return 0
	}

	err = db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("mid IN ?", mids).Delete(&model.FilmIndex{}).Error; err != nil {
			return err
		}
		if err := tx.Where("mid IN ?", mids).Delete(&model.MovieDetailInfo{}).Error; err != nil {
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
		log.Printf("CleanSearchWithoutDetail Delete Error: %v", err)
		return 0
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
	return int64(len(mids))
}
