package spider

import (
	"bytes"
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

	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/spider/fetcher"
	"server/internal/spider/progress"
	"server/internal/spider/scheduler"
	"server/internal/utils"
)

func collectFilmById(ids string, s *model.FilmSource, batchCtx *collectBatchContext) (changedMids []int64, retErr error) {
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
	return written.Notify, nil
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
		return scheduler.Mids{}, errors.New("get movie detail failed: response list is empty")
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
	batchCtx := newCollectBatchContext(model.CollectTriggerSingleUpdate, "单片更新", targets, startedAt)
	defer batchCtx.close()

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
			_, err := collectFilmById(sourceMid, &src, batchCtx)
			if err != nil {
				batchCtx.RecordSourceError(src.Id, err.Error())
				syslog.Errorf("[Spider] CollectSingleFilm 站点 %s 更新失败: %v", src.Name, err)
				return
			}
			if isPreferredCollectSource(src.Id) {
				if err := batchCtx.finalizePrimarySource(src.Id); err != nil {
					syslog.Errorf("[CollectSingleFilm] 首选站 %s 缓存刷新失败: %v", src.Name, err)
				}
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

func recoverFilmPage(ctx context.Context, s *model.FilmSource, fr *model.FailureRecord, batchCtx *collectBatchContext) {
	if s == nil || fr == nil {
		return
	}
	beginFailureRetry(fr.ID)
	settled := false
	finish := func(status, retryCount int, cause string, hasResult bool) {
		if settled {
			return
		}
		settled = true
		settleFailureRetry(fr.ID, status, retryCount, cause, hasResult)
	}
	defer func() {
		finish(fr.Status, fr.RetryCount, fr.Cause, false)
	}()
	select {
	case <-ctx.Done():
		settled = true
		abandonFailureRetry(fr.ID)
		return
	default:
	}
	r, err := requestForSource(s)
	if err != nil {
		finish(fr.Status, fr.RetryCount, err.Error(), false)
		return
	}
	r.Params.Set("pg", fmt.Sprint(fr.PageNumber))
	if fr.Hour > 0 {
		r.Params.Set("h", fmt.Sprint(fr.Hour))
	}

	list, err := fetcher.GetFilmDetailWithRetry(ctx, s, r)
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		settled = true
		abandonFailureRetry(fr.ID)
		return
	}
	if err != nil || len(list) <= 0 {
		if batchCtx != nil && err != nil {
			batchCtx.RecordSourceError(s.Id, err.Error())
		}
		status, retryCount, cause, wrote := markRecoverFailure(s, fr, "recover_fetch", err)
		if wrote {
			finish(status, retryCount, cause, true)
		}
		log.Println("Recover GetMovieDetail Error: ", err)
		return
	}

	written, err := saveCollectedFilmForCollect(ctx, s, fr.PageNumber, list)
	if ctx.Err() != nil || errors.Is(err, context.Canceled) {
		settled = true
		abandonFailureRetry(fr.ID)
		return
	}
	if err != nil {
		if batchCtx != nil {
			batchCtx.RecordSourceError(s.Id, err.Error())
		}
		status, retryCount, cause, wrote := markRecoverFailure(s, fr, "recover_save", err)
		if wrote {
			finish(status, retryCount, cause, true)
		}
		log.Println("Recover saveCollectedFilm Error: ", err)
		return
	}
	if batchCtx != nil {
		batchCtx.addAffectedMIDs(s, fr.Hour, written.Affected)
		batchCtx.AddNotifyMIDs(s.Id, written.Notify)
	}
	if err = repository.MarkFailureRecordRetrySucceeded(fr); err != nil {
		syslog.Errorf("[Spider][Recover] 失败记录标记成功失败 source_id=%s source=%s page=%d hour=%d err=%v", s.Id, s.Name, fr.PageNumber, fr.Hour, err)
		return
	}
	finish(model.FailureRecordStatusSuccess, fr.RetryCount, "", true)
}

func markRecoverFailure(s *model.FilmSource, fr *model.FailureRecord, phase string, err error) (status, retryCount int, cause string, wrote bool) {
	if err == nil {
		err = errors.New("unknown error")
	}
	cause = fmt.Sprintf("%s: %v", phase, err)
	final, count, updateErr := repository.MarkFailureRecordRetryFailed(fr, cause, model.MaxFailureRetryCount)
	if updateErr != nil {
		syslog.Errorf("[Spider][Recover] 失败记录更新失败 source_id=%s source=%s page=%d hour=%d err=%v", s.Id, s.Name, fr.PageNumber, fr.Hour, updateErr)
		return 0, 0, "", false
	}
	if final {
		syslog.Warnf("[Spider][Recover] 失败页达到最大自动重试次数 source_id=%s source=%s page=%d hour=%d retry_count=%d", s.Id, s.Name, fr.PageNumber, fr.Hour, count)
	}
	status = model.FailureRecordStatusPending
	if final {
		status = model.FailureRecordStatusFailed
	}
	return status, count, cause, true
}

var (
	errFailureRecoverMissing  = errors.New("采集重试执行失败: 失败记录不存在")
	errFailureRecoverSuccess  = errors.New("已重试成功，无需重复重试")
	errFailureRecoverNoSource = errors.New("采集站不存在，不能重试")
	errFailureRecoverDisabled = errors.New("采集站已停用，不能重试")
	errFailureRecoverBusy     = errors.New("采集站正在采集，请稍后再试")
	errFailureRecoverRunning  = errors.New("正在重试中")
)

// FailureRecoverOutcome 是一次重试入队的结果。排队本身不代表页面已经重试完成。
type FailureRecoverOutcome struct {
	Queued  int
	Already int
	Skipped int
}

func (o FailureRecoverOutcome) Message() string {
	switch {
	case o.Queued == 0 && o.Already == 0 && o.Skipped == 0:
		return "没有待重试的记录"
	case o.Queued == 0 && o.Skipped == 0:
		return fmt.Sprintf("%d 条已在重试，没有新的任务", o.Already)
	case o.Queued == 0 && o.Already == 0:
		return fmt.Sprintf("%d 条未开始：采集站正在采集、已停用或不存在", o.Skipped)
	case o.Queued == 0:
		return fmt.Sprintf("没有新的任务：已在重试 %d 条，跳过 %d 条", o.Already, o.Skipped)
	default:
		msg := fmt.Sprintf("已加入重试 %d 条", o.Queued)
		if o.Already > 0 {
			msg += fmt.Sprintf("，已在重试 %d 条", o.Already)
		}
		if o.Skipped > 0 {
			msg += fmt.Sprintf("，跳过 %d 条", o.Skipped)
		}
		return msg
	}
}

// AcceptFailureRecover 校验通过后才进入重试队列。站点不存在、已停用或正被别的采集占用时直接拒绝。
func AcceptFailureRecover(fr *model.FailureRecord) error {
	if fr == nil || fr.ID == 0 {
		return errFailureRecoverMissing
	}
	if fr.Status == model.FailureRecordStatusSuccess {
		return errFailureRecoverSuccess
	}
	source := repository.FindCollectSourceById(fr.OriginId)
	if source == nil {
		return errFailureRecoverNoSource
	}
	if !source.State {
		return errFailureRecoverDisabled
	}
	if failureRecoverBlocked(source.Id) {
		return errFailureRecoverBusy
	}
	if !TryBeginFailureRetry(fr.ID) {
		return errFailureRecoverRunning
	}
	if fr.Status == model.FailureRecordStatusFailed {
		_ = repository.UpdateFailureRecordStatusByID(fr.ID, model.FailureRecordStatusPending)
		fr.Status = model.FailureRecordStatusPending
	}
	if pushFailureRecover(source.Id, *fr) {
		go serveFailureRecoverLane(source.Id, *source, recoverQueuedFailureRecords)
	}
	return nil
}

// EnqueuePendingFailureRecovers 把所有待重试记录放进各自站点的队列。
// 本站队列已在跑时继续加入；别的采集占着站点时跳过，不把记录标成重试中。
func EnqueuePendingFailureRecovers() (FailureRecoverOutcome, error) {
	var out FailureRecoverOutcome
	list, err := repository.PendingRecord()
	if err != nil {
		return out, err
	}
	for i := range list {
		err := AcceptFailureRecover(&list[i])
		switch {
		case err == nil:
			out.Queued++
		case errors.Is(err, errFailureRecoverRunning):
			out.Already++
		default:
			out.Skipped++
		}
	}
	return out, nil
}

func recoverQueuedFailureRecords(source model.FilmSource, records []model.FailureRecord, batchCtx *collectBatchContext) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	// 上一轮采集留下的 stopping/stopped 不是这次重试的停止信号。
	baselineStopped := progress.IsStopping(source.Id) || progress.IsStopped(source.Id)
	stopWatch := make(chan struct{})
	defer close(stopWatch)
	go func() {
		ticker := time.NewTicker(200 * time.Millisecond)
		defer ticker.Stop()
		for {
			select {
			case <-stopWatch:
				return
			case <-ticker.C:
				if recoverStopRequested(source.Id, baselineStopped) {
					cancel()
					return
				}
			}
		}
	}()
	for i := range records {
		if ctx.Err() != nil || recoverStopRequested(source.Id, baselineStopped) {
			for j := i; j < len(records); j++ {
				abandonFailureRetry(records[j].ID)
			}
			abandonFailureRecoverPending(source.Id)
			break
		}
		record := records[i]
		recoverFilmPage(ctx, &source, &record, batchCtx)
	}
}

func recoverStopRequested(id string, baselineStopped bool) bool {
	if baselineStopped {
		return false
	}
	return progress.IsStopping(id) || progress.IsStopped(id)
}

func FullRecoverSpider() {
	out, err := EnqueuePendingFailureRecovers()
	if err != nil {
		log.Printf("[Spider] 失败记录重试：读取待重试记录失败: %v", err)
		return
	}
	log.Printf("[Spider] 失败记录重试：%s", out.Message())
}

func ClearSpider() error {
	filmrepo.ReportResetProgress(5, "正在停止采集任务")
	StopAllTasks()
	filmrepo.ReportResetProgress(15, "正在清空数据")
	return collectLifecycle.runExclusive(func() error {
		repository.ResetCollectStatsCoalescer()
		return filmrepo.FilmZero()
	})
}

func CollectApiTest(s model.FilmSource) error {
	return CollectApiTestCtx(context.Background(), s)
}

// CollectApiTestCtx 在给定超时内探测采集站。
func CollectApiTestCtx(ctx context.Context, s model.FilmSource) error {
	return collectApiTest(ctx, s, 0, false, false)
}

func CollectApiTestChoosingProxy(s model.FilmSource, useProxy bool) error {
	return collectApiTest(context.Background(), s, 0, useProxy, true)
}

func CollectApiTestWithTimeout(s model.FilmSource, timeoutSeconds int) error {
	return collectApiTest(context.Background(), s, timeoutSeconds, false, false)
}

func collectApiTest(ctx context.Context, s model.FilmSource, timeoutSeconds int, useProxy bool, proxyChosen bool) error {
	if ctx == nil {
		return errors.New("检测上下文为空")
	}
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}, Ctx: ctx, SkipTLSVerify: s.SkipTLSVerify}
	r.Params.Set("ac", "list")
	r.Params.Set("pg", "1")
	if timeoutSeconds > 0 {
		r.Header = map[string][]string{"timeout": {strconv.Itoa(timeoutSeconds)}}
	}
	if proxyChosen {
		if useProxy {
			cfg, err := repository.GetProxyConfig()
			if err != nil {
				return err
			}
			if !cfg.Enabled || strings.TrimSpace(cfg.ProxyURL) == "" {
				return errors.New("测试失败, 代理未开启或地址为空")
			}
			r.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
		}
	} else if s.ProxyCollect {
		ok, proxy, err := repository.ResolveSpiderProxy()
		if err != nil {
			return err
		}
		if ok {
			r.ProxyURL = proxy
		}
	} else if s.Id != "" {
		ok, proxy, err := repository.ResolveSourceProxy(s.Id)
		if err != nil {
			return err
		}
		if ok {
			r.ProxyURL = proxy
		}
	}
	err := utils.ApiTest(&r)
	if err != nil {
		return errors.New(fmt.Sprint("测试失败, 请求响应异常 : ", err.Error()))
	}
	resp := bytes.TrimPrefix(r.Resp, []byte("\xef\xbb\xbf"))
	resp = bytes.TrimSpace(resp)
	if len(resp) == 0 {
		return errors.New("测试失败, 接口返回数据为空")
	}

	lowerResp := bytes.ToLower(resp)
	isHTML := bytes.HasPrefix(lowerResp, []byte("<!doctype html")) ||
		bytes.HasPrefix(lowerResp, []byte("<html"))
	isXML := (bytes.HasPrefix(lowerResp, []byte("<?xml")) ||
		bytes.HasPrefix(lowerResp, []byte("<rss"))) && !isHTML
	isJSON := resp[0] == '{' || resp[0] == '['

	if isHTML {
		return errors.New("接口返回了 HTML 网页内容，而非 API 数据（可能受 WAF 防火墙拦截或站点配置了人机验证）")
	}

	expectedFormat := s.ResolveFormat()
	if expectedFormat == model.SourceFormatJSON {
		if isXML {
			return errors.New("接口返回为 XML 格式，与所选的 JSON 格式不一致，请切换为 XML 格式")
		}
		if !isJSON {
			return errors.New("接口返回数据格式异常，无法解析为 JSON 数据")
		}
		lp := model.FilmListPage{}
		if err = json.Unmarshal(resp, &lp); err != nil {
			return errors.New(fmt.Sprint("接口返回 JSON 结构异常，解析失败: ", err.Error()))
		}
		return nil
	}

	if isJSON {
		return errors.New("接口返回为 JSON 格式，与所选的 XML 格式不一致，请切换为 JSON 格式")
	}
	if !isXML {
		return errors.New("接口返回数据格式异常，无法解析为 XML 数据")
	}
	rss := XMLRSS{}
	if err = unmarshalXMLWithCharset(resp, &rss); err != nil {
		return errors.New(fmt.Sprint("接口返回 XML 结构异常，解析失败: ", err.Error()))
	}
	return nil
}
