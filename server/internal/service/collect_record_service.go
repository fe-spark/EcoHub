package service

import (
	"errors"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider"
)

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
