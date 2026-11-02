package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider"
)

type CollectRecordService struct{}

var CollectRecordSvc = new(CollectRecordService)

// GetHistoryList 分页获取采集运行记录
func (s *CollectRecordService) GetHistoryList(q model.CollectRecordQuery) ([]model.CollectRecord, int64, error) {
	return repository.GetCollectRecordList(q)
}

func (s *CollectRecordService) ListOpenIncidents(now time.Time) ([]model.SourceIncident, error) {
	return repository.ListOpenSourceIncidents(now)
}

// DeleteHistory 批量删除指定 ID 列表的历史记录
func (s *CollectRecordService) DeleteHistory(ids []uint) error {
	return repository.DeleteCollectRecords(ids)
}

// ClearHistory 清空全部历史记录
func (s *CollectRecordService) ClearHistory() error {
	return repository.ClearCollectRecords()
}

// RetryBatchFailedSources 就地重试某批次中失败的采集站
func (s *CollectRecordService) RetryBatchFailedSources(req model.CollectRecordRetryReq) (model.CollectBatchResult, error) {
	batchID := strings.TrimSpace(req.BatchID)
	if batchID == "" {
		return model.CollectBatchResult{}, errors.New("批次号不能为空")
	}

	rec, err := repository.GetCollectRecordByBatchID(batchID)
	if err != nil || rec == nil {
		return model.CollectBatchResult{}, errors.New("未找到对应采集记录")
	}

	// 从明细解析失败站点
	failedSourceMap := make(map[string]bool)
	if rec.DetailJson != "" {
		var details []model.CollectRecordSourceDetail
		if err := json.Unmarshal([]byte(rec.DetailJson), &details); err != nil {
			return model.CollectBatchResult{}, fmt.Errorf("批次明细损坏: %w", err)
		}
		for _, d := range details {
			if d.Status == "failed" && strings.TrimSpace(d.SourceID) != "" {
				failedSourceMap[d.SourceID] = true
			}
		}
	}

	targetIDs := make([]string, 0)
	if len(req.SourceIDs) > 0 {
		seen := make(map[string]struct{})
		for _, id := range req.SourceIDs {
			id = strings.TrimSpace(id)
			if id != "" {
				if _, ok := seen[id]; !ok {
					seen[id] = struct{}{}
					if failedSourceMap[id] {
						targetIDs = append(targetIDs, id)
					}
				}
			}
		}
		if len(targetIDs) == 0 {
			return model.CollectBatchResult{}, errors.New("所选站点未在本次失败记录中")
		}
	} else {
		for sid := range failedSourceMap {
			targetIDs = append(targetIDs, sid)
		}
	}

	if len(targetIDs) == 0 {
		return model.CollectBatchResult{}, errors.New("该批次没有需要重试的失败站点")
	}

	sources, result := spider.PrepareBatchCollectStart(targetIDs)
	if len(sources) == 0 {
		if len(result.Skipped) > 0 {
			reasons := make([]string, 0, len(result.Skipped))
			for _, sk := range result.Skipped {
				reasons = append(reasons, fmt.Sprintf("%s: %s", sk.Name, sk.Reason))
			}
			return result, fmt.Errorf("无法启动重试: %s", strings.Join(reasons, "; "))
		}
		return result, errors.New("所选站点当前无法启动重试")
	}

	retryBatchID, err := spider.StartBatchCollectRetry(rec.BatchID, rec.Hour, sources)
	if err != nil {
		return result, err
	}
	result.BatchID = retryBatchID
	return result, nil
}
