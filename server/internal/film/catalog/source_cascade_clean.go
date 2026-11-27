package catalog

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// CascadeCleanSourceData 分短事务批量删除该站档案与挂载数据，避免大站长事务锁库。
func CascadeCleanSourceData(id string) ([]int64, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	return CleanSourceDataBatched(id)
}

// deleteHomeRecommendsBySource 清理指定采集站的首页推荐分组与条目，并复位该站初始化标记。
func deleteHomeRecommendsBySource(conn *gorm.DB, sourceID string) error {
	if conn.Migrator().HasTable(&model.HomeRecommendItem{}) {
		if err := conn.Where("source_id = ?", sourceID).Delete(&model.HomeRecommendItem{}).Error; err != nil {
			return err
		}
	}
	if conn.Migrator().HasTable(&model.HomeRecommendGroup{}) {
		if err := conn.Where("source_id = ?", sourceID).Delete(&model.HomeRecommendGroup{}).Error; err != nil {
			return err
		}
	}
	if conn.Migrator().HasTable(&model.FilmSource{}) && conn.Migrator().HasColumn(&model.FilmSource{}, "home_recommend_inited") {
		if err := conn.Model(&model.FilmSource{}).
			Where("id = ?", sourceID).
			Update("home_recommend_inited", false).Error; err != nil {
			return err
		}
	}
	return nil
}

func CleanSourceDataBatched(id string) ([]int64, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("采集站不存在")
	}
	_ = EnsureFilmSourceDeletedIndex()
	var allOwned []int64
	for {
		var chunkMids []int64
		if err := db.Mdb.Unscoped().Model(&model.FilmIndex{}).Where("first_source_id = ?", id).Limit(5000).Pluck("mid", &chunkMids).Error; err != nil {
			return allOwned, err
		}
		if len(chunkMids) == 0 {
			break
		}
		if err := deleteOwnedFilmsBatched(id, chunkMids); err != nil {
			return allOwned, err
		}
		allOwned = append(allOwned, chunkMids...)
	}
	if err := DeleteBySourceBatched(&model.FilmSourcePlaylist{}, model.TableFilmSourcePlaylist, "source_id", id); err != nil {
		return allOwned, err
	}
	if err := DeleteBySourceBatched(&model.MovieSourceMapping{}, model.TableMovieSourceMapping, "source_id", id); err != nil {
		return allOwned, err
	}
	if err := DeleteBySourceBatched(&model.CollectRecordFilmItem{}, model.TableCollectRecordFilm, "source_id", id); err != nil {
		return allOwned, err
	}
	if err := db.Mdb.Where("source_id = ?", id).Delete(&model.BannerSlot{}).Error; err != nil {
		return allOwned, err
	}
	if err := deleteHomeRecommendsBySource(db.Mdb, id); err != nil {
		return allOwned, err
	}
	return allOwned, nil
}

// AssertSourceDataCleanedTx 删站前断言该站核心数据已全部清理完毕。
// 若仍有残留（如归属档案、播放列表、源站映射、采集清单等），阻断删除站点配置，防止出现不可回收的孤儿垃圾数据。
func AssertSourceDataCleanedTx(tx *gorm.DB, sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if tx == nil || sourceID == "" {
		return nil
	}
	var count int64
	if err := tx.Unscoped().Model(&model.FilmIndex{}).Where("first_source_id = ?", sourceID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("采集站影片档案未清理完毕（剩余 %d 部），保留站点以供重试", count)
	}
	if err := tx.Model(&model.FilmSourcePlaylist{}).Where("source_id = ?", sourceID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("采集站播放列表未清理完毕（剩余 %d 条），保留站点以供重试", count)
	}
	if err := tx.Model(&model.MovieSourceMapping{}).Where("source_id = ?", sourceID).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return fmt.Errorf("采集站影片映射未清理完毕（剩余 %d 条），保留站点以供重试", count)
	}
	if tx.Migrator().HasTable(&model.CollectRecordFilmItem{}) {
		if err := tx.Model(&model.CollectRecordFilmItem{}).Where("source_id = ?", sourceID).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return fmt.Errorf("采集站采集记录清单未清理完毕（剩余 %d 条），保留站点以供重试", count)
		}
	}
	return nil
}

func DeleteBySourceBatched(modelPtr any, tableName, col, sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if db.Mdb == nil || sourceID == "" {
		return nil
	}
	if !db.Mdb.Migrator().HasTable(tableName) {
		return nil
	}
	if db.Mdb.Dialector == nil || db.Mdb.Dialector.Name() != "mysql" {
		return db.Mdb.Where(col+" = ?", sourceID).Delete(modelPtr).Error
	}
	const batchLimit = 3000
	query := "DELETE FROM " + tableName + " WHERE " + col + " = ? LIMIT ?"
	totalDeleted := int64(0)
	for {
		res := db.Mdb.Exec(query, sourceID, batchLimit)
		if res.Error != nil {
			return res.Error
		}
		totalDeleted += res.RowsAffected
		if res.RowsAffected < batchLimit {
			break
		}
		if totalDeleted%12000 == 0 {
			log.Printf("[Spider] 分批清理 %s 进行中: 已清理 %d 行 (source_id=%s)", tableName, totalDeleted, sourceID)
		}
	}
	if totalDeleted > 0 {
		log.Printf("[Spider] 分批清理 %s 完成 source_id=%s 共清理 %d 行", tableName, sourceID, totalDeleted)
	}
	return nil
}

// DeleteFilmCascadeTx 在事务内按 mid 分批级联删除影片关联数据（播放源、匹配键、映射、轮播、刮削记录与索引）。
func DeleteFilmCascadeTx(tx *gorm.DB, mids []int64) error {
	for i := 0; i < len(mids); i += 1000 {
		if err := deleteOwnedFilmChunk(tx, mids[i:min(i+1000, len(mids))]); err != nil {
			return err
		}
	}
	return nil
}

func deleteOwnedFilmsBatched(sourceID string, mids []int64) error {
	total := len(mids)
	if total == 0 {
		return nil
	}
	log.Printf("[Spider] 开始分批删除归属站点影片档案 source_id=%s 总数=%d", sourceID, total)
	for i := 0; i < total; i += 1000 {
		end := min(i+1000, total)
		chunk := mids[i:end]
		if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
			return deleteOwnedFilmChunk(tx, chunk)
		}); err != nil {
			return err
		}
		if (end%10000 == 0) || end == total {
			log.Printf("[Spider] 分批清理影片档案进度: %d/%d (source_id=%s)", end, total, sourceID)
		}
	}
	return nil
}

func deleteOwnedFilmChunk(tx *gorm.DB, mids []int64) error {
	if len(mids) == 0 {
		return nil
	}
	if err := tx.Where("mid IN ?", mids).Delete(&model.FilmSourcePlaylist{}).Error; err != nil {
		return err
	}
	if err := tx.Where("mid IN ?", mids).Delete(&model.MovieMatchKey{}).Error; err != nil {
		return err
	}
	if err := tx.Where("global_mid IN ?", mids).Delete(&model.MovieSourceMapping{}).Error; err != nil {
		return err
	}
	if err := tx.Where("mid IN ?", mids).Delete(&model.BannerSlot{}).Error; err != nil {
		return err
	}
	if tx.Migrator().HasTable(&model.HomeRecommendItem{}) {
		if err := tx.Where("mid IN ?", mids).Delete(&model.HomeRecommendItem{}).Error; err != nil {
			return err
		}
	}
	if err := tx.Where("mid IN ?", mids).Delete(&model.FilmTMDBScrape{}).Error; err != nil {
		return err
	}
	return tx.Unscoped().Where("mid IN ?", mids).Delete(&model.FilmIndex{}).Error
}
