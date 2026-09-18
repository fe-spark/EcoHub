package repository

import (
	"errors"
	"log"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// --------- Collect Source -----------

// GetCollectSourceList 获取采集站 API 列表
func GetCollectSourceList() []model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var list []model.FilmSource
	if err := db.Mdb.Order("grade ASC").Find(&list).Error; err != nil {
		log.Println("GetCollectSourceList Error:", err)
		return nil
	}
	for i := range list {
		normalizeCollectCd(&list[i])
	}
	return list
}

// ReplaceCollectSources 用备份列表整体替换采集站（事务清空后写入）
func ReplaceCollectSources(list []model.FilmSource) error {
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.FilmSource{}).Error; err != nil {
			return err
		}
		if len(list) == 0 {
			return nil
		}
		for i := range list {
			if list[i].Id == "" && list[i].Uri != "" {
				list[i].Id = utils.GenerateHashKey(list[i].Uri)
			}
			normalizeCollectSourceDefaults(&list[i])
		}
		return tx.Create(&list).Error
	})
}

// GetCollectSourceListByGrade 返回指定类型的采集 Api 信息 Master | Slave
func GetCollectSourceListByGrade(grade model.SourceGrade) []model.FilmSource {
	var list []model.FilmSource
	if err := db.Mdb.Where("grade = ?", grade).Find(&list).Error; err != nil {
		log.Println("GetCollectSourceListByGrade Error:", err)
		return nil
	}
	for i := range list {
		normalizeCollectCd(&list[i])
	}
	return list
}

// PickMasterSourceForCategory 选取用于分类树同步的主站。
// 规则：分类树只能来自主站；优先已启用主站；无启用时用任意主站（含未启用）；
// 没有任何主站时返回 nil（没有主站就不能有分类树）。
func PickMasterSourceForCategory() *model.FilmSource {
	masters := GetCollectSourceListByGrade(model.MasterCollect)
	if len(masters) == 0 {
		return nil
	}
	for i := range masters {
		if masters[i].State {
			m := masters[i]
			return &m
		}
	}
	// 未启用的主站仍可作为分类树唯一来源
	m := masters[0]
	return &m
}

// GetEnabledCollectSourceList 获取已启用采集站列表。
func GetEnabledCollectSourceList() []model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var list []model.FilmSource
	if err := db.Mdb.Where("state = ?", true).Order("grade ASC").Find(&list).Error; err != nil {
		log.Println("GetEnabledCollectSourceList Error:", err)
		return nil
	}
	for i := range list {
		normalizeCollectCd(&list[i])
	}
	return list
}

func GetLastCollectTime(sourceID string) *time.Time {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || db.Mdb == nil {
		return nil
	}
	return GetCollectSourceStats([]string{sourceID})[sourceID]
}

func GetCollectSourceStats(sourceIDs []string) map[string]*time.Time {
	result := make(map[string]*time.Time, len(sourceIDs))
	if len(sourceIDs) == 0 {
		return result
	}
	var rows []model.CollectSourceStats
	if err := db.Mdb.Where("source_id IN ?", sourceIDs).Find(&rows).Error; err != nil {
		log.Println("GetCollectSourceStats Error:", err)
		return result
	}
	for _, row := range rows {
		if row.LastCollectTime == nil || row.LastCollectTime.IsZero() {
			continue
		}
		value := *row.LastCollectTime
		result[row.SourceId] = &value
	}
	return result
}

func TouchCollectSourceStatsTx(tx *gorm.DB, sourceID string, at time.Time) error {
	if sourceID == "" {
		return nil
	}
	if at.IsZero() {
		at = time.Now()
	}
	now := time.Now()
	stat := model.CollectSourceStats{SourceId: sourceID, LastCollectTime: &at}
	return tx.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "source_id"}},
		DoUpdates: clause.Assignments(map[string]any{
			"last_collect_time": at,
			"updated_at":        now,
			"deleted_at":        nil,
		}),
	}).Create(&stat).Error
}

func DeleteCollectSourceStatsTx(tx *gorm.DB, sourceIDs ...string) error {
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
	return tx.Where("source_id IN ?", ids).Unscoped().Delete(&model.CollectSourceStats{}).Error
}

// FindCollectSourceById 通过 Id 标识获取对应的资源站信息
func FindCollectSourceById(id string) *model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var fs model.FilmSource
	if err := db.Mdb.Where("id = ?", id).First(&fs).Error; err != nil {
		return nil
	}
	normalizeCollectCd(&fs)
	return &fs
}

// DelCollectResource 通过 Id 删除对应的采集站点信息及其附属采集数据
func DelCollectResource(id string) error {
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		// 1. 删除关联的定时任务关系
		if err := tx.Where("source_id = ?", id).Delete(&model.CronSourceRel{}).Error; err != nil {
			return err
		}
		// 2. 删除附属站播放列表
		if err := tx.Where("source_id = ?", id).Unscoped().Delete(&model.SlaveMoviePlaylist{}).Error; err != nil {
			return err
		}
		// 3. 删除附属站海报图源数据
		if err := tx.Where("source_id = ?", id).Unscoped().Delete(&model.MoviePoster{}).Error; err != nil {
			return err
		}
		// 4. 删除采集失败记录
		if err := DeleteFailureRecordsByOriginIdTx(tx, id); err != nil {
			return err
		}
		// 5. 删除采集站本身
		if err := tx.Where("id = ?", id).Delete(&model.FilmSource{}).Error; err != nil {
			return err
		}
		// 6. 若删除的站点是当前海报源，自动兜底将主站恢复为海报源
		return EnsureDefaultPosterSourceTx(tx)
	})
}

// AddCollectSource 添加采集站信息
func AddCollectSource(s model.FilmSource) error {
	return AddCollectSourceTx(db.Mdb, s)
}

func AddCollectSourceTx(tx *gorm.DB, s model.FilmSource) error {
	if tx == nil {
		return errors.New("database transaction is nil")
	}
	var count int64
	if err := tx.Model(&model.FilmSource{}).Where("uri = ?", s.Uri).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("当前采集站点信息已存在，请勿重复添加")
	}
	// 基于 URI 生成稳定的哈希 ID，确保服务重启后采集源顺序一致且支持主从切换
	if s.Id == "" {
		s.Id = utils.GenerateHashKey(s.Uri)
	}
	// 主站若无外部指定且无活跃海报源，默认作为海报源
	if s.Grade == model.MasterCollect && !s.IsPosterSource {
		var activePosterCount int64
		if err := tx.Model(&model.FilmSource{}).Where("is_poster_source = ? AND state = ?", true, true).Count(&activePosterCount).Error; err == nil && activePosterCount == 0 {
			s.IsPosterSource = true
			log.Printf("[Spider] 主站无活跃海报源，自动设为默认海报图源: id=%s name=%s", s.Id, s.Name)
		}
	}
	normalizeCollectSourceDefaults(&s)
	if s.IsPosterSource {
		if err := DemoteExistingPosterSourceTx(tx, s.Id); err != nil {
			return err
		}
	}
	if err := tx.Create(&s).Error; err != nil {
		return err
	}
	return EnsureDefaultPosterSourceTx(tx)
}

// BatchAddCollectSource 批量添加采集站信息
func BatchAddCollectSource(list []model.FilmSource) error {
	// 为没有 ID 的采集源生成稳定的哈希 ID
	for i := range list {
		if list[i].Id == "" {
			list[i].Id = utils.GenerateHashKey(list[i].Uri)
		}
		normalizeCollectSourceDefaults(&list[i])
	}
	return db.Mdb.Create(list).Error
}

func UpdateCollectSourceTx(tx *gorm.DB, s model.FilmSource) error {
	if tx == nil {
		return errors.New("database transaction is nil")
	}
	var count int64
	if err := tx.Model(&model.FilmSource{}).Where("id != ? AND uri = ?", s.Id, s.Uri).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("当前采集站链接已存在其他站点中，请勿重复添加")
	}
	normalizeCollectSourceDefaults(&s)
	if s.IsPosterSource {
		if err := DemoteExistingPosterSourceTx(tx, s.Id); err != nil {
			return err
		}
	}
	if err := tx.Save(&s).Error; err != nil {
		return err
	}
	return EnsureDefaultPosterSourceTx(tx)
}

func DemoteExistingMasterTx(tx *gorm.DB) error {
	if tx == nil {
		return nil
	}
	return tx.Model(&model.FilmSource{}).
		Where("grade = ?", model.MasterCollect).
		Update("grade", model.SlaveCollect).Error
}

// GetPosterSource 获取当前启用的优先海报图源站：
// 优先取显式标记 is_poster_source = true AND state = true 的站点；
// 若无显式开启海报源的站点，自动兜底取当前活跃的主站。
func GetPosterSource() *model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var fs model.FilmSource
	if err := db.Mdb.Where("is_poster_source = ? AND state = ?", true, true).First(&fs).Error; err == nil {
		normalizeCollectCd(&fs)
		return &fs
	}
	// 兜底：取活跃主站
	if err := db.Mdb.Where("grade = ? AND state = ?", model.MasterCollect, true).First(&fs).Error; err == nil {
		normalizeCollectCd(&fs)
		return &fs
	}
	return nil
}

// EnsureDefaultPosterSourceTx 确保全局至少有一个活跃海报图源（无外部海报源时将主站设为海报源）
func EnsureDefaultPosterSourceTx(tx *gorm.DB) error {
	if tx == nil {
		return nil
	}
	var count int64
	if err := tx.Model(&model.FilmSource{}).Where("is_poster_source = ? AND state = ?", true, true).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		var activeMasterCount int64
		if err := tx.Model(&model.FilmSource{}).Where("grade = ? AND state = ?", model.MasterCollect, true).Count(&activeMasterCount).Error; err == nil && activeMasterCount > 0 {
			return tx.Model(&model.FilmSource{}).Where("grade = ? AND state = ?", model.MasterCollect, true).Update("is_poster_source", true).Error
		}
	}
	return nil
}

// DemoteExistingPosterSourceTx 互斥单海报源：将其他站点的 is_poster_source 设为 false
func DemoteExistingPosterSourceTx(tx *gorm.DB, exceptID string) error {
	query := tx.Model(&model.FilmSource{}).Where("is_poster_source = ?", true)
	if strings.TrimSpace(exceptID) != "" {
		query = query.Where("id <> ?", exceptID)
	}
	return query.Update("is_poster_source", false).Error
}

func normalizeCollectSourceDefaults(source *model.FilmSource) {
	if source.Interval <= 0 {
		source.Interval = config.DefaultSpiderInterval
	}
	normalizeCollectCd(source)
}

// normalizeCollectCd 读取路径兜底：历史数据 cd 列可能为 0，统一按默认 24 小时处理。
func normalizeCollectCd(source *model.FilmSource) {
	if source.Cd <= 0 {
		source.Cd = 24
	}
}

// ExistCollectSourceList 查询是否已经存在站点 list 相关数据
func ExistCollectSourceList() bool {
	var count int64
	db.Mdb.Model(&model.FilmSource{}).Count(&count)
	return count > 0
}
