package collect

import (
	"server/internal/utils"
	"server/internal/repobase"
	"server/internal/film/catalog"
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync/atomic"

	"server/internal/infra/syslog"
	"server/internal/model"
	filmrepo "server/internal/film"
	filmcache "server/internal/film/cache"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/collect/progress"
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
	repobase.RefreshCategoryCache()
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

// AutoCollect 自动采集
func (s *SpiderService) AutoCollect(time int) {
	utils.GoSafe("自动采集", func() { AutoCollect(time) })
}

// resetSiteDataRunning 防止并发重复触发站点数据重置。
var (
	resetSiteDataRunning atomic.Bool
	resetSiteLifecycle   atomic.Bool

	// 站点重置跨模块钩子，由装配层注入。
	OnResetDisableAccess  func()
	OnResetClearAccess    func() error
	OnResetRecoverAccess  func()
	OnResetRecoverCrontab func()
	OnResetReinitSite     func() error
)

func init() {
	RegisterSiteResetChecker(func() bool {
		return resetSiteDataRunning.Load() || resetSiteLifecycle.Load()
	})
}

// triggerResetCategorySync 数据重置后异步回补默认站分类，测试可替换避免访问外网。
var triggerResetCategorySync = func() {
	go SpiderSvc.SyncMissingSourceCategories()
}

func GetTriggerResetCategorySync() func() { return triggerResetCategorySync }
func SetTriggerResetCategorySync(fn func()) { triggerResetCategorySync = fn }

// ClearFilms 重置站点数据：清空业务与配置数据表（保留账号表 user 与迁移版本表 schema_migrations），
// 同步清空图库与全部缓存，并重新写入初始化数据与启动默认定时任务。
// 全程按关键节点上报真实进度，由 SSE 推给页面。
func (s *SpiderService) ClearFilms() (retErr error) {
	if OnResetDisableAccess == nil || OnResetClearAccess == nil ||
		OnResetRecoverAccess == nil || OnResetRecoverCrontab == nil || OnResetReinitSite == nil {
		return errors.New("站点重置钩子未初始化")
	}
	resetSiteLifecycle.Store(true)
	defer resetSiteLifecycle.Store(false)

	filmrepo.StartResetProgress()
	clearedDB := false
	defer func() {
		if retErr != nil {
			// 清库前置阶段失败或半途失败时尽力恢复访问统计开关与定时任务，避免系统停摆
			if OnResetRecoverAccess != nil {
				OnResetRecoverAccess()
			}
			if clearedDB && OnResetRecoverCrontab != nil {
				OnResetRecoverCrontab()
			}
			filmrepo.FinishResetProgress(retErr)
		}
	}()

	filmrepo.ReportResetProgress(2, "正在停止后台任务")
	if OnResetDisableAccess != nil {
		OnResetDisableAccess()
	}
	StopAllCollect("数据重置，停止采集")
	CollectSvc.CancelDelFilmSources(nil)
	deleteSourceJobs.Wait()
	primaryWarmups.Wait()
	categorySyncs.Wait()

	if err := ClearSpider(); err != nil {
		return err
	}
	clearedDB = true
	if OnResetClearAccess != nil {
		if err := OnResetClearAccess(); err != nil {
			return err
		}
	}

	filmrepo.ReportResetProgress(85, "正在写入初始化数据")
	if OnResetReinitSite != nil {
		if err := OnResetReinitSite(); err != nil {
			return err
		}
	}

	filmrepo.ReportResetProgress(95, "正在同步默认分类")
	if triggerResetCategorySync != nil {
		triggerResetCategorySync()
	}

	filmrepo.ReportResetProgress(100, "数据重置完成，已恢复初始状态")
	filmrepo.FinishResetProgress(nil)
	return nil
}

// SiteResetRunning 是否已有数据重置在执行。
func (s *SpiderService) SiteResetRunning() bool {
	return resetSiteDataRunning.Load()
}

// StreamSiteReset 执行数据重置，并把每个进度快照交给 onEvent。
// 客户端断开后停止推送，重置本身继续跑完。
func (s *SpiderService) StreamSiteReset(ctx context.Context, onEvent func(any) error) error {
	if !resetSiteDataRunning.CompareAndSwap(false, true) {
		return errors.New("站点数据正在重置中，请勿重复操作")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	events := make(chan filmrepo.ResetProgress, 16)
	unsub := filmrepo.WatchResetProgress(func(p filmrepo.ResetProgress) {
		select {
		case events <- p:
		case <-ctx.Done():
		}
	})
	defer unsub()

	jobDone := make(chan struct{})
	go func() {
		defer close(jobDone)
		defer resetSiteDataRunning.Store(false)
		if err := s.ClearFilms(); err != nil {
			syslog.Errorf("[数据重置] 重置失败: %v", err)
			return
		}
		syslog.Infof("[数据重置] 重置完成")
	}()

	send := func(p filmrepo.ResetProgress) bool {
		if onEvent != nil {
			if err := onEvent(p); err != nil {
				return true
			}
		}
		return !p.Running
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case p := <-events:
			if send(p) {
				return nil
			}
		case <-jobDone:
			for {
				select {
				case p := <-events:
					if send(p) {
						return nil
					}
				default:
					return nil
				}
			}
		}
	}
}

// FollowSiteReset 订阅当前重置进度。先推一次快照；没在重置就结束，正在重置则推到结束。
func (s *SpiderService) FollowSiteReset(ctx context.Context, onEvent func(any) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	events := make(chan filmrepo.ResetProgress, 16)
	unsub := filmrepo.WatchResetProgress(func(p filmrepo.ResetProgress) {
		select {
		case events <- p:
		case <-ctx.Done():
		}
	})
	defer unsub()

	snap := filmrepo.GetResetProgress()
	if onEvent != nil {
		if err := onEvent(snap); err != nil {
			return nil
		}
	}
	if !snap.Running {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case p := <-events:
			if onEvent != nil {
				if err := onEvent(p); err != nil {
					return nil
				}
			}
			if !p.Running {
				return nil
			}
		}
	}
}

// InventoryStats 返回工作台片库规模（顶部为全部已启用站点汇总，明细为各站真实已采集数据）。
func (s *SpiderService) InventoryStats(ctx context.Context, refresh bool) (filmrepo.InventoryStats, error) {
	return filmrepo.GetInventoryStats(ctx, refresh)
}

// CollectFilms 触发主站影片采集同步（兼容单个与批量）
func (s *SpiderService) CollectFilms(ids []int64) {
	utils.GoSafe("单片更新", func() { CollectFilms(ids) })
}

// FilmClassCollect 重新拉取每个采集站的分类副本。已调整的显示和排序保留。
func (s *SpiderService) FilmClassCollect() error {
	return s.SyncAllSourceCategories()
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

	sources, err := repobase.GetEnabledCollectSourceList()
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

	primary := repobase.CurrentPrimarySource()
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
			if repobase.FindCollectSourceById(source.Id) == nil {
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
			_ = filmsnapshot.RefreshActiveProjectedReadModel()
			repobase.ClearIndexPageCache()
			log.Printf("[SpiderService] 主站分类补拉完成，已刷新前台投影读模型")
		}
	}
}

// StopAllTasks 强制停止所有採集任務
func (s *SpiderService) StopAllTasks() {
	StopAllTasks()
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

// StopTask 停止指定采集站的任务（仅停止任务，不影响采集站启用状态）。
func (s *SpiderService) StopTask(id string) error {
	fs := repobase.FindCollectSourceById(id)
	if fs == nil {
		return errors.New("采集站信息不存在")
	}
	StopTask(id)
	return nil
}
