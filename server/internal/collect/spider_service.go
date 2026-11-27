package collect

import (
	"context"
	"errors"
	"fmt"
	"log"
	"server/internal/film/catalog"
	"server/internal/repobase"
	"server/internal/sourcecatalog"
	"server/internal/taxonomy"
	"server/internal/utils"
	"strings"
	"sync"

	"server/internal/collect/progress"
	filmrepo "server/internal/film"
	filmcache "server/internal/film/cache"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/syslog"
	"server/internal/model"
)

type SpiderService struct{}

var SpiderSvc = new(SpiderService)

func clearCategorySyncRedisCaches() {
	catalog.ClearCategoryCache()
	filmcache.ClearAllSearchTagsCache()
	filmcache.ClearTVBoxListCache()
	repobase.ClearIndexPageCache()
}

func finalizeCategorySync() {
	taxonomy.RefreshCategoryCache()
	clearCategorySyncRedisCaches()
	filmrepo.RefreshInventoryStatsAsync()
}

// StartCollect 执行对指定站点的采集任务
func (s *SpiderService) StartCollect(id string, h int) error {
	res := s.BatchCollect(h, []string{id})
	if len(res.Started) == 0 {
		for _, item := range res.Skipped {
			return errors.New(item.Reason)
		}
		return errors.New("采集任务开启失败")
	}
	return nil
}

// BatchCollect 批量采集：能开的站同步标成 starting 后异步执行，跳过的站带原因返回。
func (s *SpiderService) BatchCollect(time int, ids []string) model.CollectBatchResult {
	claim, result := PrepareBatchCollectStart(ids)
	if len(claim.Sources) > 0 {
		utils.GoSafe("批量采集", func() { BatchCollectPrepared(model.CollectTriggerManual, time, claim) })
	}
	return result
}

var categorySyncs sync.WaitGroup

// WaitCategorySyncs 等待全部异步分类同步协程结束（供上层测试或站点重置调用）。
func WaitCategorySyncs() {
	categorySyncs.Wait()
}

// InventoryStats 返回工作台片库规模（顶部为全部已启用站点汇总，明细为各站真实已采集数据）。
func (s *SpiderService) InventoryStats(ctx context.Context, refresh bool) (filmrepo.InventoryStats, error) {
	return filmrepo.GetInventoryStats(ctx, refresh)
}

// CollectFilms 触发主站影片采集同步（兼容单个与批量）
func (s *SpiderService) CollectFilms(ids []int64) {
	utils.GoSafe("单片更新", func() { CollectFilms(ids) })
}

// SyncSourceCategoriesAsync 异步拉取一个采集站的分类，立即广播 syncing 状态，后台完成写入。
func (s *SpiderService) SyncSourceCategoriesAsync(id string) {
	id = strings.TrimSpace(id)
	if id == "" {
		return
	}
	progress.BroadcastCategoryStatus(id, "syncing")
	categorySyncs.Add(1)
	go func() {
		defer categorySyncs.Done()
		if _, err := s.SyncSourceCategories(id, ""); err != nil {
			syslog.Errorf("[SpiderService] 异步同步分类失败 id=%s: %v", id, err)
		}
	}()
}

// SyncAllSourceCategories 按现有采集站逐个拉取分类。某一站失败不影响其他站，最后返回首个错误。
func (s *SpiderService) SyncAllSourceCategories() error {
	sources, err := GetCollectSourceList()
	if err != nil {
		return err
	}
	if len(sources) == 0 {
		return errors.New("没有采集站，不能同步分类")
	}
	for i := range sources {
		progress.BroadcastCategoryStatus(sources[i].Id, "syncing")
	}
	var first error
	failed := 0
	for i := range sources {
		source := sources[i]
		pageCount, err := CollectCategory(&source)
		if err != nil {
			log.Printf("[SpiderService] 采集站分类同步失败: name=%s id=%s err=%v", source.Name, source.Id, err)
			progress.BroadcastCategoryStatus(source.Id, "failed")
			failed++
			if first == nil {
				first = fmt.Errorf("%s: %w", source.Name, err)
			}
			continue
		}
		log.Printf("[SpiderService] 采集站分类已保存: name=%s id=%s", source.Name, source.Id)
		progress.BroadcastCategoryReadyMessage(source.Id, "分类获取成功", false, pageCount)
	}
	finalizeCategorySync()
	_ = filmsnapshot.RefreshActiveProjectedReadModel()
	if failed == 0 {
		return nil
	}
	if failed == len(sources) {
		return first
	}
	return fmt.Errorf("有 %d 个采集站分类获取失败，首个错误: %w", failed, first)
}

// SyncMissingSourceCategories 只给还没有分类副本的已启用采集站补拉一次。已有副本或已停用的站不处理。
func (s *SpiderService) SyncMissingSourceCategories() {
	categorySyncs.Add(1)
	defer categorySyncs.Done()
	defer func() {
		if r := recover(); r != nil {
			syslog.Errorf("[SpiderService] 启动补拉分类发生异常: %v", r)
		}
	}()

	sources, err := sourcecatalog.GetEnabledCollectSourceList()
	if err != nil {
		log.Printf("[SpiderService] 读取已启用采集站失败: %v", err)
		return
	}
	if len(sources) == 0 {
		return
	}

	ids := make([]string, 0, len(sources))
	for _, src := range sources {
		if strings.TrimSpace(src.Uri) != "" && !progress.IsSourceDeleting(src.Id) {
			ids = append(ids, src.Id)
		}
	}
	if len(ids) == 0 {
		return
	}

	readyIDs := catalog.SourceIDsWithCategory(ids)
	var missing []model.FilmSource
	for _, src := range sources {
		if _, ok := readyIDs[src.Id]; !ok && strings.TrimSpace(src.Uri) != "" && !progress.IsSourceDeleting(src.Id) {
			missing = append(missing, src)
		}
	}
	if len(missing) == 0 {
		return
	}

	primary := sourcecatalog.CurrentPrimarySource()
	primaryID := ""
	if primary != nil {
		primaryID = primary.Id
	}

	synced := 0
	primarySynced := false

	for i := range missing {
		source := missing[i]
		if progress.IsSourceDeleting(source.Id) {
			continue
		}
		// 真正开始当前站点拉取时，才广播 syncing 状态，避免全局提前虚挂
		progress.BroadcastCategoryStatus(source.Id, "syncing")
		log.Printf("[SpiderService] 采集站还没有分类副本，开始补拉: name=%s id=%s", source.Name, source.Id)

		pageCount, err := CollectCategory(&source)
		if err != nil {
			log.Printf("[SpiderService] 补拉采集站分类失败: name=%s id=%s err=%v", source.Name, source.Id, err)
			if sourcecatalog.FindCollectSourceById(source.Id) == nil {
				progress.BroadcastCategoryStatus(source.Id, "idle")
			} else {
				progress.BroadcastCategoryStatus(source.Id, "failed")
			}
			continue
		}

		progress.BroadcastCategoryReadyMessage(source.Id, "分类获取成功", false, pageCount)
		synced++
		if source.Id == primaryID {
			primarySynced = true
		}
	}

	if synced > 0 {
		finalizeCategorySync()
		_ = filmsnapshot.RefreshActiveProjectedReadModel()
		if primarySynced {
			log.Printf("[SpiderService] 主站分类补拉完成，已刷新前台投影读模型")
		}
	}
}

// StopTasks 批量停止指定采集站的任务。
func (s *SpiderService) StopTasks(ids []string) {
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			StopTask(id)
		}
	}
}
