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
)

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

// CreateHomeRecommendGroup 新建推荐分组（自动排在末尾）。
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
	var maxSort int64
	_ = gdb.WithContext(ctx).Model(&model.HomeRecommendGroup{}).
		Where("source_id = ?", g.SourceId).
		Select("COALESCE(MAX(sort), 0)").Scan(&maxSort).Error
	if g.Sort <= 0 {
		g.Sort = maxSort + 1
	}
	if err := gdb.WithContext(ctx).Create(&g).Error; err != nil {
		return model.HomeRecommendGroup{}, err
	}
	repobase.BumpIndexPageGeneration()
	return g, nil
}

// UpdateHomeRecommendGroup 更新推荐分组配置。
func UpdateHomeRecommendGroup(ctx context.Context, g model.HomeRecommendGroup) error {
	gdb, err := recommendDB()
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	if g.SyncLimit <= 0 {
		g.SyncLimit = 12
	}
	res := gdb.WithContext(ctx).Model(&model.HomeRecommendGroup{}).
		Where("id = ?", strings.TrimSpace(g.Id)).
		Updates(map[string]any{
			"title":          strings.TrimSpace(g.Title),
			"enabled":        g.Enabled,
			"auto_sync":      g.AutoSync,
			"sync_chart_key": strings.TrimSpace(g.SyncChartKey),
			"category_id":    g.CategoryId,
			"sync_limit":     g.SyncLimit,
			"updated_at":     time.Now(),
		})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrRecommendGroupNotFound
	}
	repobase.BumpIndexPageGeneration()
	return nil
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

// ReorderHomeRecommendGroups 批量更新指定站点下推荐分组顺序。
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
		for idx, id := range orderedIDs {
			if err := tx.Model(&model.HomeRecommendGroup{}).
				Where("id = ? AND source_id = ?", strings.TrimSpace(id), sourceID).
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

// AddHomeRecommendItem 向指定分组追加影片。
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

	var count int64
	if err := gdb.WithContext(ctx).Model(&model.HomeRecommendItem{}).
		Where("group_id = ? AND mid = ?", groupID, mid).Count(&count).Error; err != nil {
		return model.HomeRecommendItem{}, err
	}
	if count > 0 {
		return model.HomeRecommendItem{}, ErrRecommendDuplicateFilm
	}

	var maxSort int64
	_ = gdb.WithContext(ctx).Model(&model.HomeRecommendItem{}).
		Where("group_id = ?", groupID).
		Select("COALESCE(MAX(sort), 0)").Scan(&maxSort).Error

	item := model.HomeRecommendItem{
		Id:          uuid.NewString(),
		GroupId:     groupID,
		SourceId:    sourceID,
		Mid:         mid,
		Sort:        maxSort + 1,
		DoubanScore: strings.TrimSpace(doubanScore),
	}
	if err := gdb.WithContext(ctx).Create(&item).Error; err != nil {
		if db.IsDuplicateKeyError(err) {
			return model.HomeRecommendItem{}, ErrRecommendDuplicateFilm
		}
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
	if err := gdb.WithContext(ctx).Where("group_id = ?", strings.TrimSpace(groupID)).Delete(&model.HomeRecommendItem{}).Error; err != nil {
		return err
	}
	repobase.BumpIndexPageGeneration()
	return nil
}

// ReplaceHomeRecommendGroupItems 原子替换某分组内的全部影片并可选更新同步时间。
func ReplaceHomeRecommendGroupItems(ctx context.Context, groupID, sourceID string, items []model.HomeRecommendItem, syncedAt *time.Time) error {
	gdb, err := recommendDB()
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	groupID = strings.TrimSpace(groupID)
	sourceID = strings.TrimSpace(sourceID)

	err = gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := tx.Where("group_id = ?", groupID).Delete(&model.HomeRecommendItem{}).Error; err != nil {
			return err
		}
		if len(items) > 0 {
			for i := range items {
				if items[i].Id == "" {
					items[i].Id = uuid.NewString()
				}
				items[i].GroupId = groupID
				items[i].SourceId = sourceID
				items[i].Sort = int64(i + 1)
			}
			if err := tx.Create(&items).Error; err != nil {
				return err
			}
		}
		if syncedAt != nil {
			if err := tx.Model(&model.HomeRecommendGroup{}).Where("id = ?", groupID).
				Updates(map[string]any{"last_synced_at": syncedAt, "updated_at": time.Now()}).Error; err != nil {
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
