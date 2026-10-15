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

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/spider/fetcher"
	"server/internal/spider/scheduler"
	"server/internal/utils"
)

// recoverMaxRetryCount 失败页重试的最大轮次。
const recoverMaxRetryCount = 3

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

	r := requestForSource(s)
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

	enabled := filterEnabledSources(repository.GetCollectSourceList())
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
	select {
	case <-ctx.Done():
		return
	default:
	}
	r := requestForSource(s)
	r.Params.Set("pg", fmt.Sprint(fr.PageNumber))
	if fr.Hour > 0 {
		r.Params.Set("h", fmt.Sprint(fr.Hour))
	}

	list, err := fetcher.GetFilmDetailWithRetry(ctx, s, r)
	if err != nil || len(list) <= 0 {
		if batchCtx != nil && err != nil {
			batchCtx.RecordSourceError(s.Id, err.Error())
		}
		markRecoverFailure(s, fr, "recover_fetch", err)
		log.Println("Recover GetMovieDetail Error: ", err)
		return
	}

	written, err := saveCollectedFilmForCollect(ctx, s, fr.PageNumber, list)
	if err != nil {
		if batchCtx != nil {
			batchCtx.RecordSourceError(s.Id, err.Error())
		}
		markRecoverFailure(s, fr, "recover_save", err)
		log.Println("Recover saveCollectedFilm Error: ", err)
		return
	}
	if batchCtx != nil {
		batchCtx.addAffectedMIDs(s, fr.Hour, written.Affected)
		batchCtx.AddNotifyMIDs(s.Id, written.Notify)
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
	if !s.State {
		syslog.Warnf("[Spider] 站点已禁用，跳过失败页重试 source_id=%s name=%s page=%d", s.Id, s.Name, fr.PageNumber)
		return
	}
	claimed := filterCollectableSources([]model.FilmSource{*s}, "失败恢复")
	if len(claimed) == 0 {
		log.Printf("[Spider] 站点 %s 已在正采集队列中，跳过失败页重试", s.Id)
		return
	}
	startedAt := time.Now()
	batchCtx := newCollectBatchContext(model.CollectTriggerRecover, "失败恢复", claimed, startedAt, true)
	if err := collectLifecycle.beginSource(s.Id); err != nil {
		syslog.Errorf("[Spider] 站点 %s 无法启动失败页重试: %v", s.Id, err)
		batchCtx.close()
		return
	}
	defer collectLifecycle.endSource(s.Id)

	recoverFilmPage(context.Background(), s, fr, batchCtx)
	batchCtx.markSourceFinished(*s)
	if err := batchCtx.flushAndFinalize(); err != nil {
		syslog.Errorf("[Spider] 站点 %s 失败页重试缓存刷新失败: %v", s.Id, err)
	}
	batchCtx.emitSummary()
	batchCtx.close()
}

func FullRecoverSpider() {
	list := repository.PendingRecord()
	if len(list) == 0 {
		return
	}
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
	for i := range list {
		fr := list[i]
		s := repository.FindCollectSourceById(fr.OriginId)
		if s == nil {
			syslog.Errorf("[Spider] 重试失败: 站点 %s 不存在", fr.OriginId)
			continue
		}
		if !s.State {
			syslog.Warnf("[Spider] FullRecoverSpider: 站点已禁用，跳过重试 source_id=%s name=%s page=%d", s.Id, s.Name, fr.PageNumber)
			continue
		}
		if _, ok := seen[s.Id]; !ok {
			seen[s.Id] = struct{}{}
			sourcesToFlush = append(sourcesToFlush, *s)
			sourceByID[s.Id] = *s
		}
		recordsBySource[s.Id] = append(recordsBySource[s.Id], fr)
	}
	sourcesToFlush = filterCollectableSources(sourcesToFlush, "FullRecoverSpider")
	if len(sourcesToFlush) == 0 {
		log.Println("[Spider] FullRecoverSpider: 待恢复站点均已在正采集队列中，跳过")
		return
	}
	claimed := make(map[string]struct{}, len(sourcesToFlush))
	for _, source := range sourcesToFlush {
		claimed[source.Id] = struct{}{}
	}
	batchCtx := newCollectBatchContext(model.CollectTriggerRecover, "FullRecoverSpider", sourcesToFlush, startedAt)
	for sourceID, records := range recordsBySource {
		src, ok := sourceByID[sourceID]
		if !ok {
			continue
		}
		if _, ok := claimed[sourceID]; !ok {
			continue
		}
		recordsCopy := append([]model.FailureRecord(nil), records...)
		wg.Add(1)
		sem <- struct{}{}
		go func(source model.FilmSource, pending []model.FailureRecord) {
			defer wg.Done()
			defer func() { <-sem }()
			defer batchCtx.releaseSourceOccupy(source.Id)
			if err := collectLifecycle.beginSource(source.Id); err != nil {
				syslog.Errorf("[Spider] 站点 %s 无法启动失败页重试: %v", source.Id, err)
				return
			}
			defer collectLifecycle.endSource(source.Id)
			for i := range pending {
				record := pending[i]
				recoverFilmPage(context.Background(), &source, &record, batchCtx)
			}
			batchCtx.markSourceFinished(source)
			if isPreferredCollectSource(source.Id) {
				if err := batchCtx.finalizePrimarySource(source.Id); err != nil {
					syslog.Errorf("[FullRecoverSpider] 首选站 %s 缓存刷新失败: %v", source.Name, err)
				}
			}
		}(src, recordsCopy)
	}
	wg.Wait()
	if err := batchCtx.flushAndFinalize(); err != nil {
		syslog.Errorf("[FullRecoverSpider] 缓存刷新失败: %v", err)
	}
	batchCtx.emitSummary()
	batchCtx.close()
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

func CollectApiTest(s model.FilmSource) error {
	return CollectApiTestWithTimeout(s, 0)
}

func CollectApiTestChoosingProxy(s model.FilmSource, useProxy bool) error {
	return collectApiTest(s, 0, useProxy, true)
}

func CollectApiTestWithTimeout(s model.FilmSource, timeoutSeconds int) error {
	return collectApiTest(s, timeoutSeconds, false, false)
}

func collectApiTest(s model.FilmSource, timeoutSeconds int, useProxy bool, proxyChosen bool) error {
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("ac", "list")
	r.Params.Set("pg", "1")
	if timeoutSeconds > 0 {
		r.Header = map[string][]string{"timeout": {strconv.Itoa(timeoutSeconds)}}
	}
	if proxyChosen {
		if useProxy {
			cfg := repository.GetProxyConfig()
			if !cfg.Enabled || strings.TrimSpace(cfg.ProxyURL) == "" {
				return errors.New("测试失败, 代理未开启或地址为空")
			}
			r.ProxyURL = strings.TrimSpace(cfg.ProxyURL)
		}
	} else if s.ProxyCollect {
		if ok, proxy := repository.ResolveSpiderProxy(); ok {
			r.ProxyURL = proxy
		}
	} else if s.Id != "" {
		if ok, proxy := repository.ResolveSourceProxy(s.Id); ok {
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
