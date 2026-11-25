package film

import (
	"gorm.io/gorm"
	"log"
	"server/internal/config"
	"server/internal/film/cache"
	"server/internal/film/catalog"
	"server/internal/film/snapshot"
	"server/internal/film/writer"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
	"strings"
	"time"
)

func DelFilmSearchBatch(ids []int64) error {
	playPeers := snapshot.ExpandPlayInfoPeerMids(ids)
	cleanIds, pids, err := DelFilmSearchBatchDB(ids)
	if err != nil {
		return err
	}
	InvalidateFilmSearchBatchCaches(cleanIds, pids, playPeers...)
	return nil
}

// DelFilmSearchBatchDB 仅在事务内物理清理影片与关联表，返回清理的 mid 与受影响的一级分类 ID 列表。
func DelFilmSearchBatchDB(ids []int64) ([]int64, []int64, error) {
	if len(ids) == 0 {
		return nil, nil, nil
	}
	cleanIds := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		cleanIds = append(cleanIds, id)
	}
	if len(cleanIds) == 0 {
		return nil, nil, nil
	}

	var indexes []model.FilmIndex
	_ = db.Mdb.Where("mid IN ?", cleanIds).Find(&indexes)

	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("mid IN ?", cleanIds).Delete(&model.FilmSourcePlaylist{}).Error; err != nil {
			return err
		}
		if err := tx.Unscoped().Where("mid IN ?", cleanIds).Delete(&model.FilmIndex{}).Error; err != nil {
			return err
		}
		if err := tx.Where("mid IN ?", cleanIds).Delete(&model.MovieMatchKey{}).Error; err != nil {
			return err
		}
		if err := tx.Where("global_mid IN ?", cleanIds).Delete(&model.MovieSourceMapping{}).Error; err != nil {
			return err
		}
		if err := tx.Where("mid IN ?", cleanIds).Delete(&model.BannerSlot{}).Error; err != nil {
			return err
		}
		if err := tx.Where("mid IN ?", cleanIds).Delete(&model.FilmTMDBScrape{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		return nil, nil, err
	}

	pids := make([]int64, 0, len(indexes))
	for _, info := range indexes {
		if info.Pid > 0 {
			pids = append(pids, info.Pid)
		}
	}
	return cleanIds, pids, nil
}

// InvalidateFilmSearchBatchCaches 在锁外清理影片删除后的内存读模型与检索缓存。
func InvalidateFilmSearchBatchCaches(cleanIds, pids []int64, playPeerMids ...int64) {
	if len(cleanIds) == 0 {
		return
	}
	snapshot.DeleteActiveSnapshotsByMids(cleanIds...)
	if len(playPeerMids) > 0 {
		_ = snapshot.DeletePlayInfoCacheKeys(playPeerMids)
	}
	snapshot.BumpSearchCacheVersion()
	cache.ClearTVBoxListCache()
	for _, pid := range pids {
		if pid > 0 {
			cache.ClearAllSearchTagsCache()
			break
		}
	}
	RefreshInventoryStatsAsync()
}

func DelFilmSearch(id int64) error {
	return DelFilmSearchBatch([]int64{id})
}

// FilmIDsBelongToSource 验证影片档案均属于指定采集站。
// 管理端按站点展示影片，但 mid 是全局 ID，写操作不能只依赖前端列表过滤。
func FilmIDsBelongToSource(ids []int64, sourceID string) (bool, error) {
	if db.Mdb == nil {
		return false, ErrDatabaseUnavailable
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || len(ids) == 0 {
		return false, nil
	}
	unique := make([]int64, 0, len(ids))
	seen := make(map[int64]struct{}, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		unique = append(unique, id)
	}
	if len(unique) == 0 {
		return false, nil
	}
	var rows []struct {
		Mid           int64
		FirstSourceId string
	}
	if err := db.Mdb.Model(&model.FilmIndex{}).
		Select("mid, first_source_id").
		Where("mid IN ?", unique).
		Find(&rows).Error; err != nil {
		return false, err
	}
	// 所有待校验影片必须真实存在且未被软删除，防止通过 Unscoped 硬删绕过主站归属校验。
	if len(rows) != len(unique) {
		return false, nil
	}
	needPlaylistCheck := make([]int64, 0)
	for _, row := range rows {
		if strings.TrimSpace(row.FirstSourceId) != sourceID {
			needPlaylistCheck = append(needPlaylistCheck, row.Mid)
		}
	}
	if len(needPlaylistCheck) == 0 {
		return true, nil
	}
	var matchedMids []int64
	if err := db.Mdb.Model(&model.FilmSourcePlaylist{}).
		Distinct("mid").
		Where("mid IN ? AND source_id = ? AND line_kind = ?", needPlaylistCheck, sourceID, model.LineKindPlay).
		Pluck("mid", &matchedMids).Error; err != nil {
		return false, err
	}
	matched := make(map[int64]struct{}, len(matchedMids))
	for _, mid := range matchedMids {
		matched[mid] = struct{}{}
	}
	for _, mid := range needPlaylistCheck {
		if _, ok := matched[mid]; !ok {
			return false, nil
		}
	}
	return true, nil
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
		if err := repobase.TruncateTable(conn, table); err != nil {
			return err
		}
		if cost := time.Since(startedAt); cost > time.Second {
			log.Printf("[Collect] 源重置清表较慢 table=%s cost=%s", table, cost)
		}
	}
	return sourcecatalog.DeleteCollectSourceStatsTx(conn, sourceIDs...)
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
		model.TableBannerSlot,
		model.TableFilmTMDBScrape,
	}
}

func RefreshMasterDataCaches() {
	markCategoryChanged()
	cache.ClearTVBoxListCache()
	cache.ClearTVBoxConfigCache()
}

func InvalidateMasterSwitchCaches() {
	repobase.ResetConfigCacheGens()
	catalog.InvalidatePublicSourceCache()
	writer.ResetSearchTagMemoryCaches()
	snapshot.ClearActiveFilmReadModel()
	snapshot.ClearActiveSnapshotVersion()
	snapshot.ClearAllSnapshotDynamicCaches()
	snapshot.BumpSearchCacheVersion()
	taxonomy.RefreshCategoryCache()
	taxonomy.InitMappingEngine()
	taxonomy.TouchCategoryVersion()
	if db.Rdb != nil {
		db.Rdb.Del(
			db.Cxt,
			config.SnapshotActiveVersionKey,
			config.ActiveCategoryTreeKey,
			config.CategoryTreeKey,
			config.TVBoxConfigCacheKey,
		)
	}
}

// filmCleanBatchSize 每轮清理的影片数。包级变量，测试可调小。
var filmCleanBatchSize = 500
