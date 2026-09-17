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
	// filmDetailRetryTimes 分页拉取默认重试次数。
	filmDetailRetryTimes = 3
	// rateLimitRetryTimes 命中限流后的重试次数上限。
	rateLimitRetryTimes = 6
)

// Deps 采集编排层注入的外部能力；全部为必需项，缺项会在调用处 panic。
type Deps struct {
	// GetPageCount 探测站点总页数。
	GetPageCount func(r utils.RequestInfo) (int, error)
	// GetFilmDetail 拉取单页影片明细。
	GetFilmDetail func(r utils.RequestInfo) ([]model.MovieDetail, error)
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
	// NoteSourceError 记录单源失败原因，供批次摘要使用。
	NoteSourceError func(sourceID, reason string)
	// NotifySourceFailed 单源失败即时通知。
	NotifySourceFailed func(sourceID, sourceName, reason string)
	// BatchSummaryEnabled 批次摘要事件是否开启（决定失败是否单独通知）。
	BatchSummaryEnabled func() bool
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
		pageCount, err := deps.GetPageCount(r)
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

		r.Ctx = ctx
		release, err := deps.WaitTurn(ctx, s, fmt.Sprintf("分页请求 pg=%s attempt=%d ", page, attempt))
		if err != nil {
			return nil, err
		}
		list, err := deps.GetFilmDetail(r)
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
	if deps.LiveTaskCount() <= 1 {
		if solo <= 0 {
			return 1
		}
		return solo
	}
	if base <= 0 {
		return 1
	}
	return base
}

func buildPageRequest(s *model.FilmSource, h, pg int) utils.RequestInfo {
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("pg", fmt.Sprint(pg))
	if h > 0 {
		r.Params.Set("h", fmt.Sprint(h))
	}
	return r
}
