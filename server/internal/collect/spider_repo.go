package collect

import (
	"errors"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
	"log"
	"server/internal/collect/engine"
	"server/internal/collect/ledger"
	"server/internal/film/catalog"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
	"server/internal/utils"
	"strings"
	"time"
)

// --------- Collect Source -----------

// GetCollectSourceList 获取采集站 API 列表（按排序序号升序）
func GetCollectSourceList() ([]model.FilmSource, error) {
	return sourcecatalog.GetCollectSourceListDB(db.Mdb)
}

// ReplaceCollectSources 用备份列表整体替换采集站（事务清空后写入）
func ReplaceCollectSources(list []model.FilmSource) error {
	next := append([]model.FilmSource(nil), list...)
	now := time.Now()
	for i := range next {
		next[i].Id = strings.TrimSpace(next[i].Id)
		next[i].Uri = strings.TrimSpace(next[i].Uri)
		if next[i].Id == "" && next[i].Uri != "" {
			next[i].Id = utils.GenerateHashKey(next[i].Uri)
		}
		if next[i].CreatedAt.IsZero() {
			next[i].CreatedAt = now.Add(time.Duration(i) * time.Second)
		}
		normalizeCollectSourceDefaults(&next[i])
	}

	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		var existingSources []model.FilmSource
		if tx.Migrator().HasColumn(&model.FilmSource{}, "home_recommend_inited") {
			if err := tx.Select("id", "uri", "home_recommend_inited").Find(&existingSources).Error; err != nil {
				return err
			}
		}
		initedURIByID := make(map[string]string, len(existingSources))
		for _, src := range existingSources {
			if src.HomeRecommendInited {
				initedURIByID[src.Id] = src.Uri
			}
		}
		for i := range next {
			if oldURI, ok := initedURIByID[next[i].Id]; ok && oldURI == next[i].Uri {
				next[i].HomeRecommendInited = true
			}
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.FilmSource{}).Error; err != nil {
			return err
		}
		if len(next) == 0 {
			if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.BannerSlot{}).Error; err != nil {
				return err
			}
			if tx.Migrator().HasTable(&model.HomeRecommendItem{}) {
				if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.HomeRecommendItem{}).Error; err != nil {
					return err
				}
			}
			if tx.Migrator().HasTable(&model.HomeRecommendGroup{}) {
				if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.HomeRecommendGroup{}).Error; err != nil {
					return err
				}
			}
			return nil
		}
		// 不在新列表里的站点，它们的轮播与首页推荐一并删除。
		ids := make([]string, 0, len(next))
		for _, s := range next {
			ids = append(ids, s.Id)
		}
		if err := tx.Where("source_id NOT IN ?", ids).Delete(&model.BannerSlot{}).Error; err != nil {
			return err
		}
		if tx.Migrator().HasTable(&model.HomeRecommendItem{}) {
			if err := tx.Where("source_id NOT IN ?", ids).Delete(&model.HomeRecommendItem{}).Error; err != nil {
				return err
			}
		}
		if tx.Migrator().HasTable(&model.HomeRecommendGroup{}) {
			if err := tx.Where("source_id NOT IN ?", ids).Delete(&model.HomeRecommendGroup{}).Error; err != nil {
				return err
			}
		}
		return insertFilmSources(tx, next)
	})
	if err != nil {
		return err
	}
	// 备份导入整体替换站点：存储的主站不在新列表时清空，由懒初始化按新列表重选
	sourcecatalog.ReconcilePrimaryAfterSourceReplace(next)
	catalog.InvalidatePublicSourceCache()
	return sourcecatalog.RefreshCollectSourceCatalog()
}

// GetLastCollectTime 获取指定站点的最近采集时间。
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

// DelCollectResource 删除采集站。
// 重型数据按短事务分批清理避免长事务锁库，最后在同一事务里收口分类、历史批次与站点配置行。
func DelCollectResource(id string) ([]int64, error) {
	if db.Mdb == nil {
		return nil, errors.New("database not available")
	}
	_ = catalog.EnsureFilmSourceDeletedIndex()
	orphanMids, err := catalog.CleanSourceDataBatched(id)
	if err != nil {
		return orphanMids, err
	}
	err = PauseActiveBatchRecords(id, func() error {
		return db.Mdb.Transaction(func(tx *gorm.DB) error {
			if err := catalog.ClearSourceCategoryDataTx(tx, id); err != nil {
				return err
			}
			if err := sourcecatalog.DeleteCollectSourceStatsTx(tx, id); err != nil {
				return err
			}
			if err := catalog.AssertSourceDataCleanedTx(tx, id); err != nil {
				return err
			}
			if err := tx.Where("id = ?", id).Delete(&model.FilmSource{}).Error; err != nil {
				return err
			}
			return EnsureDefaultPosterSourceTx(tx)
		})
	})
	if err != nil {
		return orphanMids, err
	}
	purgeErr := ledger.PurgeSourceCollectHistoryBatched(id)
	taxonomy.TouchRuleVersion()
	taxonomy.ReloadMappingRules()
	catalog.InvalidatePublicSourceCache(id)
	if err := sourcecatalog.RefreshCollectSourceCatalog(); err != nil {
		sourcecatalog.ForgetCollectSource(id)
		if purgeErr != nil {
			return orphanMids, errors.Join(purgeErr, err)
		}
		return orphanMids, err
	}
	return orphanMids, purgeErr
}

// UpdateCollectSourceUpdatingState 更新站点配置；换地址时同步清理该站分类，重型影片清理已提前分批完成。
func UpdateCollectSourceReplacingData(source model.FilmSource, clearCategories bool) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if clearCategories {
			if err := catalog.ClearSourceCategoryDataTx(tx, source.Id); err != nil {
				return err
			}
		}
		if err := UpdateCollectSourceTx(tx, source); err != nil {
			return err
		}
		if clearCategories && tx.Migrator().HasColumn(&model.FilmSource{}, "home_recommend_inited") {
			if err := tx.Model(&model.FilmSource{}).
				Where("id = ?", source.Id).
				Update("home_recommend_inited", false).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err != nil {
		return err
	}
	return sourcecatalog.RefreshCollectSourceCatalog()
}

// CleanCollectSourceData 清空该站已采集数据（保留采集站配置本身）。
func CleanCollectSourceData(id string) ([]int64, error) {
	return catalog.CascadeCleanSourceData(id)
}

// AddCollectSource 添加采集站信息
func AddCollectSource(s model.FilmSource) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		return AddCollectSourceTx(tx, s)
	}); err != nil {
		return err
	}
	return sourcecatalog.RefreshCollectSourceCatalog()
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
	if err := insertFilmSources(tx, []model.FilmSource{s}); err != nil {
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
	return sourcecatalog.RefreshCollectSourceCatalog()
}

// BatchUpdateCollectSourceState 批量更新采集源启用状态（原子事务提交）
func BatchUpdateCollectSourceState(ids []string, state bool) error {
	if db.Mdb == nil {
		return errors.New("database not available")
	}
	if len(ids) == 0 {
		return nil
	}
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		return tx.Model(&model.FilmSource{}).Where("id IN ?", ids).Update("state", state).Error
	})
	if err != nil {
		return err
	}
	return sourcecatalog.RefreshCollectSourceCatalog()
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
		return insertFilmSources(tx, list)
	})
	if err != nil {
		return err
	}
	return sourcecatalog.RefreshCollectSourceCatalog()
}

// ValidateCollectSourceUpdate 在执行破坏性清库或写库前预检采集站更新的唯一性等约束。
func ValidateCollectSourceUpdate(s model.FilmSource) error {
	if db.Mdb == nil {
		return errors.New("database not available")
	}
	var count int64
	if err := db.Mdb.Model(&model.FilmSource{}).Where("id != ? AND uri = ?", s.Id, s.Uri).Count(&count).Error; err != nil {
		return err
	}
	if count > 0 {
		return errors.New("当前采集站链接已存在其他站点中，请勿重复添加")
	}
	return nil
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
	if err := tx.Omit("home_recommend_inited").Save(&s).Error; err != nil {
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

// insertFilmSources 写入采集站。
func insertFilmSources(tx *gorm.DB, list []model.FilmSource) error {
	if len(list) == 0 {
		return nil
	}
	return tx.Create(&list).Error
}

func normalizeCollectSourceDefaults(source *model.FilmSource) {
	engine.NormalizeSourceDefaults(source)
}

// EnsureDefaultPosterSource 保证存在海报源站。
func EnsureDefaultPosterSource() error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	if err := EnsureDefaultPosterSourceTx(db.Mdb); err != nil {
		return err
	}
	return sourcecatalog.RefreshCollectSourceCatalog()
}

// UpdateCollectSource 更新采集站。
func UpdateCollectSource(s model.FilmSource) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		return UpdateCollectSourceTx(tx, s)
	}); err != nil {
		return err
	}
	return sourcecatalog.RefreshCollectSourceCatalog()
}
