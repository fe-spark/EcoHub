package service

import (
	"errors"
	"fmt"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider"
	"server/internal/utils"

	"gorm.io/gorm"
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

func (s *CollectService) GetFilmSourceList() []model.FilmSourceListItem {
	sources := repository.GetCollectSourceList()
	list := make([]model.FilmSourceListItem, 0, len(sources))
	progressByID := make(map[string]model.CollectProgress)
	for _, progress := range spider.GetActiveTaskProgress() {
		progressByID[progress.Id] = progress
	}
	lastCollectTimeByID := getLastCollectTimeBySource(sources)
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
		item := model.FilmSourceListItem{FilmSource: source, LastCollectTime: lastCollectTimeByID[source.Id]}
		if progress, ok := progressByID[source.Id]; ok {
			item.Progress = &progress
		}
		if ok, _ := repository.ResolveSourceProxy(source.Id); ok {
			item.ProxyEnabled = true
		}
		list = append(list, item)
	}
	return list
}

func (s *CollectService) SetPrimaryFilmSource(id string) error {
	src := repository.FindCollectSourceById(id)
	if src == nil {
		return errors.New("采集源不存在")
	}
	if err := repository.SetPrimaryCollectSource(id); err != nil {
		return err
	}

	// 切换主站时获取对应采集源的分类
	if err := spider.CollectCategory(src); err != nil {
		syslog.Warnf("[CollectService] 切换主站时获取对应采集源分类失败 name=%s uri=%s: %v", src.Name, src.Uri, err)
	}

	repository.MarkCategoryChanged()
	filmsnapshot.ClearAllSnapshotDynamicCaches()
	return nil
}

func getLastCollectTimeBySource(sources []model.FilmSource) map[string]*time.Time {
	sourceIDs := make([]string, 0, len(sources))
	for _, source := range sources {
		if source.Id != "" {
			sourceIDs = append(sourceIDs, source.Id)
		}
	}
	return repository.GetCollectSourceStats(sourceIDs)
}

func (s *CollectService) GetFilmSource(id string) *model.FilmSource {
	return repository.FindCollectSourceById(id)
}

func (s *CollectService) GetEnabledFilmSources() []model.FilmSource {
	return repository.GetEnabledCollectSourceList()
}

func (s *CollectService) GetAllFilmSources() []model.FilmSource {
	return repository.GetCollectSourceList()
}

// UpdateFilmSource 编辑采集源配置（单源），发生变更时发送 source_config_changed 通知。
func (s *CollectService) UpdateFilmSource(source model.FilmSource) error {
	return s.updateFilmSource(source, nil)
}

// updateFilmSource 编辑采集源配置核心逻辑。
// collector 非 nil 时（批量操作）不直接发送通知，而是把各源变更追加到收集器，
// 由调用方统一发送聚合通知，避免批量操作逐源轰炸。
func (s *CollectService) updateFilmSource(source model.FilmSource, collector *[]notify.SourceConfigChangeItem) error {
	old := repository.FindCollectSourceById(source.Id)
	if old == nil {
		return errors.New("采集站信息不存在")
	}
	if source.CreatedAt.IsZero() && !old.CreatedAt.IsZero() {
		source.CreatedAt = old.CreatedAt
	}

	// 安全校验：如果有任何采集任务正在运行，禁止修改 URI 或数据协议，防止引发解析错乱
	isUriChanged := old.Uri != source.Uri
	isFormatChanged := old.ResolveFormat() != source.ResolveFormat()
	if (isUriChanged || isFormatChanged) && spider.IsAnyTaskRunning() {
		return errors.New("当前有采集任务正在运行，请先停止所有任务后再执行地址或协议变更操作")
	}

	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		// 接口地址或数据格式变更时同步清空该源站的历史失败采集记录，避免使用新接口拉取旧页码导致数据错乱
		if isUriChanged || isFormatChanged {
			if err := repository.DeleteFailureRecordsByOriginIdTx(tx, source.Id); err != nil {
				syslog.Errorf("[Collect] 清理变更源关联失败记录失败: %v", err)
				return errors.New("清理原失败记录失败，请重试")
			}
		}

		return repository.UpdateCollectSourceTx(tx, source)
	})
	if err != nil {
		return err
	}

	spider.ClearLimiter(source.Id)
	if old.State && !source.State {
		spider.StopTask(source.Id)
	}

	clearProvideNetworkConfigCache()
	if old.DomainReplaceRules != source.DomainReplaceRules {
		filmsnapshot.ClearDynamicPlayCaches()
	}
	if changes := sourceChangeLabels(*old, source); len(changes) > 0 {
		notifySourceConfigChanged(source.Name, source.Id, changes, collector)
	}
	return nil
}

// notifySourceConfigChanged 单条源配置变更通知：批量收集器非空时累积到收集器，否则立即发送。
func notifySourceConfigChanged(sourceName, sourceID string, changes []string, collector *[]notify.SourceConfigChangeItem) {
	if collector != nil {
		*collector = append(*collector, notify.SourceConfigChangeItem{
			SourceName: sourceName,
			SourceID:   sourceID,
			Changes:    changes,
		})
		return
	}
	notify.PublishSourceConfigChanged(sourceName, sourceID, changes)
}

// sourceChangeLabels 对比 old→next 生成源配置变更描述；无差异时返回 nil。
func sourceChangeLabels(old, next model.FilmSource) []string {
	var changes []string
	if old.State != next.State {
		changes = append(changes, fmt.Sprintf("启用状态: %s → %s", sourceStateLabel(old.State), sourceStateLabel(next.State)))
	}
	if old.Weight != next.Weight {
		changes = append(changes, fmt.Sprintf("播放权重: %d → %d", old.Weight, next.Weight))
	}
	if old.ResolveFormat() != next.ResolveFormat() {
		changes = append(changes, fmt.Sprintf("接口格式: %s → %s", strings.ToUpper(old.ResolveFormat()), strings.ToUpper(next.ResolveFormat())))
	}
	if old.Uri != next.Uri {
		changes = append(changes, "接口地址已变更")
	}
	if old.Name != next.Name {
		changes = append(changes, fmt.Sprintf("站点名称: %s → %s", old.Name, next.Name))
	}
	if old.Interval != next.Interval {
		changes = append(changes, fmt.Sprintf("请求间隔: %dms → %dms", old.Interval, next.Interval))
	}
	if old.Cd != next.Cd {
		changes = append(changes, fmt.Sprintf("采集时长: %d小时 → %d小时", old.Cd, next.Cd))
	}
	if old.IsPosterSource != next.IsPosterSource {
		if next.IsPosterSource {
			changes = append(changes, "海报图源: 设为优先海报图源")
		} else {
			changes = append(changes, "海报图源: 取消优先海报图源")
		}
	}
	if old.DomainReplaceRules != next.DomainReplaceRules {
		changes = append(changes, "播放链接域名替换规则已更新")
	}
	return changes
}

func sourceStateLabel(on bool) string {
	if on {
		return "已启用"
	}
	return "已停用"
}

func (s *CollectService) BatchUpdateFilmSourceState(ids []string, state bool) error {
	var collector []notify.SourceConfigChangeItem
	var firstErr error
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
		if err := s.updateFilmSource(next, &collector); err != nil {
			firstErr = err
			break
		}
	}
	if len(collector) > 0 {
		notify.PublishSourceConfigsChanged(collector)
	}
	return firstErr
}

func (s *CollectService) SaveFilmSource(source model.FilmSource) error {
	if source.Id == "" {
		source.Id = utils.GenerateHashKey(source.Uri)
	}
	if err := repository.AddCollectSource(source); err != nil {
		return err
	}
	spider.ClearLimiter(source.Id)
	clearProvideNetworkConfigCache()
	notify.PublishSourceConfigChanged(source.Name, source.Id, []string{"新增采集源"})
	return nil
}

func (s *CollectService) DelFilmSource(id string) error {
	src := repository.FindCollectSourceById(id)
	if src == nil {
		return errors.New("当前资源站信息不存在, 请勿重复操作")
	}
	if err := repository.DelCollectResource(id); err != nil {
		return err
	}
	spider.ClearLimiter(id)
	clearProvideNetworkConfigCache()
	notify.PublishSourceConfigChanged(src.Name, src.Id, []string{"删除采集源"})
	removeSourceFromProxyConfig(id)
	return nil
}

func (s *CollectService) GetRecordList(params model.RecordRequestVo) []model.FailureRecord {
	repository.NormalizeFailureRecordsRetryCount()
	return repository.FailureRecordList(params)
}

func (s *CollectService) GetRecordOptions() model.OptionGroup {
	options := make(model.OptionGroup)
	options["status"] = []model.Option{
		{Name: "全部", Value: -1},
		{Name: "待重试", Value: model.FailureRecordStatusPending},
		{Name: "重试成功", Value: model.FailureRecordStatusSuccess},
		{Name: "重试失败", Value: model.FailureRecordStatusFailed},
	}

	originOptions := []model.Option{{Name: "全部", Value: ""}}
	for _, v := range repository.GetCollectSourceList() {
		originOptions = append(originOptions, model.Option{Name: v.Name, Value: v.Id})
	}
	options["origin"] = originOptions
	return options
}

func (s *CollectService) CollectRecover(id int) error {
	fr := repository.FindRecordById(uint(id))
	if fr == nil {
		return errors.New("采集重试执行失败: 失败记录信息获取异常")
	}
	if fr.Status == model.FailureRecordStatusFailed {
		_ = repository.UpdateFailureRecordStatusByID(fr.ID, model.FailureRecordStatusPending)
		fr.Status = model.FailureRecordStatusPending
	}
	go spider.SingleRecoverSpider(fr)
	return nil
}

func (s *CollectService) RecoverAll() {
	go spider.FullRecoverSpider()
}

func (s *CollectService) ClearRetriedRecords() {
	repository.DeleteRetriedRecords()
}

func (s *CollectService) ClearAllRecord() {
	repository.TruncateRecordTable()
}

func removeSourceFromProxyConfig(sourceID string) {
	if sourceID == "" {
		return
	}
	cfg := repository.GetProxyConfig()
	var newIds []string
	changed := false
	for _, id := range cfg.SourceIds {
		if id == sourceID {
			changed = true
		} else {
			newIds = append(newIds, id)
		}
	}
	if changed {
		cfg.SourceIds = newIds
		_ = repository.SaveProxyConfig(cfg)
	}
}
