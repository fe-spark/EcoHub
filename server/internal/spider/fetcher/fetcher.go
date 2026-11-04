// Package fetcher 采集取数：站点页数探测、分页拉取与分页结果投递。
//
// 本包只负责「取数」，落库、进度、通知等编排能力由采集编排层通过 Deps 注入，
// 因此不反向依赖 spider 根包。
package fetcher

import (
	"context"
	"errors"
	"fmt"
	"net/url"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/model"
	"server/internal/spider/scheduler"
	"server/internal/utils"
)

const (
	// singlePageFetchTimeout 单页抓取最大超时，避免深分页慢查询长期占着取页名额。
	singlePageFetchTimeout = 15 * time.Second
)

// Deps 采集编排层注入的外部能力；全部为必需项，缺项会在调用处 panic。
type Deps struct {
	// GetPageCount 探测站点总页数。
	GetPageCount func(s *model.FilmSource, r utils.RequestInfo) (int, error)
	// GetFilmDetail 拉取单页影片明细。
	GetFilmDetail func(s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error)
	// WaitTurn 站点级请求闸门。先等间隔或冷却，再占取页名额。
	WaitTurn func(ctx context.Context, s *model.FilmSource, tag string) (func(error), error)
	// SavePage 单页落库（由写调度器串行化后调用）。
	SavePage func(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error)
	// SkipPublishOnError 该站该采集窗口出错时是否跳过发布。
	SkipPublishOnError func(s model.FilmSource, h int) bool
	// ResolveSpiderProxy 获取全局爬虫通道代理配置（纯内存读取，避免逐页查库）。
	ResolveSpiderProxy func() (bool, string, error)
	// ResolveSourceProxy 查询站点是否启用代理。配置读取失败必须返回错误。
	ResolveSourceProxy func(sourceID string) (bool, string, error)
	// OnSourceFailed 站点尚无成功页而停止采集时的通知回调（可选）。
	OnSourceFailed func(sourceID, sourceName, reason string)
}

var (
	deps           Deps
	fetchSlotsOnce sync.Once
	fetchSlots     chan struct{}
)

// Configure 注入外部能力，由采集编排层在 init 阶段调用一次。
func Configure(d Deps) {
	deps = d
}

func globalFetchSlots() chan struct{} {
	fetchSlotsOnce.Do(func() {
		n := config.CollectFetchInFlight
		if n < 1 {
			n = 1
		}
		fetchSlots = make(chan struct{}, n)
	})
	return fetchSlots
}

func acquireSlot(ctx context.Context, slots chan struct{}) error {
	select {
	case <-ctx.Done():
		return ctx.Err()
	case slots <- struct{}{}:
		return nil
	}
}

func releaseSlot(slots chan struct{}) {
	<-slots
}

// withFetchSlot 先等站点间隔或冷却，再占用一个全局取页名额。HTTP 结束立即释放。
func withFetchSlot(ctx context.Context, s *model.FilmSource, tag string, call func(context.Context) error) error {
	if deps.WaitTurn == nil {
		return errors.New("WaitTurn 未注入")
	}
	release, err := deps.WaitTurn(ctx, s, tag)
	if err != nil {
		return err
	}
	if release == nil {
		release = func(error) {}
	}
	if err := acquireSlot(ctx, globalFetchSlots()); err != nil {
		return err
	}
	defer releaseSlot(globalFetchSlots())

	reqCtx, cancel := context.WithTimeout(ctx, singlePageFetchTimeout)
	defer cancel()
	callErr := call(reqCtx)
	release(callErr)
	return callErr
}

func GetPageCountWithRetry(ctx context.Context, s *model.FilmSource, r utils.RequestInfo) (int, error) {
	pageCount, err := fetchPageCountOnce(ctx, s, r)
	if err == nil || !utils.IsRateLimitedErr(err) {
		return pageCount, err
	}
	return fetchPageCountOnce(ctx, s, r)
}

func fetchPageCountOnce(ctx context.Context, s *model.FilmSource, r utils.RequestInfo) (int, error) {
	var pageCount int
	err := withFetchSlot(ctx, s, "页数请求 ", func(reqCtx context.Context) error {
		r.Ctx = reqCtx
		n, ferr := deps.GetPageCount(s, r)
		if ferr != nil {
			return ferr
		}
		pageCount = n
		return nil
	})
	if err != nil {
		return 0, err
	}
	return pageCount, nil
}

func GetFilmDetailWithRetry(ctx context.Context, s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
	list, err := fetchFilmDetailOnce(ctx, s, r)
	if err == nil || !utils.IsRateLimitedErr(err) {
		return list, err
	}
	return fetchFilmDetailOnce(ctx, s, r)
}

func fetchFilmDetailOnce(ctx context.Context, s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
	page := r.Params.Get("pg")
	if page == "" {
		page = "-"
	}
	var list []model.MovieDetail
	err := withFetchSlot(ctx, s, fmt.Sprintf("分页请求 pg=%s ", page), func(reqCtx context.Context) error {
		r.Ctx = reqCtx
		got, ferr := deps.GetFilmDetail(s, r)
		if ferr != nil {
			return ferr
		}
		if len(got) == 0 {
			return errors.New("response list is empty")
		}
		list = got
		return nil
	})
	if err != nil {
		return nil, err
	}
	return list, nil
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
