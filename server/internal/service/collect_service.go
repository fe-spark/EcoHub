package service

import (
	"errors"
	"fmt"
	"log"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/spider"
	"server/internal/utils"

	"gorm.io/gorm"
)

type CollectService struct{}

var CollectSvc = new(CollectService)

func clearProvideNetworkConfigCache() {
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
	for _, source := range sources {
		item := model.FilmSourceListItem{FilmSource: source, LastCollectTime: lastCollectTimeByID[source.Id]}
		if progress, ok := progressByID[source.Id]; ok {
			item.Progress = &progress
		}
		list = append(list, item)
	}
	return list
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
	masters := repository.GetCollectSourceListByGrade(model.MasterCollect)

	// 1. 安全校验：如果有任何采集任务正在运行，禁止修改等级或 URI，防止引发元数据清空冲突
	isGradeChanged := old.Grade != source.Grade
	isUriChanged := old.Uri != source.Uri
	if (isGradeChanged || isUriChanged) && spider.IsAnyTaskRunning() {
		return errors.New("当前有采集任务正在运行，请先停止所有任务后再执行等级或地址变更操作")
	}

	// 2. 强制单主站机制：如果新等级设为主站，则自动将旧主站降级
	if source.Grade == model.MasterCollect && old.Grade != model.MasterCollect {
		log.Printf("[Collect] 站点 %s 提升为主采集站，保留附属站播放列表并降级现有主站...", source.Name)
	}

	// 3. 检测主站切换并清理数据
	// 情况A: 原来是附属站、现在升级为主站
	masterLookup := old.Grade == model.SlaveCollect && source.Grade == model.MasterCollect
	// 情况B: 依然是主站，但 URI 发生变更
	masterUriChanged := old.Grade == model.MasterCollect && source.Grade == model.MasterCollect && old.Uri != source.Uri
	// 情况C: 原来是主站，现在降级为附属站
	masterDowngrade := old.Grade == model.MasterCollect && source.Grade != model.MasterCollect

	if masterLookup || masterUriChanged || masterDowngrade {
		log.Printf("[Collect] 检测到主站变更 (lookup=%v, uriChanged=%v, downgrade=%v)，进行数据重置...", masterLookup, masterUriChanged, masterDowngrade)
		// 强制中断所有任务（双重保险）
		spider.StopAllTasks()
	}

	affectedSourceIDs := make([]string, 0, len(masters)+2)
	for _, master := range masters {
		affectedSourceIDs = append(affectedSourceIDs, master.Id)
	}
	affectedSourceIDs = append(affectedSourceIDs, source.Id)
	if masterDowngrade {
		affectedSourceIDs = append(affectedSourceIDs, old.Id)
	}

	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if masterLookup {
			if err := repository.DemoteExistingMasterTx(tx); err != nil {
				syslog.Errorf("[Collect] 自动降级旧主站失败: %v", err)
				return errors.New("主站自动降级失败，请重试")
			}
		}

		return repository.UpdateCollectSourceTx(tx, source)
	})
	if err != nil {
		return err
	}

	if masterLookup || masterUriChanged || masterDowngrade {
		if err := filmrepo.ClearMasterDataBySourceIDsFast(affectedSourceIDs...); err != nil {
			syslog.Errorf("[Collect] 主站切换数据清理失败: %v", err)
			return errors.New("主站切换数据清理失败，请重试")
		}
	}

	spider.ClearLimiter(source.Id)
	if old.State && !source.State {
		spider.StopTask(source.Id)
	}

	// 分类树只跟主站走（主站未启用也可同步）；无主站则不同步、不从附属站构建
	if masterLookup || masterUriChanged {
		if source.Grade == model.MasterCollect {
			if syncErr := SpiderSvc.SyncMasterCategoryTree(); syncErr != nil {
				return syncErr
			}
		}
	}
	// 主站启用/停用变更：停用后仍可用该主站 URI 同步分类；降级后若无主站则 Sync 会失败（符合「无主站无分类树」）
	if source.Grade == model.MasterCollect && old.State != source.State {
		if syncErr := SpiderSvc.SyncMasterCategoryTree(); syncErr != nil {
			return syncErr
		}
	}
	clearProvideNetworkConfigCache()
	if changes := sourceChangeLabels(*old, source); len(changes) > 0 {
		notifySourceConfigChanged(source.Name, source.Id, changes, collector)
	}
	// masterLookup：旧主站被自动降级且主数据已清空，单独通知，避免切换静默
	if masterLookup {
		for _, m := range masters {
			if m.Id == source.Id {
				continue
			}
			notifySourceConfigChanged(m.Name, m.Id, []string{"原主站已降级为附属站，主站数据已清空"}, collector)
		}
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
	if old.Grade != next.Grade {
		changes = append(changes, fmt.Sprintf("站点类型: %s → %s", sourceGradeLabel(old.Grade), sourceGradeLabel(next.Grade)))
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
	return changes
}

func sourceStateLabel(on bool) string {
	if on {
		return "已启用"
	}
	return "已停用"
}

func sourceGradeLabel(g model.SourceGrade) string {
	if g == model.MasterCollect {
		return "主站"
	}
	return "附属站"
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
	// 全成功或部分成功均发送已收集的变更，避免中途失败时静默丢失已生效的源
	if len(collector) > 0 {
		notify.PublishSourceConfigsChanged(collector)
	}
	return firstErr
}

// MaxCollectSources 采集站数量上限（与前端 MAX_COLLECT_SOURCES 一致）
const MaxCollectSources = 12

func (s *CollectService) SaveFilmSource(source model.FilmSource) error {
	// 新增时校验总数上限（更新走 Update 不经过此路径）
	existing := repository.GetCollectSourceList()
	if len(existing) >= MaxCollectSources {
		return fmt.Errorf("采集站数量已达上限（%d 个），请先删除不用的采集站", MaxCollectSources)
	}

	// 强制单主站机制：如果新增站点为主站，自动降级现有主站
	if source.Grade == model.MasterCollect {
		if source.Id == "" {
			source.Id = utils.GenerateHashKey(source.Uri)
		}
		masters := repository.GetCollectSourceListByGrade(model.MasterCollect)
		affectedSourceIDs := make([]string, 0, len(masters)+1)
		for _, master := range masters {
			affectedSourceIDs = append(affectedSourceIDs, master.Id)
		}
		affectedSourceIDs = append(affectedSourceIDs, source.Id)

		log.Printf("[Collect] 新增站点 %s 为主采集站，自动降级现有主站...", source.Name)
		if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
			if err := repository.DemoteExistingMasterTx(tx); err != nil {
				return err
			}
			return repository.AddCollectSourceTx(tx, source)
		}); err != nil {
			return err
		}
		if err := filmrepo.ClearMasterDataBySourceIDsFast(affectedSourceIDs...); err != nil {
			syslog.Errorf("[Collect] 新主站接管前数据清理失败: %v", err)
			return errors.New("主站切换数据清理失败，请重试")
		}
		spider.ClearLimiter(source.Id)
		// 新增主站即同步分类树（未启用也要同步；分类树只认主站）
		if syncErr := SpiderSvc.SyncMasterCategoryTree(); syncErr != nil {
			return syncErr
		}
		clearProvideNetworkConfigCache()
		notify.PublishSourceConfigChanged(source.Name, source.Id, []string{"新增采集源（主站）"})
		// 现有主站被自动降级且主数据已清空，单独通知
		for _, m := range masters {
			notify.PublishSourceConfigChanged(m.Name, m.Id, []string{"原主站已降级为附属站，主站数据已清空"})
		}
		return nil
	}
	// 附属站新增：与主站分支一致，先基于 URI 生成稳定 ID，供通知限流 key 与消息展示使用。
	if source.Id == "" {
		source.Id = utils.GenerateHashKey(source.Uri)
	}
	if err := repository.AddCollectSource(source); err != nil {
		return err
	}
	spider.ClearLimiter(source.Id)
	clearProvideNetworkConfigCache()
	notify.PublishSourceConfigChanged(source.Name, source.Id, []string{"新增采集源（附属站）"})
	return nil
}

func (s *CollectService) DelFilmSource(id string) error {
	src := repository.FindCollectSourceById(id)
	if src == nil {
		return errors.New("当前资源站信息不存在, 请勿重复操作")
	}
	if src.Grade == model.MasterCollect {
		return errors.New("主站点无法直接删除, 请先降级为附属站点再进行删除")
	}
	if err := repository.DelCollectResource(id); err != nil {
		return err
	}
	spider.ClearLimiter(id)
	clearProvideNetworkConfigCache()
	notify.PublishSourceConfigChanged(src.Name, src.Id, []string{"删除采集源"})
	return nil
}

func (s *CollectService) GetRecordList(params model.RecordRequestVo) []model.FailureRecord {
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
