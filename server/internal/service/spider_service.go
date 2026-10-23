package service

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync/atomic"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	filmcache "server/internal/repository/film/cache"
	"server/internal/spider"
	"server/internal/spider/progress"
)

type SpiderService struct{}

var SpiderSvc = new(SpiderService)

func clearCategorySyncRedisCaches() {
	repository.ClearCategoryCache()
	filmcache.ClearAllSearchTagsCache()
	filmcache.ClearTVBoxListCache()
	repository.ClearIndexPageCache()
}

func finalizeCategorySync() {
	repository.RefreshCategoryCache()
	clearCategorySyncRedisCaches()
}

// StartCollect 执行对指定站点的采集任务
func (s *SpiderService) StartCollect(id string, h int) error {
	fs := repository.FindCollectSourceById(id)
	if fs == nil {
		return errors.New("采集任务开启失败，采集站信息不存在")
	}
	if !fs.State {
		return errors.New("采集任务开启失败，该采集站已被禁用，请先启用后再采集")
	}
	if !repository.SourceHasCategoryMapping(fs.Id) {
		return fmt.Errorf("采集站 %s 还没有分类，请重新保存后再采集", fs.Name)
	}
	if err := spider.PrepareSingleCollectStart(*fs); err != nil {
		return err
	}
	go func() {
		err := spider.HandlePreparedCollect(id, h)
		if err != nil {
			log.Printf("[SpiderService] 资源站[%s]采集任务执行失败: %s", id, err)
		}
	}()
	return nil
}

// BatchCollect 批量采集：能开的站同步标成 starting 后异步执行，跳过的站带原因返回。
func (s *SpiderService) BatchCollect(time int, ids []string) model.CollectBatchResult {
	sources, result := spider.PrepareBatchCollectStart(ids)
	if len(sources) > 0 {
		go spider.BatchCollectPrepared(model.CollectTriggerManual, time, sources)
	}
	return result
}

// AutoCollect 自动采集
func (s *SpiderService) AutoCollect(time int) {
	go spider.AutoCollect(time)
}

// resetSiteDataRunning 防止并发重复触发站点数据重置。
var resetSiteDataRunning atomic.Bool

// ClearFilms 重置站点业务数据：清空影视/采集派生数据。
// 清空会连分类一起清掉，随后按每个采集站重新拉取分类副本。
// 注意：不重置任何账号与密码，也不改网站配置、映射规则、采集源和采集计划任务。线上轮播清空，定时排片关闭，排片配方保留。
// 全程按关键节点上报真实进度，由 SSE 推给页面。
func (s *SpiderService) ClearFilms() (retErr error) {
	filmrepo.StartResetProgress()
	defer func() {
		if retErr != nil {
			filmrepo.FinishResetProgress(retErr)
		}
	}()
	if err := spider.ClearSpider(); err != nil {
		return err
	}
	if err := DisableBannerQuickSchedule(); err != nil {
		log.Printf("[SpiderService] 重置后关闭定时排片失败: %v", err)
		return err
	}
	// 物理清空后，广播各采集站分类已清空（未就绪态）
	sources, err := repository.GetCollectSourceList()
	if err != nil {
		return err
	}
	for _, src := range sources {
		if src.Id != "" {
			progress.BroadcastCategoryReady(src.Id, false)
		}
	}
	// 分类与影视库存一并清空后，按每个采集站重新拉取，采集才能写上 pid/cid。
	filmrepo.ReportResetProgress(96, "正在同步各采集站分类")
	if err := s.SyncAllSourceCategories(); err != nil {
		filmrepo.FinishResetProgress(err)
		return err
	}
	filmrepo.ReportResetProgress(100, "重置完成，各采集站分类已同步")
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

// InventoryStats 返回工作台片库规模。scope=sources 只算各采集站，其余只算整库。
func (s *SpiderService) InventoryStats(scope string) (filmrepo.InventoryStats, error) {
	stats := filmrepo.InventoryStats{Sources: []filmrepo.SourceInventory{}}
	if scope == "sources" {
		sources, err := filmrepo.GetSourceInventories()
		if err != nil {
			return stats, err
		}
		stats.Sources = sources
		return stats, nil
	}
	library, err := filmrepo.GetLibraryInventory()
	if err != nil {
		return stats, err
	}
	stats.Library = library
	return stats, nil
}

// CollectFilms 触发主站影片采集同步（兼容单个与批量）
func (s *SpiderService) CollectFilms(ids []int64) {
	go spider.CollectFilms(ids)
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
	go func() {
		if _, err := s.SyncSourceCategories(id, ""); err != nil {
			syslog.Errorf("[SpiderService] 异步同步分类失败 id=%s: %v", id, err)
		}
	}()
}

// SyncAllSourceCategories 按现有采集站逐个拉取分类。某一站失败不影响其他站，最后返回首个错误。
func (s *SpiderService) SyncAllSourceCategories() error {
	sources, err := repository.GetCollectSourceList()
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
		if err := spider.CollectCategory(&source); err != nil {
			log.Printf("[SpiderService] 采集站分类同步失败: name=%s id=%s err=%v", source.Name, source.Id, err)
			progress.BroadcastCategoryStatus(source.Id, "failed")
			failed++
			if first == nil {
				first = fmt.Errorf("%s: %w", source.Name, err)
			}
			continue
		}
		log.Printf("[SpiderService] 采集站分类已保存: name=%s id=%s", source.Name, source.Id)
		progress.BroadcastCategoryStatus(source.Id, "ready")
	}
	finalizeCategorySync()
	if failed == 0 {
		return nil
	}
	if failed == len(sources) {
		return first
	}
	return fmt.Errorf("有 %d 个采集站分类获取失败，首个错误: %w", failed, first)
}

// SyncMissingSourceCategories 只给还没有分类副本的采集站补拉一次。已有副本的站不覆盖。
func (s *SpiderService) SyncMissingSourceCategories() {
	sources, err := repository.GetCollectSourceList()
	if err != nil {
		log.Printf("[SpiderService] 读取采集站失败: %v", err)
		return
	}
	var missing []model.FilmSource
	for i := range sources {
		if !repository.SourceHasCategoryMapping(sources[i].Id) {
			missing = append(missing, sources[i])
			progress.BroadcastCategoryStatus(sources[i].Id, "syncing")
		}
	}
	if len(missing) == 0 {
		return
	}
	synced := 0
	for i := range missing {
		source := missing[i]
		log.Printf("[SpiderService] 采集站还没有分类副本，补拉: name=%s id=%s", source.Name, source.Id)
		if err := spider.CollectCategory(&source); err != nil {
			log.Printf("[SpiderService] 补拉采集站分类失败: name=%s id=%s err=%v", source.Name, source.Id, err)
			progress.BroadcastCategoryStatus(source.Id, "failed")
			continue
		}
		progress.BroadcastCategoryStatus(source.Id, "ready")
		synced++
	}
	if synced > 0 {
		finalizeCategorySync()
	}
}

// StopAllTasks 强制停止所有採集任務
func (s *SpiderService) StopAllTasks() {
	spider.StopAllTasks()
}

// StopTasks 批量停止指定采集站的任务。
func (s *SpiderService) StopTasks(ids []string) {
	for _, id := range ids {
		id = strings.TrimSpace(id)
		if id != "" {
			spider.StopTask(id)
		}
	}
}

// StopTask 停止指定采集站的任务（仅停止任务，不影响采集站启用状态）。
func (s *SpiderService) StopTask(id string) error {
	fs := repository.FindCollectSourceById(id)
	if fs == nil {
		return errors.New("采集站信息不存在")
	}
	spider.StopTask(id)
	return nil
}
