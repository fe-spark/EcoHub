package service

import (
	"errors"
	"log"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider"
)

type CollectService struct{}

var CollectSvc = new(CollectService)

func (s *CollectService) GetFilmSourceList() []model.FilmSource {
	return repository.GetCollectSourceList()
}

func (s *CollectService) GetFilmSource(id string) *model.FilmSource {
	return repository.FindCollectSourceById(id)
}

func (s *CollectService) UpdateFilmSource(source model.FilmSource) error {
	old := repository.FindCollectSourceById(source.Id)
	if old == nil {
		return errors.New("采集站信息不存在")
	}

	// 1. 安全校验：如果有任何采集任务正在运行，禁止修改等级或 URI，防止引发元数据清空冲突
	isGradeChanged := old.Grade != source.Grade
	isUriChanged := old.Uri != source.Uri
	if (isGradeChanged || isUriChanged) && spider.IsAnyTaskRunning() {
		return errors.New("当前有采集任务正在运行，请先停止所有任务后再执行等级或地址变更操作")
	}

	// 2. 强制单主站机制：如果新等级设为主站，则自动将旧主站降级
	if source.Grade == model.MasterCollect && old.Grade != model.MasterCollect {
		log.Printf("[Collect] 站点 %s 提升为主采集站，后台异步清理其旧有播放列表数据并降级现有主站...", source.Name)
		// 异步清理该站点在作为附属站时期采集的所有播放列表，防止阻塞 API 几十秒（MySQL DELETE 数据量大时极慢）
		go func(sid string) {
			_ = repository.DeletePlaylistBySourceId(sid)
			log.Printf("[Collect] 站点 %s 的旧有播放列表数据清理完成", sid)
		}(source.Id)

		if err := repository.DemoteExistingMaster(); err != nil {
			log.Printf("[Collect] 自动降级旧主站失败: %v", err)
			return errors.New("主站自动降级失败，请重试")
		}
	}

	// 3. 检测主站切换并清理数据
	// 情况A: 原来是附属站、现在升级为主站
	masterLookup := old.Grade == model.SlaveCollect && source.Grade == model.MasterCollect
	// 情况B: 依然是主站，但 URI 发生变更
	masterUriChanged := old.Grade == model.MasterCollect && source.Grade == model.MasterCollect && old.Uri != source.Uri

	if masterLookup || masterUriChanged {
		log.Printf("[Collect] 检测到主站变更 (lookup=%v, uriChanged=%v)，进行数据重置...", masterLookup, masterUriChanged)
		// 强制中断所有任务（双重保险）
		spider.StopAllTasks()
		repository.MasterFilmZero()
	}

	return repository.UpdateCollectSource(source)
}

func (s *CollectService) SaveFilmSource(source model.FilmSource) error {
	// 强制单主站机制：如果新增站点为主站，自动降级现有主站
	if source.Grade == model.MasterCollect {
		log.Printf("[Collect] 新增站点 %s 为主采集站，自动降级现有主站...", source.Name)
		_ = repository.DemoteExistingMaster()
	}
	return repository.AddCollectSource(source)
}

func (s *CollectService) DelFilmSource(id string) error {
	src := repository.FindCollectSourceById(id)
	if src == nil {
		return errors.New("当前资源站信息不存在, 请勿重复操作")
	}
	if src.Grade == model.MasterCollect {
		return errors.New("主站点无法直接删除, 请先降级为附属站点再进行删除")
	}
	return repository.DelCollectResource(id)
}

func (s *CollectService) GetRecordList(params model.RecordRequestVo) []model.FailureRecord {
	return repository.FailureRecordList(params)
}

func (s *CollectService) GetRecordOptions() model.OptionGroup {
	options := make(model.OptionGroup)
	options["collectType"] = []model.Option{{Name: "全部", Value: -1}, {Name: "影片详情", Value: 0}, {Name: "文章", Value: 1}, {Name: "演员", Value: 2}, {Name: "角色", Value: 3}, {Name: "网站", Value: 4}}
	options["status"] = []model.Option{{Name: "全部", Value: -1}, {Name: "待重试", Value: 1}, {Name: "已处理", Value: 0}}

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

func (s *CollectService) ClearDoneRecord() {
	repository.DelDoneRecord()
}

func (s *CollectService) ClearAllRecord() {
	repository.TruncateRecordTable()
}
