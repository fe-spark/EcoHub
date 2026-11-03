package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"strconv"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider"
)

type CollectRecordService struct{}

var CollectRecordSvc = new(CollectRecordService)

// 单片重放默认走采集器。测试可替换，避免重试请求打到外站。
var replayCollectFilms = spider.CollectFilms

// GetHistoryList 分页获取采集运行记录
func (s *CollectRecordService) GetHistoryList(q model.CollectRecordQuery) ([]model.CollectRecord, int64, error) {
	list, total, err := repository.GetCollectRecordList(q)
	if err != nil || len(list) == 0 {
		return list, total, err
	}
	attachSingleFilmNames(list)
	return list, total, nil
}

func attachSingleFilmNames(list []model.CollectRecord) {
	ids := make([]int64, 0)
	seen := make(map[int64]struct{})
	parsed := make([][]int64, len(list))
	for i := range list {
		if list[i].Trigger != model.CollectTriggerSingleUpdate && list[i].Tag != model.CollectTagSingleUpdate && list[i].Tag != "单片更新" {
			continue
		}
		rowIDs := parseFilmIDs(list[i].FilmIDs)
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
		return
	}
	names, err := repository.FilmNamesByIDs(ids)
	if err != nil {
		return
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
}

func (s *CollectRecordService) ListOpenIncidents(now time.Time) ([]model.SourceIncident, error) {
	return repository.ListOpenSourceIncidents(now)
}

// GetBatchFilms 分页获取某采集批次某站点入库/更新的全部影片清单
func (s *CollectRecordService) GetBatchFilms(q model.CollectRecordFilmQuery) ([]model.CollectRecordFilm, int64, error) {
	list, total, err := repository.GetCollectRecordFilms(q)
	if err == nil && total > 0 {
		return list, total, nil
	}

	// 历史批次兜底兼容：若独立影片表尚无数据，从 detail_json 中回填快照影片
	rec, recErr := repository.GetCollectRecordByBatchID(q.BatchID)
	if recErr != nil || rec == nil || rec.DetailJson == "" {
		return list, total, err
	}

	var details []model.CollectRecordSourceDetail
	if jsonErr := json.Unmarshal([]byte(rec.DetailJson), &details); jsonErr != nil {
		return list, total, err
	}

	var fallbackFilms []model.CollectRecordFilm
	for _, d := range details {
		if strings.TrimSpace(d.SourceID) == strings.TrimSpace(q.SourceID) || strings.TrimSpace(q.SourceID) == "" {
			fallbackFilms = append(fallbackFilms, d.Films...)
		}
	}
	if len(fallbackFilms) == 0 {
		return list, total, err
	}

	keyword := strings.TrimSpace(q.Keyword)
	if keyword != "" {
		filtered := make([]model.CollectRecordFilm, 0, len(fallbackFilms))
		for _, f := range fallbackFilms {
			if strings.Contains(f.Name, keyword) {
				filtered = append(filtered, f)
			}
		}
		fallbackFilms = filtered
	}

	totalFallback := int64(len(fallbackFilms))
	page := q.Page
	if page < 1 {
		page = 1
	}
	pageSize := q.PageSize
	if pageSize < 1 {
		pageSize = 20
	}
	start := (page - 1) * pageSize
	if start >= len(fallbackFilms) {
		return []model.CollectRecordFilm{}, totalFallback, nil
	}
	end := start + pageSize
	if end > len(fallbackFilms) {
		end = len(fallbackFilms)
	}
	return fallbackFilms[start:end], totalFallback, nil
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

	// 单片更新没有时间窗口。按原影片重放，不能走小时采集。
	if rec.Trigger == model.CollectTriggerSingleUpdate {
		ids := parseFilmIDs(rec.FilmIDs)
		if len(ids) == 0 {
			return model.CollectBatchResult{}, errors.New("单片更新缺少影片编号，无法重试")
		}
		go replayCollectFilms(ids)
		return model.CollectBatchResult{}, nil
	}

	// 从明细解析失败站点
	failedSourceMap := make(map[string]bool)
	if rec.DetailJson != "" {
		var details []model.CollectRecordSourceDetail
		if err := json.Unmarshal([]byte(rec.DetailJson), &details); err != nil {
			return model.CollectBatchResult{}, fmt.Errorf("批次明细损坏: %w", err)
		}
		for _, d := range details {
			if (d.Status == "failed" || d.Status == "partial") && strings.TrimSpace(d.SourceID) != "" {
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
	if rec.Hour == 0 {
		return model.CollectBatchResult{}, errors.New("采集时长不能为 0")
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

func parseFilmIDs(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	seen := make(map[int64]struct{}, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || id <= 0 {
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
