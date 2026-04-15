package film

import (
	"fmt"
	"log"
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
		if err := tx.Where("global_mid = ?", id).Delete(&model.MovieSourceMapping{}).Error; err != nil {
			return err
		}
		if err := tx.Where("mid = ?", id).Delete(&model.Banner{}).Error; err != nil {
			return err
		}
		return nil
	})

	if err == nil && info != nil {
		ClearSearchTagsCache(info.Pid)
	}
	return err
}

func ShieldFilmSearch(cid int64) error {
	var mids []int64
	db.Mdb.Model(&model.SearchInfo{}).Where("cid = ?", cid).Pluck("mid", &mids)

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

	if pID := support.GetParentId(cid); pID > 0 {
		ClearSearchTagsCache(pID)
	}
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

	if pID := support.GetParentId(cid); pID > 0 {
		ClearSearchTagsCache(pID)
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
	db.Rdb.Del(ctx, fmt.Sprintf("%s:%d", config.SearchTags, pid))
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
	keys, err := db.Rdb.Keys(db.Cxt, pattern).Result()
	if err == nil && len(keys) > 0 {
		db.Rdb.Del(db.Cxt, keys...)
	}
	ClearTVBoxConfigCache()
}

// FilmZero 删除所有库存数据 (包含 MySQL 持久化表)
func FilmZero() {
	tables := []string{
		model.TableMovieDetail,
		model.TableSearchInfo,
		model.TableMoviePlaylist,
		model.TableCategory,
		model.TableVirtualPicture,
		model.TableSearchTag,
	}
	for _, t := range tables {
		db.Mdb.Exec(fmt.Sprintf("TRUNCATE table %s", t))
	}
	db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.MovieSourceMapping{})
	db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.CategoryMapping{})
	time.Sleep(100 * time.Millisecond)

	support.TruncateRecordTable()
	refreshCategoryCaches()
	support.ClearIndexPageCache()
	db.Rdb.Del(db.Cxt, config.VirtualPictureKey)
	ClearTVBoxListCache()
	support.InitMappingEngine()
}

// MasterFilmZero 仅清理主站相关数据 (search_infos / movie_detail_infos / category)
// 保留附属站 movie_playlists 数据，用于主站切换时防止附属站数据丢失
func MasterFilmZero() {
	tables := []string{
		model.TableSearchInfo,
		model.TableMovieDetail,
		model.TableCategory,
		model.TableVirtualPicture,
		model.TableSearchTag,
	}
	for _, t := range tables {
		db.Mdb.Exec(fmt.Sprintf("TRUNCATE table %s", t))
	}
	db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.MovieSourceMapping{})
	db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.CategoryMapping{})
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
