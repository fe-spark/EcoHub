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

// GetCollectSourceList 获取采集站 API 列表（按排序序号升序）
func GetCollectSourceList() ([]model.FilmSource, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	list := make([]model.FilmSource, 0)
	if err := db.Mdb.Order("sort ASC, created_at ASC, id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// ReplaceCollectSources 用备份列表整体替换采集站（事务清空后写入）
func ReplaceCollectSources(list []model.FilmSource) error {
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
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
	if err != nil {
		return err
	}
	return RefreshCollectSourceCatalog()
}

// PickPrimarySourceForCategory 选取用于分类树同步的首选站点（当前基准源）。
// 统一原则：首位即基准（Sort-as-Master）。
// 仅从已启用的站点中按顺序选取首位（sort ASC, created_at ASC）；
// 若所有站点均停用或无站点，返回 nil，严禁无视停用开关进行静默兜底。
func PickPrimarySourceForCategory() *model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var primary model.FilmSource
	if err := db.Mdb.Where("state = ?", true).Order("sort ASC, created_at ASC, id ASC").First(&primary).Error; err == nil && primary.Id != "" {
		return &primary
	}
	return nil
}

func GetActiveCollectSource() *model.FilmSource {
	return PickPrimarySourceForCategory()
}

// GetEnabledCollectSourceList 获取已启用采集站列表。
func GetEnabledCollectSourceList() ([]model.FilmSource, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	list := make([]model.FilmSource, 0)
	if err := db.Mdb.Where("state = ?", true).Order("sort ASC, created_at ASC, id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

func GetLastCollectTime(sourceID string) (*time.Time, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil, nil
	}
	stats, err := GetCollectSourceStats([]string{sourceID})
	if err != nil {
		return nil, err
	}
	return stats[sourceID], nil
}

func GetCollectSourceStats(sourceIDs []string) (map[string]*time.Time, error) {
	result := make(map[string]*time.Time, len(sourceIDs))
	if len(sourceIDs) == 0 {
		return result, nil
	}
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	var rows []model.CollectSourceStats
	if err := db.Mdb.Where("source_id IN ?", sourceIDs).Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		if row.LastCollectTime == nil || row.LastCollectTime.IsZero() {
			continue
		}
		value := *row.LastCollectTime
		result[row.SourceId] = &value
	}
	return result, nil
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
	return &fs
}

// CascadeCleanSourceDataTx 删除该站自己的档案和挂载数据。有没有播放线路都删。
// 返回被删除的档案 mid。其他站的档案不删。
func CascadeCleanSourceDataTx(tx *gorm.DB, id string) ([]int64, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, nil
	}
	var owned []int64
	if err := tx.Model(&model.FilmIndex{}).Where("first_source_id = ?", id).Pluck("mid", &owned).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("source_id = ?", id).Delete(&model.FilmSourcePlaylist{}).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("source_id = ?", id).Delete(&model.MovieSourceMapping{}).Error; err != nil {
		return nil, err
	}
	if err := deleteOwnedFilmsTx(tx, owned); err != nil {
		return nil, err
	}
	return owned, nil
}

func deleteOwnedFilmsTx(tx *gorm.DB, mids []int64) error {
	for i := 0; i < len(mids); i += 1000 {
		if err := deleteOwnedFilmChunk(tx, mids[i:min(i+1000, len(mids))]); err != nil {
			return err
		}
	}
	return nil
}

// DelCollectResource 删除采集站。档案、失败记录、分类和站点行在同一个事务里提交。
func DelCollectResource(id string) ([]int64, error) {
	if db.Mdb == nil {
		return nil, errors.New("database not available")
	}
	var orphanMids []int64
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		var err error
		orphanMids, err = cascadeCleanSourceDataTx(tx, id)
		if err != nil {
			return err
		}
		if err := tx.Where("source_id = ?", id).Delete(&model.CronSourceRel{}).Error; err != nil {
			return err
		}
		if err := DeleteFailureRecordsByOriginIdTx(tx, id); err != nil {
			return err
		}
		if err := ClearSourceCategoryDataTx(tx, id); err != nil {
			return err
		}
		if err := tx.Where("id = ?", id).Delete(&model.FilmSource{}).Error; err != nil {
			return err
		}
		return EnsureDefaultPosterSourceTx(tx)
	})
	if err != nil {
		return nil, err
	}
	if err := RefreshCollectSourceCatalog(); err != nil {
		forgetCollectSource(id)
		return orphanMids, err
	}
	return orphanMids, nil
}

// UpdateCollectSourceReplacingData 更换地址或协议时，清数据和更新配置在同一个事务里提交。
func UpdateCollectSourceReplacingData(source model.FilmSource, cleanData, cleanFailures bool) ([]int64, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	var owned []int64
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if cleanData {
			var err error
			owned, err = cascadeCleanSourceDataTx(tx, source.Id)
			if err != nil {
				return err
			}
			if err := ClearSourceCategoryDataTx(tx, source.Id); err != nil {
				return err
			}
		}
		if cleanFailures {
			if err := DeleteFailureRecordsByOriginIdTx(tx, source.Id); err != nil {
				return err
			}
		}
		return UpdateCollectSourceTx(tx, source)
	})
	if err != nil {
		return nil, err
	}
	if err := RefreshCollectSourceCatalog(); err != nil {
		return owned, err
	}
	return owned, nil
}

// CleanCollectSourceData 清空该站已采集数据（保留采集站配置本身）。
func CleanCollectSourceData(id string) ([]int64, error) {
	orphanMids, err := CascadeCleanSourceData(id)
	if err != nil {
		return orphanMids, err
	}
	if err := DeleteFailureRecordsByOriginIdTx(db.Mdb, id); err != nil {
		return orphanMids, err
	}
	return orphanMids, nil
}

// AddCollectSource 添加采集站信息
func AddCollectSource(s model.FilmSource) error {
	if err := AddCollectSourceTx(db.Mdb, s); err != nil {
		return err
	}
	return RefreshCollectSourceCatalog()
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
	// 新增采集站默认排在末尾
	if s.Sort <= 0 {
		s.Sort = GetCollectSourceMaxSortTx(tx) + 1
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

// GetCollectSourceMaxSortTx 获取当前采集站的最大排序序号
func GetCollectSourceMaxSortTx(tx *gorm.DB) int {
	if tx == nil {
		return -1
	}
	var maxSort int
	row := tx.Model(&model.FilmSource{}).Select("COALESCE(MAX(sort), -1)").Row()
	_ = row.Scan(&maxSort)
	return maxSort
}

// SortCollectSources 批量按传入的 ID 顺序更新采集站排序序号 (0, 1, 2, ...)
func SortCollectSources(ids []string) error {
	if db.Mdb == nil {
		return errors.New("database not available")
	}
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		for index, id := range ids {
			id = strings.TrimSpace(id)
			if id == "" {
				continue
			}
			if err := tx.Model(&model.FilmSource{}).Where("id = ?", id).Update("sort", index).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return RefreshCollectSourceCatalog()
}

// BatchAddCollectSource 批量添加采集站信息
func BatchAddCollectSource(list []model.FilmSource) error {
	now := time.Now()
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		nextSort := GetCollectSourceMaxSortTx(tx) + 1
		for i := range list {
			if list[i].Id == "" {
				list[i].Id = utils.GenerateHashKey(list[i].Uri)
			}
			if list[i].CreatedAt.IsZero() {
				list[i].CreatedAt = now.Add(time.Duration(i) * time.Second)
			}
			if list[i].Sort <= 0 {
				list[i].Sort = nextSort
				nextSort++
			}
			normalizeCollectSourceDefaults(&list[i])
		}
		return tx.Create(&list).Error
	})
	if err != nil {
		return err
	}
	return RefreshCollectSourceCatalog()
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

// EnsureDefaultPosterSourceTx 确保全局至少有一个活跃海报图源（无外部海报源时将活跃首位站点设为海报源）
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
		if err := tx.Model(&model.FilmSource{}).Where("state = ?", true).Order("sort ASC, created_at ASC, id ASC").First(&topSource).Error; err == nil && topSource.Id != "" {
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

func normalizeCollectCd(source *model.FilmSource) {
	if source.Cd <= 0 {
		source.Cd = 24
	}
}

// ExistCollectSourceList 查询是否已经存在站点 list 相关数据。
func ExistCollectSourceList() (bool, error) {
	if db.Mdb == nil {
		return false, errors.New("数据库未连接")
	}
	var count int64
	if err := db.Mdb.Model(&model.FilmSource{}).Count(&count).Error; err != nil {
		return false, err
	}
	return count > 0, nil
}
