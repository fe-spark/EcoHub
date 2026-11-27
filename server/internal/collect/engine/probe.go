package engine

import (
	"context"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/utils"
)

const failedPageErrorRunes = 2000

func formatFailedPagesError(failedPages []int, totalFailed int, lastErr string) string {
	if totalFailed <= 0 {
		return ""
	}
	pagesDesc := fmt.Sprintf("共 %d 页失败", totalFailed)
	if totalFailed == 1 && len(failedPages) == 1 {
		pagesDesc = fmt.Sprintf("第 %d 页失败", failedPages[0])
	}
	trimmedErr := strings.TrimSpace(lastErr)
	runes := []rune(trimmedErr)
	if len(runes) > failedPageErrorRunes {
		trimmedErr = string(runes[:failedPageErrorRunes]) + "..."
	}
	if trimmedErr != "" {
		return pagesDesc + "。抓取失败: " + trimmedErr
	}
	return pagesDesc
}

const (
	singlePageFetchTimeout = 15 * time.Second
	pageFetchRetries       = 2
	pageCountRetryGap      = 2 * time.Second
)

// ProxyResolver 获取爬虫代理开关与地址。
type ProxyResolver func() (bool, string, error)

// Page1Result 第 1 页探测产物，包含总页数与已解析的第 1 页明细（供直接复用落库）。
type Page1Result struct {
	PageCount int
	Items     []model.MovieDetail
}

// BuildPageRequest 构造分页采集请求参数与代理配置。
func BuildPageRequest(s *model.FilmSource, h, pg int, resolveProxy ProxyResolver) (utils.RequestInfo, error) {
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
	if s.ProxyCollect && resolveProxy != nil {
		ok, proxy, err := resolveProxy()
		if err != nil {
			return r, err
		}
		if ok {
			r.ProxyURL = proxy
		}
	}
	return r, nil
}

func runHeldFetch(ctx context.Context, ts *ThrottleStore, sourceID string, fn func(context.Context) error) error {
	if ts == nil {
		ts = defaultThrottle
	}
	release, err := ts.Acquire(ctx, sourceID)
	if err != nil {
		return err
	}
	reqCtx, cancel := context.WithTimeout(ctx, singlePageFetchTimeout)
	defer cancel()
	err = fn(reqCtx)
	release(err)
	return err
}

// ProbePage1WithRetry 带重试探测站点总页数并返回第 1 页已解析影片明细。
func ProbePage1WithRetry(
	ctx context.Context,
	s *model.FilmSource,
	r utils.RequestInfo,
	collector Collector,
	ts *ThrottleStore,
) (*Page1Result, error) {
	if collector == nil {
		collector = ResolveCollector(s.ResolveFormat())
	}
	tryOnce := func() (*Page1Result, error) {
		var res Page1Result
		err := runHeldFetch(ctx, ts, sourceIDOf(s), func(reqCtx context.Context) error {
			req := r
			req.Ctx = reqCtx
			count, items, ferr := collector.ProbePage1(req)
			if ferr != nil {
				return ferr
			}
			res = Page1Result{PageCount: count, Items: items}
			return nil
		})
		if err != nil {
			return nil, err
		}
		return &res, nil
	}

	res, err := tryOnce()
	for attempt := 1; attempt <= pageFetchRetries && err != nil && ctx.Err() == nil; attempt++ {
		log.Printf("[Spider] 站点 %s 总页探测失败，重试 %d/%d: %v", sourceNameOf(s), attempt, pageFetchRetries, err)
		timer := time.NewTimer(pageCountRetryGap)
		select {
		case <-ctx.Done():
			timer.Stop()
			return nil, ctx.Err()
		case <-timer.C:
		}
		res, err = tryOnce()
	}
	return res, err
}

// GetPageCountWithRetry 仅探测站点总页数（供后台总页数巡检调用）。
func GetPageCountWithRetry(
	ctx context.Context,
	s *model.FilmSource,
	r utils.RequestInfo,
	collector Collector,
	ts *ThrottleStore,
) (int, error) {
	res, err := ProbePage1WithRetry(ctx, s, r, collector, ts)
	if err != nil {
		return 0, err
	}
	return res.PageCount, nil
}

// GetFilmDetailWithRetry 带重试抓取单页影片明细。
func GetFilmDetailWithRetry(
	ctx context.Context,
	s *model.FilmSource,
	r utils.RequestInfo,
	collector Collector,
	ts *ThrottleStore,
) ([]model.MovieDetail, error) {
	if collector == nil {
		collector = ResolveCollector(s.ResolveFormat())
	}
	page := r.Params.Get("pg")
	if page == "" {
		page = "-"
	}
	tryOnce := func() ([]model.MovieDetail, error) {
		var list []model.MovieDetail
		err := runHeldFetch(ctx, ts, sourceIDOf(s), func(reqCtx context.Context) error {
			req := r
			req.Ctx = reqCtx
			got, ferr := collector.GetFilmDetail(req)
			if ferr != nil {
				return ferr
			}
			list = got
			return nil
		})
		if err != nil {
			return nil, err
		}
		return list, nil
	}

	list, err := tryOnce()
	for attempt := 1; attempt <= pageFetchRetries && err != nil && ctx.Err() == nil; attempt++ {
		log.Printf("[Spider] 站点 %s 第 %s 页抓取失败，重试 %d/%d: %v", sourceNameOf(s), page, attempt, pageFetchRetries, err)
		list, err = tryOnce()
		if err == nil {
			log.Printf("[Spider] 站点 %s 第 %s 页第 %d 次重试成功", sourceNameOf(s), page, attempt)
		}
	}
	return list, err
}

func sourceIDOf(s *model.FilmSource) string {
	if s == nil {
		return ""
	}
	return s.Id
}

func sourceNameOf(s *model.FilmSource) string {
	if s == nil {
		return ""
	}
	return s.Name
}
