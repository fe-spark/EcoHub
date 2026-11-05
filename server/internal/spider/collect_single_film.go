package spider

import (
	"context"
	"errors"
	"fmt"
	"log"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider/fetcher"
	"server/internal/spider/progress"
	"server/internal/spider/scheduler"
)

func singleFilmSourceDetail(src model.FilmSource, changed int, errMsg string) model.CollectRecordSourceDetail {
	detail := model.CollectRecordSourceDetail{
		SourceID:    src.Id,
		SourceName:  strings.TrimSpace(src.Name),
		Status:      progress.StatusDone,
		PageTotal:   1,
		PageCurrent: 1,
		SuccessCnt:  1,
		FilmsTotal:  changed,
	}
	if errMsg != "" {
		detail.Status = progress.StatusFailed
		detail.PageCurrent = 0
		detail.SuccessCnt = 0
		detail.FailedCnt = 1
		detail.FilmsTotal = 0
		detail.Error = errMsg
	}
	if detail.SourceName == "" {
		detail.SourceName = src.Id
	}
	return detail
}

func collectFilmById(ids string, s *model.FilmSource, batchCtx *collectBatchContext) (storedMids []int64, retErr error) {
	if s == nil {
		return nil, errors.New("采集站信息不存在")
	}
	if err := collectLifecycle.beginSource(s.Id); err != nil {
		log.Printf("[Spider] 站点 %s 无法启动单片采集: %v\n", s.Id, err)
		return nil, err
	}
	defer collectLifecycle.endSource(s.Id)

	written, err := collectFilmIDs(context.Background(), ids, s, batchCtx)
	if err != nil {
		return nil, err
	}
	return written.Affected, nil
}

func collectFilmIDs(ctx context.Context, ids string, s *model.FilmSource, batchCtx *collectBatchContext) (scheduler.Mids, error) {
	if s == nil {
		return scheduler.Mids{}, errors.New("采集站信息不存在")
	}
	ids = strings.TrimSpace(ids)
	if ids == "" {
		return scheduler.Mids{}, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	r, err := requestForSource(s)
	if err != nil {
		return scheduler.Mids{}, err
	}
	r.Params.Set("pg", "1")
	r.Params.Set("ids", ids)
	list, err := fetcher.GetFilmDetailWithRetry(ctx, s, r)
	if err != nil {
		return scheduler.Mids{}, fmt.Errorf("get movie detail failed: %w", err)
	}
	if len(list) <= 0 {
		return scheduler.Mids{}, errors.New("get movie detail failed: film not found or unplayable")
	}

	written, err := saveCollectedFilmForCollect(ctx, s, 1, list)
	if err != nil {
		return scheduler.Mids{}, err
	}
	if batchCtx != nil {
		batchCtx.markSourceFinished(*s)
		batchCtx.addAffectedMIDs(s, 1, written.Affected)
		batchCtx.AddNotifyMIDs(s.Id, written.Notify)
	}
	return written, nil
}

func CollectFilms(mids []int64) {
	for _, id := range mids {
		if id > 0 {
			CollectSingleFilm(strconv.FormatInt(id, 10))
		}
	}
}

func CollectSingleFilm(ids string) {
	globalMid, err := strconv.ParseInt(strings.TrimSpace(ids), 10, 64)
	if err != nil || globalMid <= 0 {
		log.Printf("[Spider] CollectSingleFilm: 非法影片 ID %q\n", ids)
		return
	}

	sources, err := repository.GetCollectSourceList()
	if err != nil {
		log.Printf("[Spider] CollectSingleFilm 读取采集站失败: %v", err)
		return
	}
	enabled := filterEnabledSources(sources)
	if len(enabled) == 0 {
		log.Println("[Spider] CollectSingleFilm: 未找到任何启用的站点")
		return
	}

	targets := make([]model.FilmSource, 0, len(enabled))
	sourceMids := make(map[string]string, len(enabled))
	for _, source := range enabled {
		requestID := resolveSingleCollectSourceMid(globalMid, source)
		if requestID == "" {
			continue
		}
		targets = append(targets, source)
		sourceMids[source.Id] = requestID
	}
	targets = filterCollectableSources(targets, "单片更新")
	if len(targets) == 0 {
		log.Println("[Spider] CollectSingleFilm: 匹配站点均已在正采集队列中，跳过")
		return
	}

	startedAt := time.Now()
	batchCtx := newCollectBatchContext(model.CollectTriggerSingleUpdate, model.CollectTagSingleUpdate, targets, startedAt).
		withReplayFilmIDs([]int64{globalMid})
	defer batchCtx.close()

	if err := batchCtx.recordStart(); err != nil {
		log.Printf("[CollectSingleFilm] 初始采集记录写入失败 batchID=%s: %v", batchCtx.batchID, err)
		return
	}

	var wg sync.WaitGroup
	for _, source := range targets {
		requestID := sourceMids[source.Id]
		if requestID == "" {
			continue
		}

		wg.Add(1)
		go func(src model.FilmSource, sourceMid string) {
			defer wg.Done()
			defer batchCtx.releaseSourceOccupy(src.Id)
			stored, err := collectFilmById(sourceMid, &src, batchCtx)
			if errors.Is(err, errCollectDuringMaintenance) {
				log.Printf("[Spider] 站点 %s 停止采集：维护任务执行中（%s）", src.Name, maintenanceRunningDetail())
				detail := singleFilmSourceDetail(src, 0, "")
				detail.Status = progress.StatusStopped
				detail.PageCurrent = 0
				detail.SuccessCnt = 0
				batchCtx.recordSourceSnapshot(detail)
				return
			}
			if err != nil {
				batchCtx.recordSourceSnapshot(singleFilmSourceDetail(src, 0, err.Error()))
				batchCtx.RecordSourceError(src.Id, err.Error())
				syslog.Errorf("[Spider] CollectSingleFilm 站点 %s 更新失败: %v", src.Name, err)
				return
			}
			batchCtx.recordSourceSnapshot(singleFilmSourceDetail(src, len(stored), ""))
			if err := batchCtx.sealFinishedSource(src); err != nil {
				syslog.Errorf("[CollectSingleFilm] 站点 %s 封口入队失败: %v", src.Name, err)
			}
		}(source, requestID)
	}
	wg.Wait()

	if err := batchCtx.flushAndFinalize(); err != nil {
		syslog.Errorf("[CollectSingleFilm] 缓存刷新失败: %v", err)
	}
	batchCtx.emitSummary()
}

func resolveSingleCollectSourceMid(globalMid int64, source model.FilmSource) string {
	if globalMid <= 0 {
		return ""
	}
	var sourceMid int64
	if err := db.Mdb.Model(&model.MovieSourceMapping{}).
		Where("global_mid = ? AND source_id = ?", globalMid, source.Id).
		Select("source_mid").
		First(&sourceMid).Error; err == nil && sourceMid > 0 {
		return strconv.FormatInt(sourceMid, 10)
	}
	return ""
}
