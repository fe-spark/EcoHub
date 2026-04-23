package service

import (
	"errors"
	"log"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/spider"

	"gorm.io/gorm"
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
	masters := repository.GetCollectSourceListByGrade(model.MasterCollect)

	// 1. 安全校验：如果有任何采集任务正在运行，禁止修改等级或 URI，防止引发元数据清空冲突
	isGradeChanged := old.Grade != source.Grade
	isUriChanged := old.Uri != source.Uri
	if (isGradeChanged || isUriChanged) && spider.IsAnyTaskRunning() {
		return errors.New("当前有采集任务正在运行，请先停止所有任务后再执行等级或地址变更操作")
	}

	// 2. 强制单主站机制：如果新等级设为主站，则自动将旧主站降级
	if source.Grade == model.MasterCollect && old.Grade != model.MasterCollect {
		log.Printf("[Collect] 站点 %s 提升为主采集站，清理其旧有附属站播放列表并降级现有主站...", source.Name)
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
	}

	affectedSourceIDs := make([]string, 0, len(masters)+1)
	for _, master := range masters {
		affectedSourceIDs = append(affectedSourceIDs, master.Id)
	}
	affectedSourceIDs = append(affectedSourceIDs, source.Id)

	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if source.Grade == model.MasterCollect && old.Grade != model.MasterCollect {
			if err := filmrepo.DeletePlaylistBySourceIdTx(tx, source.Id); err != nil {
				log.Printf("[Collect] 清理站点 %s 的旧有播放列表失败: %v", source.Name, err)
				return errors.New("清理新主站旧附属站数据失败，请重试")
			}

			if err := repository.DemoteExistingMasterTx(tx); err != nil {
				log.Printf("[Collect] 自动降级旧主站失败: %v", err)
				return errors.New("主站自动降级失败，请重试")
			}
		}

		if masterLookup || masterUriChanged {
			if err := filmrepo.ClearMasterDataBySourceIDsTx(tx, affectedSourceIDs...); err != nil {
				log.Printf("[Collect] 主站切换数据清理失败: %v", err)
				return errors.New("主站切换数据清理失败，请重试")
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

	if masterLookup || masterUriChanged {
		filmrepo.RefreshMasterDataCaches()
	}
	return nil
}

func (s *CollectService) SaveFilmSource(source model.FilmSource) error {
	// 强制单主站机制：如果新增站点为主站，自动降级现有主站
	if source.Grade == model.MasterCollect {
		log.Printf("[Collect] 新增站点 %s 为主采集站，自动降级现有主站...", source.Name)
		if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
			if err := repository.DemoteExistingMasterTx(tx); err != nil {
				return err
			}
			return repository.AddCollectSourceTx(tx, source)
		}); err != nil {
			return err
		}
		spider.ClearLimiter(source.Id)
		return nil
	}
	if err := repository.AddCollectSource(source); err != nil {
		return err
	}
	spider.ClearLimiter(source.Id)
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
	return nil
}

func (s *CollectService) GetRecordList(params model.RecordRequestVo) []model.FailureRecord {
	return repository.FailureRecordList(params)
}

func (s *CollectService) GetRecordOptions() model.OptionGroup {
	options := make(model.OptionGroup)
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
