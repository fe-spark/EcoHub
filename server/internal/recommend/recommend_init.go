package recommend

import (
	"context"
	"errors"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repobase"

	"github.com/google/uuid"
	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"
)

var recommendInitSF singleflight.Group

// IsSourceHomeRecommendInited 查询指定采集站是否已完成首页推荐默认初始化。
func IsSourceHomeRecommendInited(ctx context.Context, sourceID string) (inited bool, exists bool, err error) {
	gdb, err := recommendDB()
	if err != nil {
		return false, false, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	var src model.FilmSource
	err = gdb.WithContext(ctx).
		Select("id", "home_recommend_inited").
		Where("id = ?", strings.TrimSpace(sourceID)).
		First(&src).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return false, false, nil
	}
	if err != nil {
		return false, false, err
	}
	return src.HomeRecommendInited, true, nil
}

// MarkSourceHomeRecommendInited 将指定采集站标记为已完成首页推荐初始化（避免后续删空分组被自动重新填充）。
func MarkSourceHomeRecommendInited(ctx context.Context, sourceID string) error {
	gdb, err := recommendDB()
	if err != nil {
		return err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}
	return gdb.WithContext(ctx).
		Model(&model.FilmSource{}).
		Where("id = ? AND home_recommend_inited = ?", sourceID, false).
		Update("home_recommend_inited", true).Error
}

// InitDefaultCategoryGroupsTx 在事务内以条件更新抢占初始化权，为首次就绪的采集站一次性追加缺失的可见一级分类分组。
// 保留该站升级前已有的推荐分组顺序（例如 v3.1.7 已配置的榜单分组），仅将尚未存在的分类分组追加至末尾。
func InitDefaultCategoryGroupsTx(ctx context.Context, sourceID string, roots []*model.CategoryTree, syncLimit int64) (bool, error) {
	gdb, err := recommendDB()
	if err != nil {
		return false, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || len(roots) == 0 {
		return false, nil
	}

	var created bool
	err = gdb.WithContext(ctx).Transaction(func(tx *gorm.DB) error {
		res := tx.Model(&model.FilmSource{}).
			Where("id = ? AND home_recommend_inited = ?", sourceID, false).
			Update("home_recommend_inited", true)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return nil
		}
		var existing []model.HomeRecommendGroup
		if err := tx.Model(&model.HomeRecommendGroup{}).
			Select("category_id", "sort").
			Where("source_id = ?", sourceID).
			Find(&existing).Error; err != nil {
			return err
		}
		existByCat := make(map[int64]bool, len(existing))
		var maxSort int64
		for _, g := range existing {
			if g.CategoryId > 0 {
				existByCat[g.CategoryId] = true
			}
			if g.Sort > maxSort {
				maxSort = g.Sort
			}
		}
		groups := make([]model.HomeRecommendGroup, 0, len(roots))
		for _, ch := range roots {
			if ch == nil || !ch.Show || ch.Id <= 0 || existByCat[ch.Id] {
				continue
			}
			title := strings.TrimSpace(ch.Name)
			if title == "" {
				continue
			}
			maxSort++
			groups = append(groups, model.HomeRecommendGroup{
				Id:         uuid.NewString(),
				SourceId:   sourceID,
				Title:      title,
				Sort:       maxSort,
				Enabled:    true,
				CategoryId: ch.Id,
				SyncLimit:  syncLimit,
			})
		}
		if len(groups) == 0 {
			return nil
		}
		if err := tx.Create(&groups).Error; err != nil {
			return err
		}
		created = true
		return nil
	})
	if err != nil {
		return false, err
	}
	if created {
		repobase.BumpIndexPageGeneration()
	}
	return created, nil
}

// ensureDefaultCategoryGroups 为尚未初始化过首页推荐的采集站一次性导入当前可见的一级分类。
// 若站点暂无可见一级分类则暂不落标，待采集出分类后再触发；v3.1.7 升级站保留已有榜单分组并在其后补齐站点分类。
func (s *HomeRecommendService) ensureDefaultCategoryGroups(ctx context.Context, sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}
	inited, exists, err := IsSourceHomeRecommendInited(ctx, sourceID)
	if err != nil || !exists || inited {
		return err
	}
	_, err, _ = recommendInitSF.Do(sourceID, func() (any, error) {
		initCtx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
		defer cancel()

		inited, exists, err := IsSourceHomeRecommendInited(initCtx, sourceID)
		if err != nil || !exists || inited {
			return nil, err
		}
		roots, _ := visibleRootCategories(sourceID)
		if len(roots) == 0 {
			return nil, nil
		}
		created, err := InitDefaultCategoryGroupsTx(initCtx, sourceID, roots, recommendSyncLimitDefault)
		if err != nil {
			return nil, err
		}
		if created {
			invalidateHomeCacheForSource(sourceID)
		}
		return nil, nil
	})
	return err
}
