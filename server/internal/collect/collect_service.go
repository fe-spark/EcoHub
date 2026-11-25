package collect

import (
	"errors"
	"sync"
	"time"

	"server/internal/collect/progress"
	"server/internal/film/catalog"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/repobase"
)

type CollectService struct{}

var CollectSvc = new(CollectService)

var (
	primarySwitchMu sync.Mutex
	// primaryWarmups 记录切换主站后异步预热的协程，测试用来等它们结束。
	primaryWarmups sync.WaitGroup
	// categorySyncs 记录异步同步分类的协程，测试用来等它们结束。
	categorySyncs sync.WaitGroup
	// deleteSourceJobs 记录已入队待处理的异步删站任务，保证 WaitAsyncWork 不与 Add 竞态。
	deleteSourceJobs sync.WaitGroup
	// OnPrimarySwitched 切换主站重建索引完成后的预热回调，由装配层注入。
	OnPrimarySwitched func(targetSourceID string) error
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

func WaitDeleteSourceJobs() {
	deleteSourceJobs.Wait()
}

func ApplyPrimaryChanged(oldPrimary, newPrimary *model.FilmSource) bool {
	return applyPrimaryChanged(oldPrimary, newPrimary)
}

// IsPrimarySiteSwitching 返回当前是否正在构建检索索引或预热主站缓存
func IsPrimarySiteSwitching() bool {
	return filmsnapshot.IsSearchIndexBuilding() || filmsnapshot.IsPrimaryWarming()
}

func (s *CollectService) SearchIndexProgress() filmsnapshot.SearchIndexProgress {
	return *filmsnapshot.GetLatestSearchIndexProgress()
}

func (s *CollectService) GetFilmSourceList() ([]model.FilmSourceListItem, error) {
	sources, err := GetCollectSourceList()
	if err != nil {
		return nil, err
	}
	list := make([]model.FilmSourceListItem, 0, len(sources))
	progressByID := make(map[string]model.CollectProgress)
	for _, progress := range GetActiveTaskProgress() {
		progressByID[progress.Id] = progress
	}
	runStatsByID := getRunStatsBySource(sources)
	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Id != "" {
			sourceIDs = append(sourceIDs, source.Id)
		}
	}
	categoryReady := catalog.SourceIDsWithCategory(sourceIDs)
	primarySource := repobase.CurrentPrimarySource()
	primaryID := ""
	if primarySource != nil {
		primaryID = primarySource.Id
	}
	spiderProxyOn, _, err := repobase.ResolveSpiderProxy()
	if err != nil {
		return nil, err
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
		if spiderProxyOn && source.ProxyCollect {
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
	sources, err := GetCollectSourceList()
	if err != nil {
		return nil, err
	}
	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Id != "" {
			sourceIDs = append(sourceIDs, source.Id)
		}
	}
	readySet := catalog.SourceIDsWithCategory(sourceIDs)
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
	sources, err := GetCollectSourceList()
	if err != nil {
		return nil, err
	}
	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Id != "" {
			sourceIDs = append(sourceIDs, source.Id)
		}
	}
	readySet := catalog.SourceIDsWithCategory(sourceIDs)
	result := make(map[string]bool, len(sources))
	for _, source := range sources {
		_, ok := readySet[source.Id]
		result[source.Id] = ok
	}
	return result, nil
}

// applyPrimaryChanged 主站实际发生变化时调用：失效相关缓存并异步预热新主站检索索引。
// old 与 new 相同时为无操作。调用方保证仅在主站切换/交接/懒初始化生效时调用。
func applyPrimaryChanged(oldPrimary, newPrimary *model.FilmSource) bool {
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
	catalog.MarkCategoryChanged()
	repobase.ClearIndexPageCache()
	filmsnapshot.BumpSearchCacheVersion()
	filmsnapshot.RefreshAccessDataCaches()
	filmsnapshot.ClearSearchCache()
	filmsnapshot.ClearDynamicPlayCaches()
	repobase.ClearProvideNetworkConfigCache()
	enabled, err := repobase.GetEnabledCollectSourceList()
	if err != nil {
		syslog.Errorf("[Collect] 读取启用采集站失败: %v", err)
	} else {
		notify.NotifySvc.PublishPrimaryChange(oldPrimary, newPrimary, enabled)
	}
	// 先占住切换状态再丢掉上一站索引，不让旧索引继续常驻，也不让这段时间再切一次。
	filmsnapshot.BeginPrimaryWarmup()
	filmsnapshot.DropPrimarySearchIndex()
	primaryWarmups.Add(1)
	go func(targetSourceID string) {
		defer primaryWarmups.Done()
		defer filmsnapshot.EndPrimaryWarmup()
		defer func() {
			if r := recover(); r != nil {
				syslog.Errorf("[Collect] 切换主站后异步预热索引发生异常: %v", r)
			}
		}()

		// 连续切换时旧协程在这里排队；轮到自己时目标已不是当前主站就直接退出。
		primarySwitchMu.Lock()
		defer primarySwitchMu.Unlock()

		latestPrimary := repobase.CurrentPrimarySource()
		if latestPrimary == nil || latestPrimary.Id != targetSourceID {
			syslog.Infof("[Collect] 实际主站已非目标站，改为补建当前主站索引 targetSource=%s", targetSourceID)
			if err := filmsnapshot.EnsurePrimarySearchIndex(); err != nil {
				syslog.Errorf("[Collect] 补建当前主站索引失败: %v", err)
			}
			return
		}

		syslog.Infof("[Collect] 开始重建主站检索索引 (targetSource=%s)...", targetSourceID)
		if err := filmsnapshot.RebuildPrimarySearchIndex(); err != nil {
			syslog.Errorf("[Collect] 切换主站后重建检索索引失败: %v", err)
			if err := filmsnapshot.EnsurePrimarySearchIndex(); err != nil {
				syslog.Errorf("[Collect] 补建当前主站索引失败: %v", err)
			}
			return
		}

		if targetSourceID != "" && OnPrimarySwitched != nil {
			if err := OnPrimarySwitched(targetSourceID); err != nil {
				syslog.Errorf("[Collect] 切换主站后预热首页数据失败: %v", err)
			} else {
				syslog.Infof("[Collect] 切换主站首页缓存预热完成 (targetSource=%s)", targetSourceID)
			}
		}
	}(newID)
	return true
}

func getRunStatsBySource(sources []model.FilmSource) map[string]CollectSourceRunStat {
	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Id != "" {
			sourceIDs = append(sourceIDs, source.Id)
		}
	}
	return GetCollectSourceRunStats(sourceIDs)
}

func (s *CollectService) RefreshSitePageTotal(id string) (int, error) {
	src := repobase.FindCollectSourceById(id)
	if src == nil {
		return 0, errors.New("当前资源站信息不存在")
	}
	total, err := ProbeSitePageTotal(src)
	if err != nil {
		return 0, err
	}
	if total > 0 {
		_ = UpdateSitePageTotal(id, total)
	}
	return total, nil
}

func (s *CollectService) GetFilmSource(id string) *model.FilmSource {
	return repobase.FindCollectSourceById(id)
}

func (s *CollectService) GetEnabledFilmSources() ([]model.FilmSource, error) {
	return repobase.GetEnabledCollectSourceList()
}

func (s *CollectService) GetAllFilmSources() ([]model.FilmSource, error) {
	return GetCollectSourceList()
}
