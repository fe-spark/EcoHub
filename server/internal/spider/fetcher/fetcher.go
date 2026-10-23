// Package fetcher 采集取数：站点页数探测、分页拉取（含限流退避重试）与分页结果投递。
//
// 本包只负责「取数」，落库、进度、通知等编排能力由采集编排层通过 Deps 注入，
// 因此不反向依赖 spider 根包。
package fetcher

import (
	"context"
	"errors"
	"fmt"
	"math/rand"
	"net/url"
	"time"

	"server/internal/config"
	"server/internal/model"
	"server/internal/spider/scheduler"
	"server/internal/utils"
)

const (
	// pageCountRetryTimes 页数探测默认重试次数。
	pageCountRetryTimes = 3
	// filmDetailRetryTimes 分页拉取默认重试次数（1 次初试 + 1 次重试，超时或坏页快速登记入库，不卡死流水线）。
	filmDetailRetryTimes = 2
	// rateLimitRetryTimes 命中限流后的重试次数。
	// 3 次只覆盖约 9 秒，源站此时仍返回 429。多留几次，等冷却拉到 30 秒后再记失败。
	rateLimitRetryTimes = 6
	// singlePageFetchTimeout 单页抓取最大超时，避免深分页慢查询长期卡死 worker。
	singlePageFetchTimeout = 15 * time.Second
)

// Deps 采集编排层注入的外部能力；全部为必需项，缺项会在调用处 panic。
type Deps struct {
	// GetPageCount 探测站点总页数。
	GetPageCount func(s *model.FilmSource, r utils.RequestInfo) (int, error)
	// GetFilmDetail 拉取单页影片明细。
	GetFilmDetail func(s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error)
	// WaitTurn 站点级请求闸门，返回 release(err) 回调用于更新自适应限速。
	WaitTurn func(ctx context.Context, s *model.FilmSource, tag string) (func(error), error)
	// LiveTaskCount 当前活跃采集任务数，用于在单站/多站并发之间切换页并发。
	LiveTaskCount func() int
	// SavePage 单页落库（由写调度器串行化后调用）。
	SavePage func(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error)
	// SavePageFailure 记录失败页现场，供后续重试。
	SavePageFailure func(s *model.FilmSource, h, page int, stage string, err error)
	// SkipPublishOnError 该站该采集窗口出错时是否跳过发布。
	SkipPublishOnError func(s model.FilmSource, h int) bool
	// ResolveSpiderProxy 获取全局爬虫通道代理配置（纯内存读取，避免逐页查库）。
	ResolveSpiderProxy func() (bool, string, error)
	// ResolveSourceProxy 查询站点是否启用代理。配置读取失败必须返回错误。
	ResolveSourceProxy func(sourceID string) (bool, string, error)
	// OnSourceFailed 单站连续失败达到上限终止采集时的通知回调（可选）。
	OnSourceFailed func(sourceID, sourceName, reason string)
}

var deps Deps

// Configure 注入外部能力，由采集编排层在 init 阶段调用一次。
func Configure(d Deps) {
	deps = d
}

func GetPageCountWithRetry(ctx context.Context, s *model.FilmSource, r utils.RequestInfo) (int, error) {
	var lastErr error
	maxAttempts := pageCountRetryTimes
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		r.Ctx = ctx
		release, err := deps.WaitTurn(ctx, s, fmt.Sprintf("页数请求 attempt=%d ", attempt))
		if err != nil {
			return 0, err
		}
		pageCount, err := deps.GetPageCount(s, r)
		if err == nil {
			release(nil)
			return pageCount, nil
		}
		release(err)
		lastErr = err
		if utils.IsRateLimitedErr(lastErr) && maxAttempts < rateLimitRetryTimes {
			maxAttempts = rateLimitRetryTimes
		}
		if attempt < maxAttempts {
			if waitErr := waitRetryBackoff(ctx, attempt); waitErr != nil {
				return 0, waitErr
			}
		}
	}
	return 0, lastErr
}

func GetFilmDetailWithRetry(ctx context.Context, s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
	var lastErr error
	page := r.Params.Get("pg")
	if page == "" {
		page = "-"
	}
	maxAttempts := filmDetailRetryTimes
	for attempt := 1; attempt <= maxAttempts; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		// 等令牌沿用采集任务上下文。限流冷却最长 30 秒，不能套进单页 15 秒超时。
		release, err := deps.WaitTurn(ctx, s, fmt.Sprintf("分页请求 pg=%s attempt=%d ", page, attempt))
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			return nil, err
		}
		reqCtx, reqCancel := context.WithTimeout(ctx, singlePageFetchTimeout)
		r.Ctx = reqCtx
		list, err := deps.GetFilmDetail(s, r)
		reqCancel()
		if err == nil && len(list) > 0 {
			release(nil)
			return list, nil
		}
		release(err)
		if err != nil {
			lastErr = err
		} else {
			lastErr = errors.New("response list is empty")
		}
		if utils.IsRateLimitedErr(lastErr) && maxAttempts < rateLimitRetryTimes {
			maxAttempts = rateLimitRetryTimes
		}
		if attempt < maxAttempts {
			if waitErr := waitRetryBackoff(ctx, attempt); waitErr != nil {
				return nil, waitErr
			}
		}
	}
	return nil, lastErr
}

func waitRetryBackoff(ctx context.Context, attempt int) error {
	if attempt <= 0 {
		attempt = 1
	}
	base := time.Duration(1<<uint(attempt-1)) * time.Second
	if base > 10*time.Second {
		base = 10 * time.Second
	}
	jitter := time.Duration(rand.Intn(500)) * time.Millisecond
	delay := base + jitter

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func GetSourcePageConcurrency(_ *model.FilmSource) int {
	base := config.CollectPageWorkers
	if base <= 0 {
		base = config.DefaultCollectPageWorkers
	}
	solo := config.CollectPageWorkersSolo
	if solo <= 0 {
		solo = config.DefaultCollectPageWorkersSolo
	}
	if solo < base {
		solo = base
	}
	live := 1
	if deps.LiveTaskCount != nil {
		if n := deps.LiveTaskCount(); n > 1 {
			live = n
		}
	}
	if live <= 1 {
		if solo <= 0 {
			return 1
		}
		return solo
	}
	if base <= 0 {
		return 1
	}
	budget := config.CollectFetchInFlight
	if budget <= 0 {
		budget = base * 2
	}
	perSource := budget / live
	if perSource < 2 {
		perSource = 2
	}
	if perSource > base {
		perSource = base
	}
	return perSource
}

// GetSourcePageConcurrencyMax 获取单站允许的最大并发协程上限（用于弹性 worker 协程池容量）。
func GetSourcePageConcurrencyMax(_ *model.FilmSource) int {
	solo := config.CollectPageWorkersSolo
	if solo <= 0 {
		solo = config.DefaultCollectPageWorkersSolo
	}
	base := config.CollectPageWorkers
	if base <= 0 {
		base = config.DefaultCollectPageWorkers
	}
	if solo < base {
		solo = base
	}
	return solo
}

func buildPageRequest(s *model.FilmSource, h, pg int) (utils.RequestInfo, error) {
	if s == nil {
		return utils.RequestInfo{}, errors.New("采集站为空")
	}
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}, SkipTLSVerify: s.SkipTLSVerify}
	r.Params.Set("pg", fmt.Sprint(pg))
	if h > 0 {
		r.Params.Set("h", fmt.Sprint(h))
	}
	if s.ResolveFormat() == model.SourceFormatXML {
		r.Params.Set("ac", "videolist")
	}
	if s.ProxyCollect {
		if deps.ResolveSpiderProxy != nil {
			ok, proxy, err := deps.ResolveSpiderProxy()
			if err != nil {
				return r, err
			}
			if ok {
				r.ProxyURL = proxy
			}
		}
		return r, nil
	}
	if s.Id != "" && deps.ResolveSourceProxy != nil {
		ok, proxy, err := deps.ResolveSourceProxy(s.Id)
		if err != nil {
			return r, err
		}
		if ok {
			r.ProxyURL = proxy
		}
	}
	return r, nil
}
