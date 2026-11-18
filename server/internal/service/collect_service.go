package service

import (
	"errors"
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider"
	"server/internal/spider/progress"
	"sync"
	"sync/atomic"
	"time"
)

type CollectService struct{}

var CollectSvc = new(CollectService)

var (
	primarySwitchEpoch atomic.Uint64
	primarySwitchMu    sync.Mutex
	primaryWarmMu      sync.Mutex
	isPrimaryWarming   atomic.Bool
	// primaryWarmups 记录切换主站后异步预热的协程，测试用来等它们结束。
	primaryWarmups sync.WaitGroup
	// categorySyncs 记录异步同步分类的协程，测试用来等它们结束。
	categorySyncs sync.WaitGroup
	// deleteSourceJobs 记录已入队待处理的异步删站任务（含尾部的切主站触发），保证 WaitAsyncWork 不与 Add 竞态。
	deleteSourceJobs sync.WaitGroup
)

// WaitAsyncWork 等本包启动的后台协程（异步删站、切主站预热、异步同步分类）全部结束，超时返回 false。测试用。
func WaitAsyncWork(timeout time.Duration) bool {
	done := make(chan struct{})
	go func() {
		deleteSourceJobs.Wait()
		primaryWarmups.Wait()
		categorySyncs.Wait()
		close(done)
	}()
	select {
	case <-done:
		return true
	case <-time.After(timeout):
		return false
	}
}

// IsPrimarySiteSwitching 返回当前是否正在构建检索索引或预热主站缓存
func IsPrimarySiteSwitching() bool {
	return filmsnapshot.IsSearchIndexBuilding() || isPrimaryWarming.Load()
}

func (s *CollectService) SearchIndexProgress() filmsnapshot.SearchIndexProgress {
	return *filmsnapshot.GetLatestSearchIndexProgress()
}

func clearProvideNetworkConfigCache() {
	if db.Rdb == nil {
		return
	}
	pattern := config.TVBoxNetworkConfigCacheKey + ":*"
	iter := db.Rdb.Scan(db.Cxt, 0, pattern, config.MaxScanCount).Iterator()
	for iter.Next(db.Cxt) {
		db.Rdb.Del(db.Cxt, iter.Val())
	}
}

func (s *CollectService) GetFilmSourceList() ([]model.FilmSourceListItem, error) {
	sources, err := repository.GetCollectSourceList()
	if err != nil {
		return nil, err
	}
	list := make([]model.FilmSourceListItem, 0, len(sources))
	progressByID := make(map[string]model.CollectProgress)
	for _, progress := range spider.GetActiveTaskProgress() {
		progressByID[progress.Id] = progress
	}
	runStatsByID := getRunStatsBySource(sources)
	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Id != "" {
			sourceIDs = append(sourceIDs, source.Id)
		}
	}
	categoryReady := repository.SourceIDsWithCategory(sourceIDs)
	primarySource := repository.GetActiveCollectSource()
	primaryID := ""
	if primarySource != nil {
		primaryID = primarySource.Id
	}
	for _, source := range sources {
		if source.Id == primaryID {
			source.IsPrimary = true
		} else {
			source.IsPrimary = false
		}
		stat := runStatsByID[source.Id]
		item := model.FilmSourceListItem{
			FilmSource:       source,
			LastRunTime:      stat.LastRunTime,
			LastRunPageCount: stat.LastRunPageCount,
			SitePageTotal:    stat.SitePageTotal,
			Deleting:         progress.IsSourceDeleting(source.Id),
		}
		if progress, ok := progressByID[source.Id]; ok {
			item.Progress = &progress
		}
		ok, _, err := repository.ResolveSourceProxy(source.Id)
		if err != nil {
			return nil, err
		}
		if ok {
			item.ProxyEnabled = true
		}
		if _, ok := categoryReady[source.Id]; ok {
			item.CategoryReady = true
		}
		if progress.IsCategorySyncing(source.Id) {
			item.CategorySyncing = true
		}
		list = append(list, item)
	}
	return list, nil
}

// GetCategoryStatusMap 返回所有采集站的分类同步状态映射 ("syncing" | "ready" | "failed")
func (s *CollectService) GetCategoryStatusMap() (map[string]string, error) {
	sources, err := repository.GetCollectSourceList()
	if err != nil {
		return nil, err
	}
	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Id != "" {
			sourceIDs = append(sourceIDs, source.Id)
		}
	}
	readySet := repository.SourceIDsWithCategory(sourceIDs)
	result := make(map[string]string, len(sources))
	for _, source := range sources {
		if progress.IsCategorySyncing(source.Id) {
			result[source.Id] = "syncing"
		} else if _, ok := readySet[source.Id]; ok {
			result[source.Id] = "ready"
		} else {
			result[source.Id] = "failed"
		}
	}
	return result, nil
}

// GetCategoryReadyMap 返回所有采集站的分类就绪状态映射
func (s *CollectService) GetCategoryReadyMap() (map[string]bool, error) {
	sources, err := repository.GetCollectSourceList()
	if err != nil {
		return nil, err
	}
	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Id != "" {
			sourceIDs = append(sourceIDs, source.Id)
		}
	}
	readySet := repository.SourceIDsWithCategory(sourceIDs)
	result := make(map[string]bool, len(sources))
	for _, source := range sources {
		_, ok := readySet[source.Id]
		result[source.Id] = ok
	}
	return result, nil
}

func onPrimarySourceMaybeChanged(oldPrimary, newPrimary *model.FilmSource) bool {
	oldID := ""
	if oldPrimary != nil {
		oldID = oldPrimary.Id
	}
	newID := ""
	if newPrimary != nil {
		newID = newPrimary.Id
	}
	if oldID == newID {
		return false
	}
	epoch := primarySwitchEpoch.Add(1)
	repository.MarkCategoryChanged()
	repository.ClearIndexPageCache()
	filmsnapshot.BumpSearchCacheVersion()
	filmsnapshot.RefreshAccessDataCaches()
	filmsnapshot.ClearSearchCache()
	enabled, err := repository.GetEnabledCollectSourceList()
	if err != nil {
		syslog.Errorf("[Collect] 读取启用采集站失败: %v", err)
	} else {
		NotifySvc.PublishSourceOrderChange(oldPrimary, newPrimary, enabled)
	}
	// 先占住切换状态再丢掉上一站索引。防抖只推迟扫描，不让旧索引继续常驻，也不让这段时间再切一次。
	// 标志的置位和清除共用一把锁，避免上一轮结束时把新一轮刚置上的状态清掉。
	primaryWarmMu.Lock()
	isPrimaryWarming.Store(true)
	primaryWarmMu.Unlock()
	filmsnapshot.DropPrimarySearchIndex()
	primaryWarmups.Add(1)
	go func(targetSourceID string, currentEpoch uint64) {
		defer primaryWarmups.Done()
		defer func() {
			if r := recover(); r != nil {
				syslog.Errorf("[Collect] 切换主站后异步预热索引发生异常: %v", r)
			}
		}()
		defer func() {
			primaryWarmMu.Lock()
			if primarySwitchEpoch.Load() == currentEpoch {
				isPrimaryWarming.Store(false)
			}
			primaryWarmMu.Unlock()
		}()

		// 150ms 防抖：如果连续快速拖拽切换主站，直接跳过旧代数，避免短时间内多次扫描主站
		time.Sleep(150 * time.Millisecond)
		if primarySwitchEpoch.Load() != currentEpoch {
			syslog.Infof("[Collect] 检测到更新的主站切换，跳过历史代数预热 epoch=%d targetSource=%s", currentEpoch, targetSourceID)
			return
		}

		// 互斥排队串行化构建，避免重叠扫描主站
		primarySwitchMu.Lock()
		defer primarySwitchMu.Unlock()

		// 获取锁后再次检查代数与当前最新主站：如果中途有更新切换，立即退出
		if primarySwitchEpoch.Load() != currentEpoch {
			syslog.Infof("[Collect] 排队期间主站再次变更，放弃本代构建 epoch=%d", currentEpoch)
			return
		}
		latestPrimary := repository.PickPrimarySourceForCategory()
		if latestPrimary == nil || latestPrimary.Id != targetSourceID {
			syslog.Infof("[Collect] 实际主站已非目标站，改为补建当前主站索引 targetSource=%s", targetSourceID)
			if err := filmsnapshot.EnsurePrimarySearchIndex(); err != nil {
				syslog.Errorf("[Collect] 补建当前主站索引失败: %v", err)
			}
			return
		}

		syslog.Infof("[Collect] 开始重建主站检索索引 (epoch=%d, targetSource=%s)...", currentEpoch, targetSourceID)
		if err := filmsnapshot.RebuildPrimarySearchIndex(); err != nil {
			syslog.Errorf("[Collect] 切换主站后重建检索索引失败: %v", err)
			if err := filmsnapshot.EnsurePrimarySearchIndex(); err != nil {
				syslog.Errorf("[Collect] 补建当前主站索引失败: %v", err)
			}
			return
		}

		if primarySwitchEpoch.Load() != currentEpoch {
			syslog.Infof("[Collect] 索引已就绪，但主站代数已失效，跳过首页缓存写入 epoch=%d", currentEpoch)
			return
		}

		if targetSourceID != "" {
			if _, err := IndexSvc.IndexPage(targetSourceID); err != nil {
				syslog.Errorf("[Collect] 切换主站后预热首页数据失败: %v", err)
			} else {
				syslog.Infof("[Collect] 切换主站首页缓存预热完成 (epoch=%d, targetSource=%s)", currentEpoch, targetSourceID)
			}
		}
	}(newID, epoch)
	return true
}

func getRunStatsBySource(sources []model.FilmSource) map[string]repository.CollectSourceRunStat {
	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Id != "" {
			sourceIDs = append(sourceIDs, source.Id)
		}
	}
	return repository.GetCollectSourceRunStats(sourceIDs)
}

func (s *CollectService) RefreshSitePageTotal(id string) (int, error) {
	src := repository.FindCollectSourceById(id)
	if src == nil {
		return 0, errors.New("当前资源站信息不存在")
	}
	total, err := spider.ProbeSitePageTotal(src)
	if err != nil {
		return 0, err
	}
	if total > 0 {
		_ = repository.UpdateSitePageTotal(id, total)
	}
	return total, nil
}

func (s *CollectService) GetFilmSource(id string) *model.FilmSource {
	return repository.FindCollectSourceById(id)
}

func (s *CollectService) GetEnabledFilmSources() ([]model.FilmSource, error) {
	return repository.GetEnabledCollectSourceList()
}

func (s *CollectService) GetAllFilmSources() ([]model.FilmSource, error) {
	return repository.GetCollectSourceList()
}
