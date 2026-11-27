package recommend

import (
	"context"
	"errors"
	"sort"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repobase"

	"github.com/google/uuid"
	"gorm.io/gorm"
)

// ReplaceHomeRecommendGroupItems 原子替换某分组内的全部影片并可选更新同步时间（先锁父分组防间隙锁死锁与孤儿写入）。
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
		return replaceGroupItemsInTx(tx, groupID, sourceID, items, syncedAt)
	})
	if err == nil {
		repobase.BumpIndexPageGeneration()
	}
	return err
}

func replaceGroupItemsInTx(tx *gorm.DB, groupID, sourceID string, items []model.HomeRecommendItem, syncedAt *time.Time) error {
	if _, err := lockRecommendGroupTx(tx, groupID); err != nil {
		return err
	}
	return replaceLockedGroupItemsInTx(tx, groupID, sourceID, items, syncedAt)
}

func replaceLockedGroupItemsInTx(tx *gorm.DB, groupID, sourceID string, items []model.HomeRecommendItem, syncedAt *time.Time) error {
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
}

// ReplaceMultipleGroupItemsAtomic 在单事务内按站锁和组顺序原子替换同站多个推荐分组的影片，防止 AB-BA 死锁与跨站越权。
func ReplaceMultipleGroupItemsAtomic(ctx context.Context, sourceID string, batch []struct {
	GroupID string
	Items   []model.HomeRecommendItem
}) error {
	gdb, err := recommendDB()
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sourceID = strings.TrimSpace(sourceID)
	sortedBatch := append([]struct {
		GroupID string
		Items   []model.HomeRecommendItem
	}(nil), batch...)
	sort.Slice(sortedBatch, func(i, j int) bool {
		return strings.TrimSpace(sortedBatch[i].GroupID) < strings.TrimSpace(sortedBatch[j].GroupID)
	})
	err = gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRecommendSourceTx(tx, sourceID); err != nil {
			return err
		}
		for _, entry := range sortedBatch {
			g, lockErr := lockRecommendGroupTx(tx, entry.GroupID)
			if lockErr != nil {
				return lockErr
			}
			if g.SourceId != sourceID {
				return errors.New("推荐分组不属于当前采集站")
			}
			if err := replaceLockedGroupItemsInTx(tx, g.Id, sourceID, entry.Items, nil); err != nil {
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

// CreateMissingGroupsAtomicTx 在站点行锁事务内查重并批量插入缺失的推荐/分类分组，同时标记站点已初始化。
func CreateMissingGroupsAtomicTx(
	ctx context.Context,
	sourceID string,
	buildMissing func(existing []model.HomeRecommendGroup, maxSort int64) []model.HomeRecommendGroup,
) ([]model.HomeRecommendGroup, error) {
	gdb, err := recommendDB()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sourceID = strings.TrimSpace(sourceID)
	var created []model.HomeRecommendGroup
	err = gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		if err := lockRecommendSourceTx(tx, sourceID); err != nil {
			return err
		}
		var existing []model.HomeRecommendGroup
		if err := tx.Where("source_id = ?", sourceID).Order("sort ASC, id ASC").Find(&existing).Error; err != nil {
			return err
		}
		var maxSort int64
		for _, g := range existing {
			if g.Sort > maxSort {
				maxSort = g.Sort
			}
		}
		toCreate := buildMissing(existing, maxSort)
		for i := range toCreate {
			if toCreate[i].Id == "" {
				toCreate[i].Id = uuid.NewString()
			}
			toCreate[i].SourceId = sourceID
			toCreate[i].Title = strings.TrimSpace(toCreate[i].Title)
			if toCreate[i].SyncLimit <= 0 {
				toCreate[i].SyncLimit = 12
			}
		}
		if len(toCreate) > 0 {
			if err := tx.Create(&toCreate).Error; err != nil {
				return err
			}
			created = toCreate
		}
		return tx.Model(&model.FilmSource{}).
			Where("id = ? AND home_recommend_inited = ?", sourceID, false).
			Update("home_recommend_inited", true).Error
	})
	if err != nil {
		return nil, err
	}
	if len(created) > 0 {
		repobase.BumpIndexPageGeneration()
	}
	return created, nil
}
