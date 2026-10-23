package service

import (
	"errors"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider"
	"server/internal/spider/progress"
	"server/internal/utils"
)

type CollectService struct{}

var CollectSvc = new(CollectService)

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
	lastRunTimeByID := getLastRunTimeBySource(sources)
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
		item := model.FilmSourceListItem{FilmSource: source, LastRunTime: lastRunTimeByID[source.Id]}
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
	// 分类编号属于原首选站。轮播和排片配方一起清空，定时排片关闭。
	if err := ResetBannerDeckForSourceChange(); err != nil {
		syslog.Errorf("[Banner] 切换首选站清空轮播失败: %v", err)
	}
	repository.MarkCategoryChanged()
	filmsnapshot.BumpSearchCacheVersion()
	filmsnapshot.RefreshAccessDataCaches()
	filmsnapshot.ClearSearchCache()
	if newID != "" {
		if version := filmsnapshot.GetActiveReadModelVersion(); version != "" {
			filmsnapshot.NotifySnapshotPublished(version)
		}
	}
	enabled, err := repository.GetEnabledCollectSourceList()
	if err != nil {
		syslog.Errorf("[Collect] 读取启用采集站失败: %v", err)
	} else {
		NotifySvc.PublishSourceOrderChange(oldPrimary, newPrimary, enabled)
	}
	return true
}

func (s *CollectService) SortFilmSources(ids []string) error {
	if len(ids) == 0 {
		return nil
	}
	oldPrimary := repository.PickPrimarySourceForCategory()
	if err := repository.SortCollectSources(ids); err != nil {
		return err
	}
	filmsnapshot.ClearDynamicPlayCaches()
	clearProvideNetworkConfigCache()
	newPrimary := repository.PickPrimarySourceForCategory()
	if !onPrimarySourceMaybeChanged(oldPrimary, newPrimary) {
		enabled, err := repository.GetEnabledCollectSourceList()
		if err != nil {
			return err
		}
		NotifySvc.PublishSourceOrderChange(oldPrimary, newPrimary, enabled)
	}
	return nil
}

func getLastRunTimeBySource(sources []model.FilmSource) map[string]*time.Time {
	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Id != "" {
			sourceIDs = append(sourceIDs, source.Id)
		}
	}
	return repository.GetCollectSourceRunTimes(sourceIDs)
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

// UpdateFilmSource 编辑采集源配置（单源）。
// 采集链接变更时清空该站已采集数据、失败记录和分类，调用方再按新地址重新获取分类。
func (s *CollectService) UpdateFilmSource(source model.FilmSource) error {
	oldPrimary := repository.PickPrimarySourceForCategory()
	if err := s.updateFilmSourceCore(source); err != nil {
		return err
	}
	onPrimarySourceMaybeChanged(oldPrimary, repository.PickPrimarySourceForCategory())
	return nil
}

func (s *CollectService) updateFilmSourceCore(source model.FilmSource) error {
	old := repository.FindCollectSourceById(source.Id)
	if old == nil {
		return errors.New("采集站信息不存在")
	}
	if source.CreatedAt.IsZero() && !old.CreatedAt.IsZero() {
		source.CreatedAt = old.CreatedAt
	}
	// 排序只走排序接口。编辑请求不带 sort，不能写成 0 把别的站顶成主站。
	source.Sort = old.Sort

	// 安全校验：如果有任何采集任务正在运行，禁止修改 URI 或数据协议，防止引发解析错乱
	isUriChanged := old.Uri != source.Uri
	isFormatChanged := old.ResolveFormat() != source.ResolveFormat()
	if (isUriChanged || isFormatChanged) && spider.IsAnyTaskRunning() {
		return errors.New("当前有采集任务正在运行，请先停止所有任务后再执行地址或协议变更操作")
	}

	var orphanMids []int64
	if isUriChanged {
		orphans, err := repository.CascadeCleanSourceData(source.Id)
		if len(orphans) > 0 {
			filmsnapshot.DeleteActiveSnapshotsByMids(orphans...)
		}
		if err != nil {
			syslog.Errorf("[Collect] 更换源地址清空历史数据失败: %v", err)
			return errors.New("清空历史数据失败，请重试")
		}
		orphanMids = orphans
	}
	if isUriChanged || isFormatChanged {
		if err := repository.DeleteFailureRecordsByOrigin(source.Id); err != nil {
			syslog.Errorf("[Collect] 清理变更源关联失败记录失败: %v", err)
			return errors.New("清理原失败记录失败，请重试")
		}
	}
	if isUriChanged {
		if err := repository.ClearSourceCategoryData(source.Id); err != nil {
			syslog.Errorf("[Collect] 更换源地址清空分类失败: %v", err)
			return errors.New("清空分类失败，请重试")
		}
	}
	err := repository.UpdateCollectSource(source)
	if err != nil {
		return err
	}

	if isUriChanged {
		syslog.Infof("[Collect] 采集源 %s(%s) 更换地址，已清空该站采集数据、失败记录和分类，删除档案 %d 部", source.Name, source.Id, len(orphanMids))
		repository.MarkCategoryChanged()
		progress.BroadcastCategoryReady(source.Id, false)
	}

	spider.ClearLimiter(source.Id)
	if old.State && !source.State {
		spider.StopTask(source.Id)
	}

	clearProvideNetworkConfigCache()
	filmsnapshot.ClearDynamicPlayCaches()
	if old.IsPosterSource != source.IsPosterSource {
		filmsnapshot.BumpSearchCacheVersion()
		filmsnapshot.ClearDerivedFilmListCaches()
		filmsnapshot.RefreshAccessDataCaches()
		filmsnapshot.ClearSearchCache()
	}
	return repository.RefreshCollectSourceCatalog()
}

func (s *CollectService) BatchUpdateFilmSourceState(ids []string, state bool) error {
	planned := make(map[string]bool)
	for _, id := range ids {
		source := repository.FindCollectSourceById(id)
		if source == nil {
			return errors.New("采集站信息不存在")
		}
		if source.State != state {
			planned[id] = state
		}
	}
	if len(planned) == 0 {
		return nil
	}
	oldPrimary := repository.PickPrimarySourceForCategory()
	var firstErr error
	changed := false
	for _, id := range ids {
		source := repository.FindCollectSourceById(id)
		if source == nil {
			firstErr = errors.New("采集站信息不存在")
			break
		}
		if source.State == state {
			continue
		}
		next := *source
		next.State = state
		if err := s.updateFilmSourceCore(next); err != nil {
			firstErr = err
			break
		}
		changed = true
	}
	if changed {
		onPrimarySourceMaybeChanged(oldPrimary, repository.PickPrimarySourceForCategory())
	}
	return firstErr
}

func (s *CollectService) SaveFilmSource(source model.FilmSource) error {
	oldPrimary := repository.PickPrimarySourceForCategory()
	if source.Id == "" {
		source.Id = utils.GenerateHashKey(source.Uri)
	}
	if err := repository.AddCollectSource(source); err != nil {
		return err
	}
	spider.ClearLimiter(source.Id)
	clearProvideNetworkConfigCache()
	filmsnapshot.ClearDynamicPlayCaches()
	onPrimarySourceMaybeChanged(oldPrimary, repository.PickPrimarySourceForCategory())
	return nil
}

var deleteSourceQueue = make(chan string, 1000)

func init() {
	go runDeleteSourceWorker()
}

func runDeleteSourceWorker() {
	for id := range deleteSourceQueue {
		processAsyncDeleteSource(id)
	}
}

func processAsyncDeleteSource(id string) {
	defer progress.SetSourceDeleting(id, false)
	if err := CollectSvc.DelFilmSource(id); err != nil {
		syslog.Errorf("[Collect] 异步删除站点 %s 失败: %v", id, err)
		progress.BroadcastSourceDeleteStatus(id, "failed", err.Error())
		return
	}
	progress.BroadcastSourceDeleteStatus(id, "deleted", "")
}

// TriggerDelFilmSource 异步触发采集站删除：广播 deleting 状态并毫秒级返回，后台串行队列安全停工与级联清理
func (s *CollectService) TriggerDelFilmSource(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("采集站ID不能为空")
	}
	src := repository.FindCollectSourceForDelete(id)
	if src == nil {
		return errors.New("当前资源站信息不存在, 请勿重复操作")
	}
	if progress.IsSourceDeleting(id) {
		return nil
	}

	select {
	case deleteSourceQueue <- id:
		progress.BroadcastSourceDeleteStatus(id, "deleting", "")
		return nil
	default:
		return errors.New("删除队列已满")
	}
}

// TriggerDelFilmSourceBatch 异步批量触发采集站删除。有一站失败就返回错误。
func (s *CollectService) TriggerDelFilmSourceBatch(ids []string) error {
	var failed []string
	for _, id := range ids {
		if err := s.TriggerDelFilmSource(id); err != nil {
			failed = append(failed, err.Error())
		}
	}
	if len(failed) > 0 {
		return errors.New(strings.Join(failed, "；"))
	}
	return nil
}

func (s *CollectService) DelFilmSource(id string) error {
	err := spider.WithSourceExclusive(id, func() error {
		src := repository.FindCollectSourceById(id)
		if src == nil {
			return errors.New("当前资源站信息不存在, 请勿重复操作")
		}
		// 采集中、停止中、分类采集中都不拦截。先停采集，等本次运行退出后再删。
		release, err := spider.StopSourceForDelete(id)
		if err != nil {
			return err
		}
		defer release()
		oldPrimary := repository.PickPrimarySourceForCategory()
		orphanMids, err := repository.DelCollectResource(id)
		if len(orphanMids) > 0 {
			filmsnapshot.DeleteActiveSnapshotsByMids(orphanMids...)
			syslog.Infof("[Collect] 删除采集源 %s(%s)，已删除该站档案 %d 部", src.Name, id, len(orphanMids))
		}
		if err != nil {
			return err
		}
		spider.ClearLimiter(id)
		clearProvideNetworkConfigCache()
		filmsnapshot.ClearDynamicPlayCaches()
		repository.MarkCategoryChanged()
		progress.BroadcastCategoryStatus(id, "idle")
		onPrimarySourceMaybeChanged(oldPrimary, repository.PickPrimarySourceForCategory())
		return nil
	})
	if err != nil {
		return err
	}
	spider.DropSourceRuntime(id)
	return nil
}

// PrepareRecordQuery 未指定采集源时落到首选站。没有站点时保持空条件。
func (s *CollectService) PrepareRecordQuery(params model.RecordRequestVo) model.RecordRequestVo {
	params.OriginId = strings.TrimSpace(params.OriginId)
	if params.OriginId == "" {
		if active := repository.GetActiveCollectSource(); active != nil {
			params.OriginId = active.Id
		}
	}
	return params
}

func (s *CollectService) GetRecordList(params model.RecordRequestVo) ([]model.FailureRecord, error) {
	params = s.PrepareRecordQuery(params)
	return repository.FailureRecordList(params)
}

func (s *CollectService) GetRecordOptions() (map[string]any, error) {
	sources, err := repository.GetCollectSourceList()
	if err != nil {
		return nil, err
	}
	defaultSourceId := ""
	if active := repository.GetActiveCollectSource(); active != nil {
		defaultSourceId = active.Id
	} else if len(sources) > 0 {
		defaultSourceId = sources[0].Id
	}
	originOptions := make([]model.Option, 0, len(sources))
	for _, v := range sources {
		name := v.Name
		if !v.State {
			name += " (已停用)"
		}
		originOptions = append(originOptions, model.Option{Name: name, Value: v.Id})
	}
	return map[string]any{
		"status": []model.Option{
			{Name: "全部", Value: -1},
			{Name: "待重试", Value: model.FailureRecordStatusPending},
			{Name: "重试成功", Value: model.FailureRecordStatusSuccess},
			{Name: "重试失败", Value: model.FailureRecordStatusFailed},
		},
		"origin":          originOptions,
		"defaultSourceId": defaultSourceId,
	}, nil
}

func (s *CollectService) CollectRecover(id int) error {
	return spider.AcceptFailureRecover(repository.FindRecordById(uint(id)))
}

func (s *CollectService) RecoverAll() (spider.FailureRecoverOutcome, error) {
	return spider.EnqueuePendingFailureRecovers()
}

func (s *CollectService) ClearRetriedRecords() error {
	return repository.DeleteRetriedRecords()
}

func (s *CollectService) ClearAllRecord() error {
	return repository.TruncateRecordTable()
}
