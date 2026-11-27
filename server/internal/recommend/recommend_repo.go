package recommend

import (
	"context"
	"errors"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"

	"github.com/google/uuid"
	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func lockRecommendGroupTx(tx *gorm.DB, id string) (model.HomeRecommendGroup, error) {
	var g model.HomeRecommendGroup
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Where("id = ?", strings.TrimSpace(id)).
		First(&g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.HomeRecommendGroup{}, ErrRecommendGroupNotFound
	}
	return g, err
}

func lockRecommendSourceTx(tx *gorm.DB, sourceID string) error {
	var src model.FilmSource
	return tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").
		Where("id = ?", strings.TrimSpace(sourceID)).
		First(&src).Error
}

var (
	ErrRecommendGroupNotFound = errors.New("推荐分组不存在")
	ErrRecommendItemNotFound  = errors.New("推荐影片不存在")
	ErrRecommendDuplicateFilm = errors.New("该影片已在本分组中")
)

func recommendDB() (*gorm.DB, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	return db.Mdb, nil
}

// ListHomeRecommendGroups 查询指定采集站的全部推荐分组（按 sort 升序）。
func ListHomeRecommendGroups(ctx context.Context, sourceID string) ([]model.HomeRecommendGroup, error) {
	gdb, err := recommendDB()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	list := make([]model.HomeRecommendGroup, 0)
	err = gdb.WithContext(ctx).
		Where("source_id = ?", strings.TrimSpace(sourceID)).
		Order("sort ASC, id ASC").
		Find(&list).Error
	return list, err
}

// ListEnabledHomeRecommendGroups 查询指定采集站已启用的推荐分组。
func ListEnabledHomeRecommendGroups(ctx context.Context, sourceID string) ([]model.HomeRecommendGroup, error) {
	gdb, err := recommendDB()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	list := make([]model.HomeRecommendGroup, 0)
	err = gdb.WithContext(ctx).
		Where("source_id = ? AND enabled = ?", strings.TrimSpace(sourceID), true).
		Order("sort ASC, id ASC").
		Find(&list).Error
	return list, err
}

// ListAutoSyncRecommendGroups 查询所有开启自动跟随豆瓣榜单的分组。
func ListAutoSyncRecommendGroups(ctx context.Context) ([]model.HomeRecommendGroup, error) {
	gdb, err := recommendDB()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	list := make([]model.HomeRecommendGroup, 0)
	err = gdb.WithContext(ctx).
		Where("auto_sync = ? AND sync_chart_key <> ?", true, "").
		Order("source_id ASC, sort ASC, id ASC").
		Find(&list).Error
	return list, err
}

// GetHomeRecommendGroup 获取单个推荐分组。
func GetHomeRecommendGroup(ctx context.Context, id string) (model.HomeRecommendGroup, error) {
	gdb, err := recommendDB()
	if err != nil {
		return model.HomeRecommendGroup{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var g model.HomeRecommendGroup
	err = gdb.WithContext(ctx).Where("id = ?", strings.TrimSpace(id)).First(&g).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.HomeRecommendGroup{}, ErrRecommendGroupNotFound
	}
	return g, err
}

// CreateHomeRecommendGroup 新建推荐分组（站行锁事务内计算排位，防并发序号冲突）。
func CreateHomeRecommendGroup(ctx context.Context, g model.HomeRecommendGroup) (model.HomeRecommendGroup, error) {
	gdb, err := recommendDB()
	if err != nil {
		return model.HomeRecommendGroup{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	g.Id = uuid.NewString()
	g.SourceId = strings.TrimSpace(g.SourceId)
	g.Title = strings.TrimSpace(g.Title)
	if g.SyncLimit <= 0 {
		g.SyncLimit = 12
	}
	if err := gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRecommendSourceTx(tx, g.SourceId); err != nil {
			return err
		}
		var maxSort int64
		_ = tx.Model(&model.HomeRecommendGroup{}).
			Where("source_id = ?", g.SourceId).
			Select("COALESCE(MAX(sort), 0)").Scan(&maxSort).Error
		if g.Sort <= 0 {
			g.Sort = maxSort + 1
		}
		return tx.Create(&g).Error
	}); err != nil {
		return model.HomeRecommendGroup{}, err
	}
	repobase.BumpIndexPageGeneration()
	return g, nil
}

// UpdateHomeRecommendGroupAtomic 在 FOR UPDATE 事务内校验、修改分组并在模式切换时同事务清理旧条目，防止丢失更新。
func UpdateHomeRecommendGroupAtomic(
	ctx context.Context,
	id string,
	mutate func(existing model.HomeRecommendGroup) (updated model.HomeRecommendGroup, clearItems bool, syncNow bool, err error),
) (updatedGroup model.HomeRecommendGroup, needSync bool, err error) {
	gdb, err := recommendDB()
	if err != nil {
		return model.HomeRecommendGroup{}, false, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	id = strings.TrimSpace(id)
	err = gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		existing, lockErr := lockRecommendGroupTx(tx, id)
		if lockErr != nil {
			return lockErr
		}
		next, clearItems, syncNow, mErr := mutate(existing)
		if mErr != nil {
			return mErr
		}
		if next.SyncLimit <= 0 {
			next.SyncLimit = 12
		}
		res := tx.Model(&model.HomeRecommendGroup{}).
			Where("id = ?", id).
			Updates(map[string]any{
				"title":          strings.TrimSpace(next.Title),
				"enabled":        next.Enabled,
				"auto_sync":      next.AutoSync,
				"sync_chart_key": strings.TrimSpace(next.SyncChartKey),
				"category_id":    next.CategoryId,
				"sync_limit":     next.SyncLimit,
				"updated_at":     time.Now(),
			})
		if res.Error != nil {
			return res.Error
		}
		if clearItems {
			if err := tx.Where("group_id = ?", id).Delete(&model.HomeRecommendItem{}).Error; err != nil {
				return err
			}
		}
		updatedGroup = next
		needSync = syncNow
		return nil
	})
	if err == nil {
		repobase.BumpIndexPageGeneration()
	}
	return updatedGroup, needSync, err
}

// DeleteHomeRecommendGroup 删除分组及其下全部推荐影片。
func DeleteHomeRecommendGroup(ctx context.Context, id string) error {
	gdb, err := recommendDB()
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	id = strings.TrimSpace(id)
	err = gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if _, err := lockRecommendGroupTx(tx, id); err != nil {
			return err
		}
		if err := tx.Where("group_id = ?", id).Delete(&model.HomeRecommendItem{}).Error; err != nil {
			return err
		}
		res := tx.Where("id = ?", id).Delete(&model.HomeRecommendGroup{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrRecommendGroupNotFound
		}
		return nil
	})
	if err == nil {
		repobase.BumpIndexPageGeneration()
	}
	return err
}

// ReorderHomeRecommendGroups 批量更新指定站点下推荐分组顺序（站锁下校验完整 ID 集合）。
func ReorderHomeRecommendGroups(ctx context.Context, sourceID string, orderedIDs []string) error {
	gdb, err := recommendDB()
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sourceID = strings.TrimSpace(sourceID)
	err = gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRecommendSourceTx(tx, sourceID); err != nil {
			return err
		}
		var currentIDs []string
		if err := tx.Model(&model.HomeRecommendGroup{}).Where("source_id = ?", sourceID).Pluck("id", &currentIDs).Error; err != nil {
			return err
		}
		allowed := make(map[string]struct{}, len(currentIDs))
		for _, id := range currentIDs {
			allowed[id] = struct{}{}
		}
		seen := make(map[string]struct{}, len(orderedIDs))
		cleanOrdered := make([]string, 0, len(orderedIDs))
		for _, raw := range orderedIDs {
			id := strings.TrimSpace(raw)
			if _, ok := allowed[id]; !ok {
				return errors.New("分组列表包含不属于当前站点的分组")
			}
			if _, dup := seen[id]; dup {
				return errors.New("分组排序列表存在重复分组")
			}
			seen[id] = struct{}{}
			cleanOrdered = append(cleanOrdered, id)
		}
		if len(cleanOrdered) != len(allowed) {
			return errors.New("分组排序列表不完整，请刷新后重试")
		}
		for idx, id := range cleanOrdered {
			if err := tx.Model(&model.HomeRecommendGroup{}).
				Where("id = ? AND source_id = ?", id, sourceID).
				Update("sort", int64(idx+1)).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		repobase.BumpIndexPageGeneration()
	}
	return err
}

// ListHomeRecommendItemsBySource 查询某采集站下全部推荐条目。
func ListHomeRecommendItemsBySource(ctx context.Context, sourceID string) ([]model.HomeRecommendItem, error) {
	gdb, err := recommendDB()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	list := make([]model.HomeRecommendItem, 0)
	err = gdb.WithContext(ctx).
		Where("source_id = ?", strings.TrimSpace(sourceID)).
		Order("group_id ASC, sort ASC, id ASC").
		Find(&list).Error
	return list, err
}

// AddHomeRecommendItem 向指定分组追加影片（先锁父分组防删除竞态与死锁）。
func AddHomeRecommendItem(ctx context.Context, groupID, sourceID string, mid int64, doubanScore string) (model.HomeRecommendItem, error) {
	gdb, err := recommendDB()
	if err != nil {
		return model.HomeRecommendItem{}, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	groupID = strings.TrimSpace(groupID)
	sourceID = strings.TrimSpace(sourceID)

	var item model.HomeRecommendItem
	err = gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		lockedGroup, err := lockRecommendGroupTx(tx, groupID)
		if err != nil {
			return err
		}
		if lockedGroup.CategoryId > 0 {
			return errors.New("站点分类分组由片库分类实时驱动，不支持手工添加影片")
		}
		var count int64
		if err := tx.Model(&model.HomeRecommendItem{}).
			Where("group_id = ? AND mid = ?", groupID, mid).Count(&count).Error; err != nil {
			return err
		}
		if count > 0 {
			return ErrRecommendDuplicateFilm
		}
		var maxSort int64
		_ = tx.Model(&model.HomeRecommendItem{}).
			Where("group_id = ?", groupID).
			Select("COALESCE(MAX(sort), 0)").Scan(&maxSort).Error

		item = model.HomeRecommendItem{
			Id:          uuid.NewString(),
			GroupId:     groupID,
			SourceId:    sourceID,
			Mid:         mid,
			Sort:        maxSort + 1,
			DoubanScore: strings.TrimSpace(doubanScore),
		}
		if err := tx.Create(&item).Error; err != nil {
			if db.IsDuplicateKeyError(err) {
				return ErrRecommendDuplicateFilm
			}
			return err
		}
		return nil
	})
	if err != nil {
		return model.HomeRecommendItem{}, err
	}
	repobase.BumpIndexPageGeneration()
	return item, nil
}

// ReorderHomeRecommendItems 更新分组内影片顺序。
func ReorderHomeRecommendItems(ctx context.Context, groupID string, orderedItemIDs []string) error {
	gdb, err := recommendDB()
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	groupID = strings.TrimSpace(groupID)
	err = gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		lockedGroup, err := lockRecommendGroupTx(tx, groupID)
		if err != nil {
			return err
		}
		if lockedGroup.CategoryId > 0 {
			return errors.New("站点分类分组由片库分类实时驱动，不支持手工添加影片")
		}
		for idx, itemID := range orderedItemIDs {
			if err := tx.Model(&model.HomeRecommendItem{}).
				Where("id = ? AND group_id = ?", strings.TrimSpace(itemID), groupID).
				Update("sort", int64(idx+1)).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		repobase.BumpIndexPageGeneration()
	}
	return err
}

// DeleteHomeRecommendItem 删除单条推荐影片，并返回该条目所属采集站 ID。
func DeleteHomeRecommendItem(ctx context.Context, itemID string) (string, error) {
	gdb, err := recommendDB()
	if err != nil {
		return "", err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var existing model.HomeRecommendItem
	if err := gdb.WithContext(ctx).Where("id = ?", strings.TrimSpace(itemID)).First(&existing).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return "", ErrRecommendItemNotFound
		}
		return "", err
	}
	res := gdb.WithContext(ctx).Where("id = ?", existing.Id).Delete(&model.HomeRecommendItem{})
	if res.Error != nil {
		return "", res.Error
	}
	if res.RowsAffected == 0 {
		return "", ErrRecommendItemNotFound
	}
	repobase.BumpIndexPageGeneration()
	return existing.SourceId, nil
}

// ClearHomeRecommendGroupItems 清空指定分组的影片。
func ClearHomeRecommendGroupItems(ctx context.Context, groupID string) error {
	gdb, err := recommendDB()
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	groupID = strings.TrimSpace(groupID)
	err = gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		lockedGroup, err := lockRecommendGroupTx(tx, groupID)
		if err != nil {
			return err
		}
		if lockedGroup.CategoryId > 0 {
			return errors.New("站点分类分组由片库分类实时驱动，不支持手工添加影片")
		}
		return tx.Where("group_id = ?", groupID).Delete(&model.HomeRecommendItem{}).Error
	})
	if err != nil {
		return err
	}
	repobase.BumpIndexPageGeneration()
	return nil
}
