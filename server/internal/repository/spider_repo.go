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

// GetCollectSourceList 获取采集站 API 列表（按权重降序）
func GetCollectSourceList() []model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var list []model.FilmSource
	if err := db.Mdb.Order("weight DESC, created_at ASC, id ASC").Find(&list).Error; err != nil {
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
		now := time.Now()
		for i := range list {
			if list[i].Id == "" && list[i].Uri != "" {
				list[i].Id = utils.GenerateHashKey(list[i].Uri)
			}
			if list[i].CreatedAt.IsZero() {
				list[i].CreatedAt = now.Add(time.Duration(i) * time.Second)
			}
			normalizeCollectSourceDefaults(&list[i])
		}
		return tx.Create(&list).Error
	})
}

// PickPrimarySourceForCategory 选取用于分类树同步的首选站点（当前生效主站）。
// 规则：
// 1. 优先 is_primary = true 且 state = true 的站点；
// 2. 其次优先已启用的最高权重站点；
// 3. 再次优先最高权重站点（含未启用）；
// 4. 没有任何站点时返回 nil。
func PickPrimarySourceForCategory() *model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var primary model.FilmSource
	if err := db.Mdb.Where("is_primary = ? AND state = ?", true, true).First(&primary).Error; err == nil && primary.Id != "" {
		normalizeCollectCd(&primary)
		return &primary
	}

	var list []model.FilmSource
	if err := db.Mdb.Order("weight DESC, created_at ASC, id ASC").Find(&list).Error; err != nil || len(list) == 0 {
		return nil
	}
	for i := range list {
		if list[i].State {
			normalizeCollectCd(&list[i])
			m := list[i]
			return &m
		}
	}
	normalizeCollectCd(&list[0])
	m := list[0]
	return &m
}

func GetActiveCollectSource() *model.FilmSource {
	return PickPrimarySourceForCategory()
}

func PickMasterSourceForCategory() *model.FilmSource {
	return PickPrimarySourceForCategory()
}

// SetPrimaryCollectSource 将指定采集站设为当前生效主站，并重置其他站点
func SetPrimaryCollectSource(sourceId string) error {
	if db.Mdb == nil {
		return errors.New("database not available")
	}
	sourceId = strings.TrimSpace(sourceId)
	if sourceId == "" {
		return errors.New("采集源ID不能为空")
	}
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		var src model.FilmSource
		if err := tx.Where("id = ?", sourceId).First(&src).Error; err != nil {
			return errors.New("采集源不存在")
		}
		if err := tx.Model(&model.FilmSource{}).Where("1 = 1").Update("is_primary", false).Error; err != nil {
			return err
		}
		if err := tx.Model(&model.FilmSource{}).Where("id = ?", sourceId).Updates(map[string]any{
			"is_primary": true,
			"state":      true,
		}).Error; err != nil {
			return err
		}
		return nil
	})
}

// GetEnabledCollectSourceList 获取已启用采集站列表。
func GetEnabledCollectSourceList() []model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var list []model.FilmSource
	if err := db.Mdb.Where("state = ?", true).Order("weight DESC, created_at ASC, id ASC").Find(&list).Error; err != nil {
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
		// 2. 删除多源播放列表
		if err := tx.Where("source_id = ?", id).Delete(&model.FilmSourcePlaylist{}).Error; err != nil {
			return err
		}
		// 3. 删除海报图源数据
		if err := tx.Where("source_id = ?", id).Unscoped().Delete(&model.MoviePoster{}).Error; err != nil {
			return err
		}
		// 4. 删除来源映射
		if err := tx.Where("source_id = ?", id).Delete(&model.MovieSourceMapping{}).Error; err != nil {
			return err
		}
		// 5. 删除快照关联记录
		if err := tx.Where("source_id = ?", id).Delete(&model.FilmSnapshotSource{}).Error; err != nil {
			return err
		}
		// 6. 删除采集失败记录
		if err := DeleteFailureRecordsByOriginIdTx(tx, id); err != nil {
			return err
		}
		// 7. 删除采集站本身
		if err := tx.Where("id = ?", id).Delete(&model.FilmSource{}).Error; err != nil {
			return err
		}
		// 8. 若删除的站点是当前海报源，自动兜底将可用站点设为海报源
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
	// 基于 URI 生成稳定的哈希 ID，确保服务重启后采集源顺序一致
	if s.Id == "" {
		s.Id = utils.GenerateHashKey(s.Uri)
	}
	if s.CreatedAt.IsZero() {
		s.CreatedAt = time.Now()
	}
	// 若无外部指定且无活跃海报源，默认作为海报源
	if !s.IsPosterSource {
		var activePosterCount int64
		if err := tx.Model(&model.FilmSource{}).Where("is_poster_source = ? AND state = ?", true, true).Count(&activePosterCount).Error; err == nil && activePosterCount == 0 {
			s.IsPosterSource = true
			log.Printf("[Spider] 无活跃海报源，自动设为默认海报图源: id=%s name=%s", s.Id, s.Name)
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
	now := time.Now()
	// 为没有 ID 的采集源生成稳定的哈希 ID
	for i := range list {
		if list[i].Id == "" {
			list[i].Id = utils.GenerateHashKey(list[i].Uri)
		}
		if list[i].CreatedAt.IsZero() {
			list[i].CreatedAt = now.Add(time.Duration(i) * time.Second)
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
	if s.CreatedAt.IsZero() {
		var existing model.FilmSource
		if err := tx.Select("created_at").Where("id = ?", s.Id).First(&existing).Error; err == nil && !existing.CreatedAt.IsZero() {
			s.CreatedAt = existing.CreatedAt
		}
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
	// 全站平权架构下不再区分主从站点，保留空实现兼容旧调用
	return nil
}

// GetPosterSource 获取当前启用的优先海报图源站：
// 优先取显式标记 is_poster_source = true AND state = true 的站点；
// 若无显式开启海报源的站点，自动兜底取当前活跃的最高权重站点。
func GetPosterSource() *model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var fs model.FilmSource
	if err := db.Mdb.Where("is_poster_source = ? AND state = ?", true, true).First(&fs).Error; err == nil {
		normalizeCollectCd(&fs)
		return &fs
	}
	// 兜底：取活跃最高权重站点
	if err := db.Mdb.Where("state = ?", true).Order("weight DESC, created_at ASC, id ASC").First(&fs).Error; err == nil {
		normalizeCollectCd(&fs)
		return &fs
	}
	return nil
}

// EnsureDefaultPosterSourceTx 确保全局至少有一个活跃海报图源（无外部海报源时将最高权重活跃站点设为海报源）
func EnsureDefaultPosterSourceTx(tx *gorm.DB) error {
	if tx == nil {
		return nil
	}
	var count int64
	if err := tx.Model(&model.FilmSource{}).Where("is_poster_source = ? AND state = ?", true, true).Count(&count).Error; err != nil {
		return err
	}
	if count == 0 {
		var topSource model.FilmSource
		if err := tx.Model(&model.FilmSource{}).Where("state = ?", true).Order("weight DESC, created_at ASC, id ASC").First(&topSource).Error; err == nil && topSource.Id != "" {
			return tx.Model(&model.FilmSource{}).Where("id = ?", topSource.Id).Update("is_poster_source", true).Error
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
	if source.Format == "" {
		source.Format = model.SourceFormatJSON
	}
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
