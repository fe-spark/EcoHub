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
	"server/internal/repository/support"

	"gorm.io/gorm"
)

func bumpSearchTagsCacheVersion() {
	db.Rdb.Set(db.Cxt, config.SearchTagsVersionKey, time.Now().UnixNano(), 0)
}

func getSearchTagsCacheVersion() string {
	version, err := db.Rdb.Get(db.Cxt, config.SearchTagsVersionKey).Result()
	if err == nil && version != "" {
		return version
	}
	version = fmt.Sprintf("%d", time.Now().UnixNano())
	db.Rdb.Set(db.Cxt, config.SearchTagsVersionKey, version, 0)
	return version
}

func DelFilmSearch(id int64) error {
	info := GetFilmIndexById(id)
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
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

	if err == nil && info != nil {
		if rebuildErr := RefreshSearchTagsByPids(info.Pid); rebuildErr != nil {
			log.Printf("RebuildSearchTagsByPids Error: %v", rebuildErr)
			return rebuildErr
		}
		DeleteActiveSnapshotsByMids(id)
		ClearAdminFilmSearchCache()
		ClearSearchTagsCache(info.Pid)
		ClearTVBoxListCache()
		support.ClearIndexPageCache()
	}
	return err
}

func ShieldFilmSearch(cid int64) error {
	pID := support.GetParentId(cid)

	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("cid = ?", cid).Delete(&model.FilmIndex{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Printf("ShieldFilmSearch Error: %v", err)
		return err
	}

	if pID > 0 {
		if rebuildErr := RefreshSearchTagsByPids(pID); rebuildErr != nil {
			log.Printf("RebuildSearchTagsByPids Error: %v", rebuildErr)
			return rebuildErr
		}
		DeleteActiveSnapshotsByCategory("cid", cid)
		ClearAdminFilmSearchCache()
		ClearSearchTagsCache(pID)
	}
	ClearTVBoxListCache()
	support.ClearIndexPageCache()
	return nil
}

func ShieldRootFilmSearch(pid int64) error {
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		return tx.Where("cid = ? OR (pid = ? AND cid = 0)", pid, pid).Delete(&model.FilmIndex{}).Error
	})
	if err != nil {
		log.Printf("ShieldRootFilmSearch Error: %v", err)
		return err
	}

	if rebuildErr := RefreshSearchTagsByPids(pid); rebuildErr != nil {
		log.Printf("RebuildSearchTagsByPids Error: %v", rebuildErr)
		return rebuildErr
	}
	DeleteActiveRootSnapshots(pid)
	ClearAdminFilmSearchCache()
	ClearSearchTagsCache(pid)
	ClearTVBoxListCache()
	support.ClearIndexPageCache()
	return nil
}

func RecoverFilmSearch(cid int64) error {
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.FilmIndex{}).Unscoped().Where("cid = ?", cid).Update("deleted_at", nil).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Printf("RecoverFilmSearch Error: %v", err)
		return err
	}

	pID := support.GetParentId(cid)
	if pID > 0 {
		if rebuildErr := RefreshSearchTagsByPids(pID); rebuildErr != nil {
			log.Printf("RebuildSearchTagsByPids Error: %v", rebuildErr)
			return rebuildErr
		}
		RestoreActiveSnapshotsByCategory(cid)
		ClearAdminFilmSearchCache()
		ClearSearchTagsCache(pID)
	}
	ClearTVBoxListCache()
	support.ClearIndexPageCache()
	return nil
}

func ClearMasterDataBySourceIDsTx(tx *gorm.DB, sourceIDs ...string) error {
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
	if len(ids) == 0 {
		return nil
	}

	var mids []int64
	if err := tx.Model(&model.FilmIndex{}).Where("source_id IN ?", ids).Pluck("mid", &mids).Error; err != nil {
		return err
	}

	if err := tx.Where("source_id IN ?", ids).Delete(&model.FilmIndex{}).Error; err != nil {
		return err
	}
	if err := tx.Where("source_id IN ?", ids).Delete(&model.MovieDetailInfo{}).Error; err != nil {
		return err
	}
	if err := tx.Where("source_id IN ?", ids).Delete(&model.MoviePlaylist{}).Error; err != nil {
		return err
	}
	if err := tx.Where("source_id IN ?", ids).Delete(&model.MovieSourceMapping{}).Error; err != nil {
		return err
	}
	if err := deleteMovieMatchKeysByMids(tx, mids); err != nil {
		return err
	}
	if len(mids) > 0 {
		if err := tx.Where("mid IN ?", mids).Delete(&model.Banner{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.SearchTagItem{}).Error; err != nil {
		return err
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.VirtualPictureQueue{}).Error; err != nil {
		return err
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.Category{}).Error; err != nil {
		return err
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.CategoryMapping{}).Error; err != nil {
		return err
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.SourceCategory{}).Error; err != nil {
		return err
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.FilmListSnapshot{}).Error; err != nil {
		return err
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.FilmFilterOptionSnapshot{}).Error; err != nil {
		return err
	}
	if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.FilmFilterIndexSnapshot{}).Error; err != nil {
		return err
	}
	if err := repository.DeleteCollectSourceStatsTx(tx, ids...); err != nil {
		return err
	}
	return nil
}

// ClearSearchTagsCache 清除特定分类的所有复合搜索标签缓存
func ClearSearchTagsCache(pid int64) {
	pattern := fmt.Sprintf("%s:*", config.SearchTags)
	ctx := db.Cxt
	iter := db.Rdb.Scan(ctx, 0, pattern, config.MaxScanCount).Iterator()
	for iter.Next(ctx) {
		db.Rdb.Del(ctx, iter.Val())
	}
	bumpSearchTagsCacheVersion()
}

// ClearTVBoxConfigCache 清除 TVBox 配置缓存
func ClearTVBoxConfigCache() {
	db.Rdb.Del(db.Cxt, config.TVBoxConfigCacheKey)
	pattern := config.TVBoxConfigCacheKey + ":*"
	iter := db.Rdb.Scan(db.Cxt, 0, pattern, config.MaxScanCount).Iterator()
	for iter.Next(db.Cxt) {
		db.Rdb.Del(db.Cxt, iter.Val())
	}
}

func ClearTVBoxListCache() {
	pattern := config.TVBoxList + ":*"
	iter := db.Rdb.Scan(db.Cxt, 0, pattern, config.MaxScanCount).Iterator()
	for iter.Next(db.Cxt) {
		db.Rdb.Del(db.Cxt, iter.Val())
	}
}

// ClearAllSearchTagsCache 清除所有分类的搜索标签缓存 (扫描清理)
func ClearAllSearchTagsCache() {
	pattern := config.SearchTags + ":*"
	iter := db.Rdb.Scan(db.Cxt, 0, pattern, config.MaxScanCount).Iterator()
	for iter.Next(db.Cxt) {
		db.Rdb.Del(db.Cxt, iter.Val())
	}
	bumpSearchTagsCacheVersion()
	ClearTVBoxConfigCache()
}

// FilmZero 删除所有库存数据 (包含 MySQL 持久化表)
func FilmZero() error {
	tables := []string{
		model.TableMovieDetail,
		model.TableFilmIndex,
		model.TableFilmListSnapshot,
		model.TableFilterOption,
		model.TableFilterIndex,
		model.TableMoviePlaylist,
		model.TableMovieMatchKey,
		model.TableCollectSourceStats,
		model.TableCategory,
		model.TableVirtualPicture,
		model.TableSearchTag,
		model.TableBanners,
		model.TableFailureRecord,
	}
	for _, t := range tables {
		if err := db.Mdb.Exec(fmt.Sprintf("TRUNCATE table %s", t)).Error; err != nil {
			return fmt.Errorf("truncate %s failed: %w", t, err)
		}
	}
	if err := db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.MovieSourceMapping{}).Error; err != nil {
		return fmt.Errorf("clear movie source mappings failed: %w", err)
	}
	if err := db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.CategoryMapping{}).Error; err != nil {
		return fmt.Errorf("clear category mappings failed: %w", err)
	}
	if err := db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.SourceCategory{}).Error; err != nil {
		return fmt.Errorf("clear source categories failed: %w", err)
	}
	time.Sleep(100 * time.Millisecond)

	ClearSnapshotState()
	ClearAdminFilmSearchCache()
	RefreshMasterDataCaches()
	return nil
}

// ClearMasterDataBySourceIDs 清理指定站点在主站切换时必须重置的数据。
// 旧主站会清空主数据和自身相关映射，新主站会清空其旧附属站播放列表与映射。
// 其它附属站的数据保持不动，由新主站重建骨架后继续挂接。
func ClearMasterDataBySourceIDs(sourceIDs ...string) error {
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		return ClearMasterDataBySourceIDsTx(tx, sourceIDs...)
	}); err != nil {
		return err
	}
	ClearSnapshotState()
	ClearAdminFilmSearchCache()
	RefreshMasterDataCaches()
	return nil
}

func RefreshMasterDataCaches() {
	markCategoryChanged()
	db.Rdb.Del(db.Cxt, config.VirtualPictureKey)
	db.Rdb.Del(db.Cxt, config.BannersKey)
	ClearTVBoxListCache()
	ClearTVBoxConfigCache()
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
		ClearSearchTagsCache(info.Pid)
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
		if rebuildErr := RefreshSearchTagsByPids(pids...); rebuildErr != nil {
			log.Printf("RebuildSearchTagsByPids Error: %v", rebuildErr)
		}
	}
	clearFilmIndexCachesByPidSet(pidSet)
	DeleteActiveSnapshotsByMids(mids...)
	ClearTVBoxListCache()
	return int64(len(mids))
}
