package service

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider"
	"server/internal/spider/progress"
)

// SyncSourceCategories 拉取一个采集站的分类，并写入该站自己的副本。支持变更对比与确认。
func (s *SpiderService) SyncSourceCategories(id, confirm string) (model.CategorySyncResult, error) {
	id = strings.TrimSpace(id)
	confirm = strings.TrimSpace(confirm)

	source := repository.FindCollectSourceById(id)
	if source == nil {
		if id != "" {
			progress.BroadcastCategoryStatus(id, "idle")
		}
		return model.CategorySyncResult{}, errors.New("采集站信息不存在")
	}

	progress.BroadcastCategoryStatus(source.Id, "syncing")
	log.Printf("[SpiderService] 拉取采集站分类: name=%s id=%s uri=%s confirm=%s", source.Name, source.Id, source.Uri, confirm)

	categoryTree, pageCount, err := spider.FetchCategoryTree(source)
	if err != nil {
		log.Printf("[SpiderService] 采集站分类拉取失败: name=%s id=%s err=%v", source.Name, source.Id, err)
		if repository.FindCollectSourceById(source.Id) == nil {
			progress.BroadcastCategoryStatus(source.Id, "idle")
		} else {
			progress.BroadcastCategoryStatus(source.Id, "failed")
		}
		return model.CategorySyncResult{}, fmt.Errorf("获取采集站分类失败: %w", err)
	}

	if confirm != model.CategorySyncKeep && confirm != model.CategorySyncClear {
		changes, previewErr := repository.PreviewSourceCategoryChanges(source.Id, categoryTree)
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
	applyRes, err := spider.ApplySourceCategory(source, categoryTree, clearData)
	if err != nil {
		log.Printf("[SpiderService] 采集站分类同步失败: name=%s id=%s err=%v", source.Name, source.Id, err)
		if repository.FindCollectSourceById(source.Id) == nil {
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
