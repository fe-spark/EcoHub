package collectctl

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"server/internal/utils"
	"sort"
	"strings"
	"time"

	"server/internal/collect"
	"server/internal/collect/ledger"
	"server/internal/model"
)

type CollectRecordService struct{}

var CollectRecordSvc = new(CollectRecordService)

// 单片重放默认走采集器。测试可替换，避免重试请求打到外站。
var replayCollectFilms = collect.CollectFilms

// GetHistoryList 分页获取采集运行记录
func (s *CollectRecordService) GetHistoryList(q model.CollectRecordQuery) ([]model.CollectRecord, int64, error) {
	list, total, err := ledger.GetCollectRecordList(q)
	if err != nil || len(list) == 0 {
		return list, total, err
	}
	if err := attachSingleFilmNames(list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func parseTargetMids(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	var ids []int64
	if err := json.Unmarshal([]byte(raw), &ids); err != nil {
		return nil
	}
	seen := make(map[int64]struct{}, len(ids))
	out := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

func attachSingleFilmNames(list []model.CollectRecord) error {
	ids := make([]int64, 0)
	seen := make(map[int64]struct{})
	parsed := make([][]int64, len(list))
	for i := range list {
		if list[i].Trigger != model.CollectTriggerSingleUpdate && list[i].Tag != model.CollectTagSingleUpdate {
			continue
		}
		rowIDs := parseTargetMids(list[i].TargetMids)
		if len(rowIDs) == 0 {
			continue
		}
		parsed[i] = rowIDs
		for _, id := range rowIDs {
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			ids = append(ids, id)
		}
	}
	if len(ids) == 0 {
		return nil
	}
	names, err := ledger.FilmNamesByIDs(ids)
	if err != nil {
		return err
	}
	for i := range list {
		if len(parsed[i]) == 0 {
			continue
		}
		parts := make([]string, 0, len(parsed[i]))
		films := make([]model.CollectRecordFilm, 0, len(parsed[i]))
		for _, id := range parsed[i] {
			name := names[id]
			if name == "" {
				name = fmt.Sprintf("影片 #%d", id)
			}
			parts = append(parts, name)
			films = append(films, model.CollectRecordFilm{Mid: id, Name: name})
		}
		list[i].FilmName = strings.Join(parts, "、")
		list[i].Films = films
	}
	return nil
}

// DeleteHistory 批量删除指定 ID 列表的历史记录
func (s *CollectRecordService) DeleteHistory(ids []uint) error {
	return ledger.DeleteCollectRecords(ids)
}

// ClearHistory 清空全部历史记录
func (s *CollectRecordService) ClearHistory() error {
	return ledger.ClearCollectRecords()
}

// RetryBatchFailedSources 就地重试某批次中失败的采集站
func (s *CollectRecordService) RetryBatchFailedSources(req model.CollectRecordRetryReq) (model.CollectBatchResult, error) {
	batchID := strings.TrimSpace(req.BatchID)
	if batchID == "" {
		return model.CollectBatchResult{}, errors.New("批次号不能为空")
	}

	rec, err := ledger.GetCollectRecordByBatchID(batchID)
	if err != nil || rec == nil {
		return model.CollectBatchResult{}, errors.New("未找到对应采集记录")
	}

	// 单片更新没有时间窗口。按记录里的 target_mids 重放，不能走小时采集。
	if rec.Trigger == model.CollectTriggerSingleUpdate || rec.Tag == model.CollectTagSingleUpdate {
		ids := parseTargetMids(rec.TargetMids)
		if len(ids) == 0 {
			return model.CollectBatchResult{}, errors.New("单片更新缺少影片编号，无法重试")
		}
		utils.GoSafe("单片更新重试", func() { replayCollectFilms(ids) })
		return model.CollectBatchResult{}, nil
	}

	// 从明细解析失败站点，以及各站实际拉过的小时窗口。
	failedSourceMap, hourBySource, failedPagesBySource, err := parseRetryDetail(rec.DetailJson)
	if err != nil {
		return model.CollectBatchResult{}, err
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

	// 半小时内可以重试失败页，超过直接变成重试采集站
	refTime := rec.StartedAt
	if rec.FinishedAt != nil && !rec.FinishedAt.IsZero() {
		refTime = *rec.FinishedAt
	}
	elapsed := time.Since(refTime)
	isWithinHalfHour := elapsed <= 30*time.Minute

	groups := make(map[int][]string)
	targetPagesMap := make(map[string][]int)
	for _, id := range targetIDs {
		hour := rec.Hour
		if h, ok := hourBySource[id]; ok && h != 0 {
			hour = h
		}
		if hour == 0 {
			return model.CollectBatchResult{}, errors.New("采集时长不能为 0")
		}
		groups[hour] = append(groups[hour], id)

		if isWithinHalfHour && len(failedPagesBySource[id]) > 0 {
			targetPagesMap[id] = failedPagesBySource[id]
			log.Printf("[Spider] 批次 %s 站点 %s 距批次结束 %v（半小时内），重试失败页 %d 页",
				rec.BatchID, id, elapsed.Round(time.Second), len(targetPagesMap[id]))
		} else {
			if len(failedPagesBySource[id]) > 0 {
				log.Printf("[Spider] 批次 %s 站点 %s 距批次结束 %v（已超半小时），转为重试采集站",
					rec.BatchID, id, elapsed.Round(time.Second))
			} else {
				log.Printf("[Spider] 批次 %s 站点 %s 无失败页明细，重试采集站", rec.BatchID, id)
			}
		}
	}

	hours := make([]int, 0, len(groups))
	for hour := range groups {
		hours = append(hours, hour)
	}
	sort.Ints(hours)
	var result model.CollectBatchResult
	var firstID string
	started := 0
	for _, hour := range hours {
		claim, part := collect.PrepareBatchCollectStart(groups[hour])
		result.Skipped = append(result.Skipped, part.Skipped...)
		if len(claim.Sources) == 0 {
			continue
		}
		hourTargetPages := make(map[string][]int)
		for _, s := range claim.Sources {
			if pages, ok := targetPagesMap[s.Id]; ok && len(pages) > 0 {
				hourTargetPages[s.Id] = pages
			}
		}
		retryBatchID, err := collect.StartBatchCollectRetry(rec.BatchID, hour, claim, hourTargetPages)
		if err != nil {
			if firstID != "" {
				result.BatchID = firstID
			}
			return result, err
		}
		started++
		if firstID == "" {
			firstID = retryBatchID
		}
		result.Started = append(result.Started, part.Started...)
	}
	if started == 0 {
		if len(result.Skipped) > 0 {
			reasons := make([]string, 0, len(result.Skipped))
			for _, sk := range result.Skipped {
				reasons = append(reasons, fmt.Sprintf("%s: %s", sk.Name, sk.Reason))
			}
			return result, fmt.Errorf("无法启动重试: %s", strings.Join(reasons, "; "))
		}
		return result, errors.New("所选站点当前无法启动重试")
	}
	result.BatchID = firstID
	return result, nil
}

// parseRetryDetail 从批次明细取出失败站点、各站小时窗口和全部失败页（不截断，重试失败页要全部重采）。
func parseRetryDetail(detailJSON string) (map[string]bool, map[string]int, map[string][]int, error) {
	failedSourceMap := make(map[string]bool)
	hourBySource := make(map[string]int)
	failedPagesBySource := make(map[string][]int)
	if detailJSON == "" {
		return failedSourceMap, hourBySource, failedPagesBySource, nil
	}
	var details []model.CollectRecordSourceDetail
	if err := json.Unmarshal([]byte(detailJSON), &details); err != nil {
		return nil, nil, nil, fmt.Errorf("批次明细损坏: %w", err)
	}
	for _, d := range details {
		id := strings.TrimSpace(d.SourceID)
		if id == "" {
			continue
		}
		if d.Hour != 0 {
			hourBySource[id] = d.Hour
		}
		if d.Status == "failed" || d.Status == "partial" {
			failedSourceMap[id] = true
			if len(d.FailedPages) > 0 {
				failedPagesBySource[id] = append([]int(nil), d.FailedPages...)
			}
		}
	}
	return failedSourceMap, hourBySource, failedPagesBySource, nil
}
