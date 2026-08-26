package spider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/utils"
)

func collectFilmById(ids string, s *model.FilmSource, flushAtEnd bool) (changedMids []int64, retErr error) {
	if s == nil {
		return nil, errors.New("采集站信息不存在")
	}
	if err := collectLifecycle.beginSource(s.Id); err != nil {
		log.Printf("[Spider] 站点 %s 无法启动单片采集: %v\n", s.Id, err)
		return nil, err
	}
	defer func() {
		if flushAtEnd {
			flushErr := collectLifecycle.finishSourceAndFlush(*s)
			if retErr == nil && flushErr != nil {
				retErr = flushErr
			}
			return
		}
		collectLifecycle.endSource(s.Id)
	}()

	release, err := waitSourceRequestTurn(context.Background(), s, fmt.Sprintf("单片请求 ids=%s ", ids))
	if err != nil {
		return nil, err
	}
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("pg", "1")
	r.Params.Set("ids", ids)
	list, err := spiderCore.GetFilmDetail(r)
	if err != nil {
		release(err)
		return nil, fmt.Errorf("get movie detail failed: %w", err)
	}
	if len(list) <= 0 {
		release(errors.New("response list is empty"))
		return nil, errors.New("get movie detail failed: response list is empty")
	}
	release(nil)

	written, err := saveCollectedFilmForCollect(context.Background(), s, 1, list)
	if err != nil {
		return nil, err
	}
	if s.Grade == model.MasterCollect {
		collectLifecycle.addMasterAffectedMIDs(written.Affected)
	} else {
		collectLifecycle.addAffectedMIDs(written.Affected)
	}
	return written.Notify, nil
}

func CollectSingleFilm(ids string) {
	globalMid, err := strconv.ParseInt(strings.TrimSpace(ids), 10, 64)
	if err != nil || globalMid <= 0 {
		log.Printf("[Spider] CollectSingleFilm: 非法影片 ID %q\n", ids)
		return
	}

	enabled := filterEnabledSources(repository.GetCollectSourceList())
	if len(enabled) == 0 {
		log.Println("[Spider] CollectSingleFilm: 未找到任何启用的站点")
		return
	}

	startedAt := time.Now()
	batch := notify.StartChangeBatch()
	type singleResult struct {
		source model.FilmSource
		err    error
	}
	var (
		mu      sync.Mutex
		results []singleResult
	)
	var wg sync.WaitGroup
	for _, source := range enabled {
		requestID := resolveSingleCollectSourceMid(globalMid, source)
		if requestID == "" {
			continue
		}

		wg.Add(1)
		go func(src model.FilmSource, sourceMid string) {
			defer wg.Done()
			mids, err := collectFilmById(sourceMid, &src, false)
			if err != nil {
				syslog.Errorf("[Spider] CollectSingleFilm 站点 %s 更新失败: %v", src.Name, err)
			} else if len(mids) > 0 {
				noteCollectedMIDs(batch, src.Id, src.Name, mids)
			}
			mu.Lock()
			results = append(results, singleResult{source: src, err: err})
			mu.Unlock()
		}(source, requestID)
	}
	wg.Wait()

	attempted := make([]model.FilmSource, 0, len(results))
	for _, r := range results {
		attempted = append(attempted, r.source)
	}
	var finalizeErr error
	if len(attempted) > 0 {
		flushMap := make(map[string]model.FilmSource, len(attempted))
		for _, s := range attempted {
			flushMap[s.Id] = s
		}
		if err := collectLifecycle.runFlush(func(affectedMIDs []int64, masterMIDs []int64) error {
			_, _, err := flushPendingSources(flushMap, affectedMIDs, masterMIDs)
			return err
		}); err != nil {
			syslog.Errorf("[CollectSingleFilm] 收尾刷新失败: %v", err)
			finalizeErr = err
		}
	}

	notifyResults := make([]model.SourceNotifyResult, 0, len(results))
	for _, r := range results {
		status := progressStatusDone
		errMsg := ""
		if r.err != nil {
			status = progressStatusFailed
			errMsg = r.err.Error()
		} else if finalizeErr != nil {
			status = progressStatusFailed
			errMsg = finalizeErr.Error()
		}
		notifyResults = append(notifyResults, notify.BuildSourceResultDirect(r.source, status, errMsg))
	}
	if len(notifyResults) == 0 {
		notifyResults = append(notifyResults, model.SourceNotifyResult{
			SourceName: "单片更新",
			Status:     progressStatusFailed,
			Error:      fmt.Sprintf("影片 #%d 未匹配到任何启用站点的 source_mid", globalMid),
			FailedCnt:  1,
		})
	}
	emitBatchSummaryDirect(batch, model.NotifyTriggerSingleUpdate, notifyResults, startedAt, finalizeErr)
}

func resolveSingleCollectSourceMid(globalMid int64, source model.FilmSource) string {
	if globalMid <= 0 {
		return ""
	}
	sourceMid := filmrepo.LoadSourceMidByGlobalMid(globalMid, source.Id)
	if sourceMid > 0 {
		return strconv.FormatInt(sourceMid, 10)
	}
	if source.Grade == model.MasterCollect {
		return strconv.FormatInt(globalMid, 10)
	}
	return ""
}

func ClearSpider() error {
	filmrepo.ReportResetProgress(5, "正在停止采集任务")
	StopAllTasks()
	if err := collectLifecycle.waitIdle(time.Second * 30); err != nil {
		return err
	}
	filmrepo.ReportResetProgress(15, "正在清空数据")
	return collectLifecycle.runExclusive(func() error {
		repository.ResetCollectStatsCoalescer()
		return filmrepo.FilmZero()
	})
}

func recoverFilmPage(ctx context.Context, s *model.FilmSource, fr *model.FailureRecord, batch *notify.ChangeBatch) {
	if s == nil || fr == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("pg", fmt.Sprint(fr.PageNumber))
	if fr.Hour > 0 {
		r.Params.Set("h", fmt.Sprint(fr.Hour))
	}

	list, err := getFilmDetailWithRetry(ctx, s, r)
	if err != nil || len(list) <= 0 {
		markRecoverFailure(s, fr, "recover_fetch", err)
		log.Println("Recover GetMovieDetail Error: ", err)
		return
	}

	written, err := saveCollectedFilmForCollect(ctx, s, fr.PageNumber, list)
	if err != nil {
		markRecoverFailure(s, fr, "recover_save", err)
		log.Println("Recover saveCollectedFilm Error: ", err)
		return
	}
	noteCollectedMIDs(batch, s.Id, s.Name, written.Notify)
	if s.Grade == model.MasterCollect {
		collectLifecycle.addMasterAffectedMIDs(written.Affected)
	} else {
		collectLifecycle.addAffectedMIDs(written.Affected)
	}
	repository.DeleteFailureRecord(fr)
}

func markRecoverFailure(s *model.FilmSource, fr *model.FailureRecord, phase string, err error) {
	if err == nil {
		err = errors.New("unknown error")
	}
	cause := fmt.Sprintf("%s: %v", phase, err)
	final, retryCount, updateErr := repository.MarkFailureRecordRetryFailed(fr, cause, recoverMaxRetryCount)
	if updateErr != nil {
		syslog.Errorf("[Spider][Recover] 失败记录更新失败 source_id=%s source=%s page=%d hour=%d err=%v", s.Id, s.Name, fr.PageNumber, fr.Hour, updateErr)
		return
	}
	if final {
		syslog.Warnf("[Spider][Recover] 失败页达到最大自动重试次数 source_id=%s source=%s page=%d hour=%d retry_count=%d", s.Id, s.Name, fr.PageNumber, fr.Hour, retryCount)
	}
}

func SingleRecoverSpider(fr *model.FailureRecord) {
	s := repository.FindCollectSourceById(fr.OriginId)
	if s == nil {
		syslog.Errorf("[Spider] 重试失败: 站点 %s 不存在", fr.OriginId)
		return
	}
	startedAt := time.Now()
	batch := notify.StartChangeBatch()
	if err := collectLifecycle.waitAndBeginSource(s.Id); err != nil {
		syslog.Errorf("[Spider] 站点 %s 无法启动失败页重试: %v", s.Id, err)
		if notify.IsEventEnabled(model.NotifyEventCollectBatchSummary) {
			emitBatchSummaryDirect(batch, model.NotifyTriggerRecover, []model.SourceNotifyResult{
				notify.BuildSourceResultDirect(*s, progressStatusFailed, err.Error()),
			}, startedAt, err)
		} else {
			emitSourceFailedNotify(s.Id, s.Name, err.Error())
		}
		return
	}
	var finalizeErr error
	defer func() {
		if err := collectLifecycle.finishSourceAndFlush(*s); err != nil {
			syslog.Errorf("[Spider] 站点 %s 失败页重试收尾刷新失败: %v", s.Id, err)
			finalizeErr = err
		}
		emitBatchSummaryForSources(batch, model.NotifyTriggerRecover, []model.FilmSource{*s}, startedAt, finalizeErr)
	}()
	recoverFilmPage(context.Background(), s, fr, batch)
}

func FullRecoverSpider() {
	list := repository.PendingRecord()
	sourcesToFlush := make([]model.FilmSource, 0, len(list))
	seen := make(map[string]struct{}, len(list))
	recordsBySource := make(map[string][]model.FailureRecord, len(list))
	sourceByID := make(map[string]model.FilmSource, len(list))
	limit := config.CollectPageWorkers
	if limit <= 0 {
		limit = config.DefaultCollectPageWorkers
	}
	if limit <= 0 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	startedAt := time.Now()
	batch := notify.StartChangeBatch()
	for i := range list {
		fr := list[i]
		s := repository.FindCollectSourceById(fr.OriginId)
		if s == nil {
			syslog.Errorf("[Spider] 重试失败: 站点 %s 不存在", fr.OriginId)
			continue
		}
		if _, ok := seen[s.Id]; !ok {
			seen[s.Id] = struct{}{}
			sourcesToFlush = append(sourcesToFlush, *s)
			sourceByID[s.Id] = *s
		}
		recordsBySource[s.Id] = append(recordsBySource[s.Id], fr)
	}
	for sourceID, records := range recordsBySource {
		src, ok := sourceByID[sourceID]
		if !ok {
			continue
		}
		recordsCopy := append([]model.FailureRecord(nil), records...)
		wg.Add(1)
		sem <- struct{}{}
		go func(source model.FilmSource, pending []model.FailureRecord) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := collectLifecycle.waitAndBeginSource(source.Id); err != nil {
				syslog.Errorf("[Spider] 站点 %s 无法启动失败页重试: %v", source.Id, err)
				return
			}
			defer collectLifecycle.endSource(source.Id)
			for i := range pending {
				record := pending[i]
				recoverFilmPage(context.Background(), &source, &record, batch)
			}
		}(src, recordsCopy)
	}
	wg.Wait()
	flushSourcesPending("FullRecoverSpider", model.NotifyTriggerRecover, sourcesToFlush, startedAt, batch)
}

func CollectApiTest(s model.FilmSource) error {
	return CollectApiTestWithTimeout(s, 0)
}

func CollectApiTestWithTimeout(s model.FilmSource, timeoutSeconds int) error {
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("ac", "list")
	r.Params.Set("pg", "1")
	if timeoutSeconds > 0 {
		r.Header = map[string][]string{"timeout": {strconv.Itoa(timeoutSeconds)}}
	}
	err := utils.ApiTest(&r)
	if err == nil {
		lp := model.FilmListPage{}
		if err = json.Unmarshal(r.Resp, &lp); err != nil {
			return errors.New(fmt.Sprint("测试失败, 返回数据异常, JSON序列化失败: ", err))
		}
		return nil
	}
	return errors.New(fmt.Sprint("测试失败, 请求响应异常 : ", err.Error()))
}

func GetActiveTasks() []string {
	ids := make([]string, 0)
	activeTasks.Range(func(key, value any) bool {
		ids = append(ids, key.(string))
		return true
	})
	return ids
}
