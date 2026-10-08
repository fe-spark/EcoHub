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
func GetCollectSourceList() []model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var list []model.FilmSource
	if err := db.Mdb.Order("sort ASC, created_at ASC, id ASC").Find(&list).Error; err != nil {
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
// PickPrimarySourceForCategory 选取用于分类树同步的首选站点（当前基准源）。
// 统一原则：首位即基准（Sort-as-Master）。
// 1. 优先取当前排在首位且已启用的站点（sort ASC, created_at ASC）；
// 2. 兜底：若所有站点均停用，取排在首位的站点；
// 3. 没有任何站点时返回 nil。
func PickPrimarySourceForCategory() *model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var primary model.FilmSource
	if err := db.Mdb.Where("state = ?", true).Order("sort ASC, created_at ASC, id ASC").First(&primary).Error; err == nil && primary.Id != "" {
		normalizeCollectCd(&primary)
		return &primary
	}
	if err := db.Mdb.Order("sort ASC, created_at ASC, id ASC").First(&primary).Error; err == nil && primary.Id != "" {
		normalizeCollectCd(&primary)
		return &primary
	}
	return nil
}

func GetActiveCollectSource() *model.FilmSource {
	return PickPrimarySourceForCategory()
}

func PickMasterSourceForCategory() *model.FilmSource {
	return PickPrimarySourceForCategory()
}

// SetPrimaryCollectSource 全链路首位即基准原则下已废除，保留空实现兼容
func SetPrimaryCollectSource(sourceId string) error {
	return nil
}

// GetEnabledCollectSourceList 获取已启用采集站列表。
func GetEnabledCollectSourceList() []model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var list []model.FilmSource
	if err := db.Mdb.Where("state = ?", true).Order("sort ASC, created_at ASC, id ASC").Find(&list).Error; err != nil {
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

// CascadeCleanSourceDataTx 在事务中清理指定采集站点的附属数据，并同步清理该源独占的孤儿影片实体。
// 返回被级联清理的独占影片 mid 列表。
func CascadeCleanSourceDataTx(tx *gorm.DB, id string) ([]int64, error) {
	var affectedMids []int64
	_ = tx.Model(&model.FilmSourcePlaylist{}).Where("source_id = ?", id).Pluck("DISTINCT mid", &affectedMids).Error

	if err := tx.Where("source_id = ?", id).Delete(&model.FilmSourcePlaylist{}).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("source_id = ?", id).Unscoped().Delete(&model.MoviePoster{}).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("source_id = ?", id).Delete(&model.MovieSourceMapping{}).Error; err != nil {
		return nil, err
	}
	if err := tx.Where("source_id = ?", id).Delete(&model.FilmSnapshotSource{}).Error; err != nil {
		return nil, err
	}

	var orphanMids []int64
	if len(affectedMids) > 0 {
		_ = tx.Model(&model.FilmIndex{}).
			Where("mid IN (?) AND mid NOT IN (?)",
				affectedMids,
				tx.Model(&model.FilmSourcePlaylist{}).Where("line_kind = 'play'").Select("mid"),
			).
			Pluck("mid", &orphanMids).Error
	}

	if len(orphanMids) > 0 {
		if err := tx.Where("mid IN ?", orphanMids).Delete(&model.FilmIndex{}).Error; err != nil {
			return nil, err
		}
		if err := tx.Where("mid IN ?", orphanMids).Delete(&model.MovieMatchKey{}).Error; err != nil {
			return nil, err
		}
		if err := tx.Where("global_mid IN ?", orphanMids).Delete(&model.MovieSourceMapping{}).Error; err != nil {
			return nil, err
		}
		if err := tx.Where("mid IN ?", orphanMids).Delete(&model.Banner{}).Error; err != nil {
			return nil, err
		}
	}

	return orphanMids, nil
}

// DelCollectResource 通过 Id 删除对应的采集站点信息及其附属采集数据，并级联清理独占孤儿影片
func DelCollectResource(id string) ([]int64, error) {
	var orphanMids []int64
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		// 1. 删除关联的定时任务关系
		if err := tx.Where("source_id = ?", id).Delete(&model.CronSourceRel{}).Error; err != nil {
			return err
		}
		// 2. 清理源采集数据及独占孤儿影片
		orphans, err := CascadeCleanSourceDataTx(tx, id)
		if err != nil {
			return err
		}
		orphanMids = orphans
		// 3. 删除采集失败记录
		if err := DeleteFailureRecordsByOriginIdTx(tx, id); err != nil {
			return err
		}
		// 4. 删除采集站本身
		if err := tx.Where("id = ?", id).Delete(&model.FilmSource{}).Error; err != nil {
			return err
		}
		// 5. 若删除的站点是当前海报源，自动兜底将可用站点设为海报源
		return EnsureDefaultPosterSourceTx(tx)
	})
	if err != nil {
		return nil, err
	}
	return orphanMids, nil
}

// CleanCollectSourceData 清空指定采集源的历史采集数据并级联清理独占孤儿影片（保留采集站点配置本身）
func CleanCollectSourceData(id string) ([]int64, error) {
	var orphanMids []int64
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		orphans, err := CascadeCleanSourceDataTx(tx, id)
		if err != nil {
			return err
		}
		orphanMids = orphans
		return DeleteFailureRecordsByOriginIdTx(tx, id)
	})
	if err != nil {
		return nil, err
	}
	return orphanMids, nil
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
	return db.Mdb.Transaction(func(tx *gorm.DB) error {
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
	// 兜底：取活跃首位站点
	if err := db.Mdb.Where("state = ?", true).Order("sort ASC, created_at ASC, id ASC").First(&fs).Error; err == nil {
		normalizeCollectCd(&fs)
		return &fs
	}
	return nil
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
