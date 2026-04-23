package film

import (
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

func DelFilmSearch(id int64) error {
	info := GetSearchInfoById(id)
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("mid = ?", id).Delete(&model.SearchInfo{}).Error; err != nil {
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
		if rebuildErr := RebuildSearchTagsByPids(info.Pid); rebuildErr != nil {
			log.Printf("RebuildSearchTagsByPids Error: %v", rebuildErr)
			return rebuildErr
		}
		ClearSearchTagsCache(info.Pid)
		ClearTVBoxListCache()
		support.ClearIndexPageCache()
	}
	return err
}

func ShieldFilmSearch(cid int64) error {
	pID := support.GetParentId(cid)

	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("cid = ?", cid).Delete(&model.SearchInfo{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Printf("ShieldFilmSearch Error: %v", err)
		return err
	}

	if pID > 0 {
		if rebuildErr := RebuildSearchTagsByPids(pID); rebuildErr != nil {
			log.Printf("RebuildSearchTagsByPids Error: %v", rebuildErr)
			return rebuildErr
		}
		ClearSearchTagsCache(pID)
	}
	ClearTVBoxListCache()
	support.ClearIndexPageCache()
	return nil
}

func ShieldRootFilmSearch(pid int64) error {
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		return tx.Where("cid = ? OR (pid = ? AND cid = 0)", pid, pid).Delete(&model.SearchInfo{}).Error
	})
	if err != nil {
		log.Printf("ShieldRootFilmSearch Error: %v", err)
		return err
	}

	if rebuildErr := RebuildSearchTagsByPids(pid); rebuildErr != nil {
		log.Printf("RebuildSearchTagsByPids Error: %v", rebuildErr)
		return rebuildErr
	}
	ClearSearchTagsCache(pid)
	ClearTVBoxListCache()
	support.ClearIndexPageCache()
	return nil
}

func RecoverFilmSearch(cid int64) error {
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Model(&model.SearchInfo{}).Unscoped().Where("cid = ?", cid).Update("deleted_at", nil).Error; err != nil {
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
		if rebuildErr := RebuildSearchTagsByPids(pID); rebuildErr != nil {
			log.Printf("RebuildSearchTagsByPids Error: %v", rebuildErr)
			return rebuildErr
		}
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
	if err := tx.Model(&model.SearchInfo{}).Where("source_id IN ?", ids).Pluck("mid", &mids).Error; err != nil {
		return err
	}

	if err := tx.Where("source_id IN ?", ids).Delete(&model.SearchInfo{}).Error; err != nil {
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
	return nil
}

// ClearSearchTagsCache 清除特定分类的所有复合搜索标签缓存
func ClearSearchTagsCache(pid int64) {
	pattern := fmt.Sprintf("%s:%d:*", config.SearchTags, pid)
	ctx := db.Cxt
	iter := db.Rdb.Scan(ctx, 0, pattern, config.MaxScanCount).Iterator()
	for iter.Next(ctx) {
		db.Rdb.Del(ctx, iter.Val())
	}
}

// ClearTVBoxConfigCache 清除 TVBox 配置缓存
func ClearTVBoxConfigCache() {
	db.Rdb.Del(db.Cxt, config.TVBoxConfigCacheKey)
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
	ClearTVBoxConfigCache()
}

// FilmZero 删除所有库存数据 (包含 MySQL 持久化表)
func FilmZero() {
	tables := []string{
		model.TableMovieDetail,
		model.TableSearchInfo,
		model.TableMoviePlaylist,
		model.TableMovieMatchKey,
		model.TableCategory,
		model.TableVirtualPicture,
		model.TableSearchTag,
	}
	for _, t := range tables {
		db.Mdb.Exec(fmt.Sprintf("TRUNCATE table %s", t))
	}
	db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.MovieSourceMapping{})
	db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.CategoryMapping{})
	time.Sleep(100 * time.Millisecond)

	support.TruncateRecordTable()
	refreshCategoryCaches()
	support.ClearIndexPageCache()
	db.Rdb.Del(db.Cxt, config.VirtualPictureKey)
	ClearTVBoxListCache()
	support.InitMappingEngine()
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
	RefreshMasterDataCaches()
	return nil
}

func RefreshMasterDataCaches() {
	time.Sleep(100 * time.Millisecond)
	refreshCategoryCaches()
	support.ClearIndexPageCache()
	db.Rdb.Del(db.Cxt, config.VirtualPictureKey)
	ClearTVBoxListCache()
	support.InitMappingEngine()
}

// CleanEmptyFilms 清理所有片名为空或无法识别大类(Pid=0)的垃圾记录
func CleanEmptyFilms() int64 {
	var infos []model.SearchInfo
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
