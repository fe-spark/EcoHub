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

	"gorm.io/gorm"
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
	if err := attachSingleFilmNames(list); err != nil {
		return nil, 0, err
	}
	return list, total, nil
}

func attachSingleFilmNames(list []model.CollectRecord) error {
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
		return nil
	}
	names, err := repository.FilmNamesByIDs(ids)
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

func (s *CollectRecordService) ListOpenIncidents(now time.Time) ([]model.SourceIncident, error) {
	return repository.ListOpenSourceIncidents(now)
}

// GetBatchFilms 分页获取某采集批次的影片清单。
// 第三个返回值表示结果来自 detail_json 里的历史样例，不是全量清单。
func (s *CollectRecordService) GetBatchFilms(q model.CollectRecordFilmQuery) ([]model.CollectRecordFilm, int64, bool, error) {
	q.Page, q.PageSize = normalizeFilmPage(q.Page, q.PageSize)
	list, total, err := repository.GetCollectRecordFilms(q)
	if err != nil {
		return nil, 0, false, err
	}
	if total > 0 {
		return list, total, false, nil
	}
	if strings.TrimSpace(q.Keyword) != "" {
		_, rawTotal, rawErr := repository.GetCollectRecordFilms(model.CollectRecordFilmQuery{
			BatchID:  q.BatchID,
			SourceID: q.SourceID,
			Page:     1,
			PageSize: 1,
		})
		if rawErr != nil {
			return nil, 0, false, rawErr
		}
		if rawTotal > 0 {
			return []model.CollectRecordFilm{}, 0, false, nil
		}
	}

	samples, err := legacySampleFilms(q)
	if err != nil {
		return nil, 0, false, err
	}
	if len(samples) == 0 {
		return []model.CollectRecordFilm{}, 0, false, nil
	}
	page, pageSize := normalizeFilmPage(q.Page, q.PageSize)
	start := (page - 1) * pageSize
	if start >= len(samples) {
		return []model.CollectRecordFilm{}, int64(len(samples)), true, nil
	}
	end := start + pageSize
	if end > len(samples) {
		end = len(samples)
	}
	return samples[start:end], int64(len(samples)), true, nil
}

// legacySampleFilms 只在影片表确实没有该批次记录时读取旧 detail_json 样例。
func legacySampleFilms(q model.CollectRecordFilmQuery) ([]model.CollectRecordFilm, error) {
	rec, err := repository.GetCollectRecordByBatchID(q.BatchID)
	if err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return nil, nil
		}
		return nil, err
	}
	if rec == nil || strings.TrimSpace(rec.DetailJson) == "" {
		return nil, nil
	}
	var details []model.CollectRecordSourceDetail
	if err := json.Unmarshal([]byte(rec.DetailJson), &details); err != nil {
		return nil, fmt.Errorf("批次明细损坏: %w", err)
	}

	sourceID := strings.TrimSpace(q.SourceID)
	seen := make(map[int64]struct{})
	films := make([]model.CollectRecordFilm, 0)
	for _, detail := range details {
		if sourceID != "" && strings.TrimSpace(detail.SourceID) != sourceID {
			continue
		}
		for _, film := range detail.Films {
			if film.Mid <= 0 {
				continue
			}
			if sourceID == "" {
				if _, ok := seen[film.Mid]; ok {
					continue
				}
				seen[film.Mid] = struct{}{}
				film.SourceID = ""
			}
			films = append(films, film)
		}
	}
	keyword := strings.TrimSpace(q.Keyword)
	if keyword == "" {
		return films, nil
	}
	filtered := make([]model.CollectRecordFilm, 0, len(films))
	for _, film := range films {
		if strings.Contains(film.Name, keyword) {
			filtered = append(filtered, film)
		}
	}
	return filtered, nil
}

func normalizeFilmPage(page, pageSize int) (int, int) {
	if page < 1 {
		page = 1
	}
	if pageSize < 1 {
		pageSize = 20
	} else if pageSize > 100 {
		pageSize = 100
	}
	return page, pageSize
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
