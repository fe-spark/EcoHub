package collect

import (
	"errors"
	"fmt"
	"log"
	"server/internal/collect/progress"
	"server/internal/film/catalog"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/model"
	"server/internal/sourcecatalog"
	"strings"
	"sync"
)

// ensureMasterCategoriesReady 采集前确认该站已经有自己的分类副本。
// 分类在添加或编辑采集站时拉取。采集过程中不再临时请求分类。
func ensureMasterCategoriesReady(s *model.FilmSource) error {
	if s == nil {
		return nil
	}
	if !catalog.SourceHasCategoryMapping(s.Id) {
		return fmt.Errorf("采集站 %s 还没有分类，请在采集中心重新保存该站后再采集", s.Name)
	}
	if _, _, err := catalog.EnsureOtherCategory(s.Id); err != nil {
		return fmt.Errorf("采集站 %s 补建「其他」分类失败: %w", s.Name, err)
	}
	return nil
}

// CollectCategory 影视分类采集
func CollectCategory(s *model.FilmSource) (int, error) {
	if s == nil {
		return 0, errors.New("采集站信息不存在")
	}
	pageCount := 0
	err := WithSourceExclusive(s.Id, func() error {
		if s.Id != "" && sourcecatalog.FindCollectSourceById(s.Id) == nil {
			return errors.New("采集站信息不存在")
		}
		tree, count, err := FetchCategoryTree(s)
		if err != nil {
			return err
		}
		pageCount = count
		if err := catalog.SaveCategoryTree(s.Id, tree); err != nil {
			return fmt.Errorf("保存采集站分类失败: %w", err)
		}
		return nil
	})
	return pageCount, err
}

// FetchCategoryTree 发起网络请求获取采集源分类树与全站总页，并同步更新采集站全站总页
func FetchCategoryTree(s *model.FilmSource) (*model.CategoryTree, int, error) {
	if s == nil {
		return nil, 0, errors.New("采集站信息不存在")
	}
	req, err := requestForSource(s)
	if err != nil {
		return nil, 0, err
	}
	categoryTree, pageCount, err := ResolveCollector(s.ResolveFormat()).GetCategoryTree(req)
	if err != nil {
		return nil, 0, fmt.Errorf("获取采集站分类树失败: %w", err)
	}
	if s.Id != "" && pageCount > 0 {
		_ = UpdateSitePageTotal(s.Id, pageCount)
	}
	return categoryTree, pageCount, nil
}

// ApplySourceCategory 在互斥锁内将分类树写入库，并根据选项清空数据
func ApplySourceCategory(s *model.FilmSource, tree *model.CategoryTree, clearData bool) (catalog.CategoryApplyResult, error) {
	if s == nil {
		return catalog.CategoryApplyResult{}, errors.New("采集站信息不存在")
	}
	var res catalog.CategoryApplyResult
	err := WithSourceExclusive(s.Id, func() error {
		if s.Id != "" && sourcecatalog.FindCollectSourceById(s.Id) == nil {
			return errors.New("采集站信息不存在")
		}
		applyRes, applyErr := catalog.ApplyCategoryTree(s.Id, tree, clearData)
		if applyErr != nil {
			return fmt.Errorf("保存采集站分类失败: %w", applyErr)
		}
		res = applyRes
		return nil
	})
	return res, err
}

// sourceExclusive 保证同一采集站的分类写入和删除不会交错。
var sourceExclusive sync.Map

func sourceExclusiveMu(id string) *sync.Mutex {
	if id == "" {
		id = "-"
	}
	val, _ := sourceExclusive.LoadOrStore(id, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// WithSourceExclusive 串行化同一采集站的分类保存与删除。
func WithSourceExclusive(id string, fn func() error) error {
	mu := sourceExclusiveMu(id)
	mu.Lock()
	defer mu.Unlock()
	return fn()
}

// SyncSourceCategories 拉取一个采集站的分类，并写入该站自己的副本。支持变更对比与确认。
func (s *SpiderService) SyncSourceCategories(id, confirm string) (model.CategorySyncResult, error) {
	id = strings.TrimSpace(id)
	confirm = strings.TrimSpace(confirm)

	source := sourcecatalog.FindCollectSourceById(id)
	if source == nil {
		if id != "" {
			progress.BroadcastCategoryStatus(id, "idle")
		}
		return model.CategorySyncResult{}, errors.New("采集站信息不存在")
	}

	progress.BroadcastCategoryStatus(source.Id, "syncing")
	log.Printf("[SpiderService] 拉取采集站分类: name=%s id=%s uri=%s confirm=%s", source.Name, source.Id, source.Uri, confirm)

	categoryTree, pageCount, err := FetchCategoryTree(source)
	if err != nil {
		log.Printf("[SpiderService] 采集站分类拉取失败: name=%s id=%s err=%v", source.Name, source.Id, err)
		if sourcecatalog.FindCollectSourceById(source.Id) == nil {
			progress.BroadcastCategoryStatus(source.Id, "idle")
		} else {
			progress.BroadcastCategoryStatus(source.Id, "failed")
		}
		return model.CategorySyncResult{}, fmt.Errorf("获取采集站分类失败: %w", err)
	}

	if confirm != model.CategorySyncKeep && confirm != model.CategorySyncClear {
		changes, previewErr := catalog.PreviewSourceCategoryChanges(source.Id, categoryTree)
		if previewErr != nil {
			log.Printf("[SpiderService] 采集站分类预览失败: name=%s id=%s err=%v", source.Name, source.Id, previewErr)
			progress.BroadcastCategoryStatus(source.Id, "failed")
			return model.CategorySyncResult{}, previewErr
		}
		if len(changes) > 0 {
			log.Printf("[SpiderService] 采集站分类树发生变化，等待确认: name=%s id=%s count=%d", source.Name, source.Id, len(changes))
			progress.BroadcastCategoryConfirm(source.Id, source.Name, changes)
			return model.CategorySyncResult{
				NeedConfirm:   true,
				Id:            source.Id,
				Name:          source.Name,
				Changes:       changes,
				SitePageTotal: pageCount,
			}, nil
		}
	}

	clearData := confirm == model.CategorySyncClear
	applyRes, err := ApplySourceCategory(source, categoryTree, clearData)
	if err != nil {
		log.Printf("[SpiderService] 采集站分类同步失败: name=%s id=%s err=%v", source.Name, source.Id, err)
		if sourcecatalog.FindCollectSourceById(source.Id) == nil {
			progress.BroadcastCategoryStatus(source.Id, "idle")
		} else {
			progress.BroadcastCategoryStatus(source.Id, "failed")
		}
		return model.CategorySyncResult{}, fmt.Errorf("获取采集站分类失败: %w", err)
	}

	if applyRes.DataCleared {
		if len(applyRes.OrphanMids) > 0 {
			filmsnapshot.DeleteActiveSnapshotsByMids(applyRes.OrphanMids...)
		}
		filmsnapshot.ClearDynamicPlayCaches()
	}

	finalizeCategorySync()
	log.Printf("[SpiderService] 采集站分类已保存: name=%s id=%s dataCleared=%v", source.Name, source.Id, applyRes.DataCleared)

	message := "分类获取成功"
	if confirm == model.CategorySyncKeep {
		message = "已更新分类，保留原数据"
	} else if confirm == model.CategorySyncClear {
		if applyRes.DataCleared {
			message = "已更新分类，并清空该站采集数据"
		} else {
			message = "已更新分类，保留原数据"
		}
	}

	progress.BroadcastCategoryReadyMessage(source.Id, message, applyRes.DataCleared, pageCount)
	return model.CategorySyncResult{
		NeedConfirm:   false,
		Id:            source.Id,
		Name:          source.Name,
		DataCleared:   applyRes.DataCleared,
		Message:       message,
		SitePageTotal: pageCount,
	}, nil
}
