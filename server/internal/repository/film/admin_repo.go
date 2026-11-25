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

	"gorm.io/gorm"
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
		if err := repository.DeleteBannerSlotsByMids(tx, cleanIds); err != nil {
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
		model.TableBannerSlot,
		model.TableFilmTMDBScrape,
	}
}

func truncateTable(conn *gorm.DB, table string) error {
	return support.TruncateTable(conn, table)
}

func RefreshMasterDataCaches() {
	markCategoryChanged()
	cache.ClearTVBoxListCache()
	cache.ClearTVBoxConfigCache()
}

func InvalidateMasterSwitchCaches() {
	repository.ResetConfigCacheGens()
	repository.InvalidatePublicSourceCache()
	writer.ResetSearchTagMemoryCaches()
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
		)
	}
}

// filmCleanBatchSize 每轮清理的影片数。包级变量，测试可调小。
var filmCleanBatchSize = 500

type filmCleanRow struct {
	Mid       int64
	Pid       int64
	Name      string
	CreatedAt time.Time
	UpdatedAt time.Time
}

// CleanEmptyFilms 分批硬删片名为空或无法识别大类（pid=0）的档案。
func CleanEmptyFilms() (int64, error) {
	return cleanFilmScanBatches("CleanEmptyFilms", func(batch []filmCleanRow) ([]int64, map[int64]struct{}, error) {
		var mids []int64
		pidSet := make(map[int64]struct{})
		for _, row := range batch {
			if row.Mid > 0 && (strings.TrimSpace(row.Name) == "" || row.Pid == 0) {
				mids = append(mids, row.Mid)
				if row.Pid > 0 {
					pidSet[row.Pid] = struct{}{}
				}
			}
		}
		return mids, pidSet, nil
	})
}

// CleanPlaylessFilms 清理无任何播放线路且超过宽限期的幽灵影片（例如源站全部删除后残留的影片）
func CleanPlaylessFilms(gracePeriod time.Duration) (int64, error) {
	if gracePeriod <= 0 {
		gracePeriod = 7 * 24 * time.Hour
	}
	cutoff := time.Now().Add(-gracePeriod)

	return cleanFilmScanBatches("CleanPlaylessFilms", func(batch []filmCleanRow) ([]int64, map[int64]struct{}, error) {
		candMids := make([]int64, 0)
		candPids := make(map[int64]int64)
		for _, row := range batch {
			if row.Mid > 0 && row.UpdatedAt.Before(cutoff) && row.CreatedAt.Before(cutoff) {
				candMids = append(candMids, row.Mid)
				candPids[row.Mid] = row.Pid
			}
		}
		return filterPlaylessMids(candMids, candPids)
	})
}

// CleanGhostFilms 在单轮游标扫描中同时清理空记录与超过宽限期的无线路幽灵影片，
// 避免重复扫描 film_index，并在整轮结束后统一刷新搜索标签、缓存与内存检索索引。
func CleanGhostFilms(gracePeriod time.Duration) (int64, int64, error) {
	if gracePeriod <= 0 {
		gracePeriod = 7 * 24 * time.Hour
	}
	cutoff := time.Now().Add(-gracePeriod)

	var emptyCount, playlessCount int64
	_, err := cleanFilmScanBatches("CleanGhostFilms", func(batch []filmCleanRow) ([]int64, map[int64]struct{}, error) {
		var mids []int64
		pidSet := make(map[int64]struct{})
		candMids := make([]int64, 0)
		candPids := make(map[int64]int64)

		for _, row := range batch {
			if row.Mid <= 0 {
				continue
			}
			if strings.TrimSpace(row.Name) == "" || row.Pid == 0 {
				mids = append(mids, row.Mid)
				if row.Pid > 0 {
					pidSet[row.Pid] = struct{}{}
				}
				emptyCount++
				continue
			}
			if row.UpdatedAt.Before(cutoff) && row.CreatedAt.Before(cutoff) {
				candMids = append(candMids, row.Mid)
				candPids[row.Mid] = row.Pid
			}
		}

		playlessMids, playlessPids, err := filterPlaylessMids(candMids, candPids)
		if err != nil {
			return nil, nil, err
		}
		if len(playlessMids) > 0 {
			mids = append(mids, playlessMids...)
			playlessCount += int64(len(playlessMids))
			for pid := range playlessPids {
				pidSet[pid] = struct{}{}
			}
		}
		return mids, pidSet, nil
	})
	return emptyCount, playlessCount, err
}

func filterPlaylessMids(candMids []int64, candPids map[int64]int64) ([]int64, map[int64]struct{}, error) {
	if len(candMids) == 0 {
		return nil, nil, nil
	}
	var hasPlayMids []int64
	if err := db.Mdb.Model(&model.FilmSourcePlaylist{}).
		Distinct("mid").
		Where("mid IN ? AND line_kind = ?", candMids, "play").
		Pluck("mid", &hasPlayMids).Error; err != nil {
		return nil, nil, err
	}
	hasPlay := make(map[int64]struct{}, len(hasPlayMids))
	for _, mid := range hasPlayMids {
		hasPlay[mid] = struct{}{}
	}
	var mids []int64
	pidSet := make(map[int64]struct{})
	for _, mid := range candMids {
		if _, ok := hasPlay[mid]; ok {
			continue
		}
		mids = append(mids, mid)
		if pid := candPids[mid]; pid > 0 {
			pidSet[pid] = struct{}{}
		}
	}
	return mids, pidSet, nil
}

const cleanProgressLogEveryBatches = 20

// cleanFilmScanBatches 按主键 mid 游标定长分窗扫描并过滤删除，避免过滤后置触发百万全表扫描超 readTimeout。
// 扫描期间仅做事务内级联删，受影响的 pid 集合与已删 mid 统一在扫描结束后刷新一次搜索标签、缓存与检索索引。
func cleanFilmScanBatches(logName string, pick func([]filmCleanRow) ([]int64, map[int64]struct{}, error)) (int64, error) {
	if db.Mdb == nil {
		return 0, nil
	}
	batchSize := filmCleanBatchSize
	if batchSize <= 0 {
		batchSize = 500
	}
	var cleaned int64
	var cursor int64
	var deletedMids []int64
	allPidSet := make(map[int64]struct{})
	startedAt := time.Now()
	var batches, scanned int64
	defer func() {
		if len(deletedMids) > 0 {
			flushFilmCleanSideEffects(deletedMids, allPidSet)
		}
	}()

	for {
		var batch []filmCleanRow
		if err := db.Mdb.Model(&model.FilmIndex{}).
			Select("mid, pid, name, created_at, updated_at").
			Where("mid > ?", cursor).
			Order("mid").
			Limit(batchSize).
			Scan(&batch).Error; err != nil {
			log.Printf("%s Error: %v", logName, err)
			return cleaned, fmt.Errorf("查询待清理影片失败: %w", err)
		}
		if len(batch) == 0 {
			return cleaned, nil
		}
		next := batch[len(batch)-1].Mid
		if next <= cursor {
			return cleaned, nil
		}
		cursor = next
		batches++
		scanned += int64(len(batch))

		mids, pidSet, err := pick(batch)
		if err != nil {
			log.Printf("%s Pick Error: %v", logName, err)
			return cleaned, fmt.Errorf("筛选待清理影片失败: %w", err)
		}
		if len(mids) > 0 {
			if err := deleteFilmCascadeDBBatch(mids); err != nil {
				log.Printf("%s Delete Error: %v", logName, err)
				return cleaned, fmt.Errorf("删除影片失败: %w", err)
			}
			cleaned += int64(len(mids))
			deletedMids = append(deletedMids, mids...)
			for pid := range pidSet {
				allPidSet[pid] = struct{}{}
			}
		}
		if batches%cleanProgressLogEveryBatches == 0 || len(batch) < batchSize {
			log.Printf("[OrphanClean] %s 进度: batches=%d scanned=%d cleaned=%d cursor=%d elapsed=%s",
				logName, batches, scanned, cleaned, cursor, time.Since(startedAt).Round(time.Millisecond))
		}
		if len(batch) < batchSize {
			return cleaned, nil
		}
	}
}

// orphanScanBatchSize 每轮按 mid 游标顺序扫描的候选 mid 数；orphanDeleteBatchSize 每条 DELETE 的 mid 上限。
// 两者都是包级变量，方便测试调小以覆盖多批次。
var (
	orphanScanBatchSize   = 5000
	orphanDeleteBatchSize = 1000
	// orphanBeforeDeleteHook 仅供测试：在 film_index 预检查之后、DELETE 之前调用，用于模拟并发写入。
	orphanBeforeDeleteHook func(table string, orphans []int64)
)

// CleanOrphanPlaylists 分批清理关联影片已不存在的播放列表（film_source_playlists.mid 不在 film_index 中）。
func CleanOrphanPlaylists() (int64, error) {
	return cleanOrphanRowsByMid(model.TableFilmSourcePlaylist, "mid")
}

// CleanOrphanMatchKeysAndMappings 分批清理关联影片已被删除的悬空匹配键与源映射。
// 任一步出错都会返回已清理条数与错误，交给定时任务告警。
func CleanOrphanMatchKeysAndMappings() (int64, error) {
	keys, err := cleanOrphanRowsByMid(model.TableMovieMatchKey, "mid")
	if err != nil {
		return keys, fmt.Errorf("清理悬空匹配键失败: %w", err)
	}
	mappings, err := cleanOrphanRowsByMid(model.TableMovieSourceMapping, "global_mid")
	if err != nil {
		return keys + mappings, fmt.Errorf("清理悬空源映射失败: %w", err)
	}
	return keys + mappings, nil
}

// cleanOrphanRowsByMid 按 mid 升序游标分批找出 table.column 中不在 film_index 的 mid 并删除。
// 每轮只走索引取一小段 DISTINCT mid，再到 film_index 主键查存在性，最后按 IN 列表删除，
// 避免整表 NOT EXISTS 的长语句撞上 MySQL readTimeout。
// film_index 存在性检查与原 SQL 一致，不过滤软删除记录。
func cleanOrphanRowsByMid(table, column string) (int64, error) {
	if db.Mdb == nil {
		return 0, nil
	}
	scanSize := orphanScanBatchSize
	if scanSize <= 0 {
		scanSize = 5000
	}
	deleteSize := orphanDeleteBatchSize
	if deleteSize <= 0 {
		deleteSize = 1000
	}
	session := db.Mdb.Session(&gorm.Session{SkipDefaultTransaction: true})
	var cleaned int64
	var cursor int64 = -1 << 63
	startedAt := time.Now()
	var batches, scanned int64
	for {
		var candidates []int64
		if err := session.Table(table).
			Distinct(column).
			Where(column+" > ?", cursor).
			Order(column).
			Limit(scanSize).
			Pluck(column, &candidates).Error; err != nil {
			return cleaned, fmt.Errorf("扫描 %s 失败: %w", table, err)
		}
		if len(candidates) == 0 {
			return cleaned, nil
		}
		next := candidates[len(candidates)-1]
		if next <= cursor {
			// 防御：游标不前进时直接结束，避免死循环
			return cleaned, nil
		}
		cursor = next
		batches++
		scanned += int64(len(candidates))

		var existing []int64
		if err := session.Unscoped().Model(&model.FilmIndex{}).
			Where("mid IN ?", candidates).
			Pluck("mid", &existing).Error; err != nil {
			return cleaned, fmt.Errorf("查询 film_index 失败: %w", err)
		}
		exists := make(map[int64]struct{}, len(existing))
		for _, mid := range existing {
			exists[mid] = struct{}{}
		}
		orphans := make([]int64, 0, len(candidates)-len(existing))
		for _, mid := range candidates {
			if _, ok := exists[mid]; !ok {
				orphans = append(orphans, mid)
			}
		}
		if orphanBeforeDeleteHook != nil {
			orphanBeforeDeleteHook(table, orphans)
		}
		for i := 0; i < len(orphans); i += deleteSize {
			end := i + deleteSize
			if end > len(orphans) {
				end = len(orphans)
			}
			// 删除时再原子复核一次 film_index，避免预检查与删除之间新写入的影片被误删。
			res := session.Exec("DELETE FROM "+table+" WHERE "+column+" IN ? AND NOT EXISTS (SELECT 1 FROM "+
				model.TableFilmIndex+" WHERE "+model.TableFilmIndex+".mid = "+table+"."+column+")", orphans[i:end])
			if res.Error != nil {
				return cleaned, fmt.Errorf("删除 %s 失败: %w", table, res.Error)
			}
			cleaned += res.RowsAffected
		}
		if batches%cleanProgressLogEveryBatches == 0 || len(candidates) < scanSize {
			log.Printf("[OrphanClean] 扫描 %s.%s 进度: batches=%d scanned=%d cleaned=%d cursor=%d elapsed=%s",
				table, column, batches, scanned, cleaned, cursor, time.Since(startedAt).Round(time.Millisecond))
		}
		if len(candidates) < scanSize {
			return cleaned, nil
		}
	}
}

func deleteFilmCascadeDBBatch(mids []int64) error {
	if len(mids) == 0 {
		return nil
	}
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
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
		if err := repository.DeleteBannerSlotsByMids(tx, mids); err != nil {
			return err
		}
		return nil
	})
}

func flushFilmCleanSideEffects(deletedMids []int64, pidSet map[int64]struct{}) {
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
	snapshot.DeleteActiveSnapshotsByMids(deletedMids...)
	cache.ClearTVBoxListCache()
	RefreshInventoryStatsAsync()
}
