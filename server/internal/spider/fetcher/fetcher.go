// Package fetcher 采集取数：站点页数探测、分页拉取与分页结果投递。
//
// 本包只负责「取数」，落库、进度、通知等编排能力由采集编排层通过 Deps 注入，
// 因此不反向依赖 spider 根包。
package fetcher

import (
	"context"
	"errors"
	"fmt"
	"log"
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
	// pageFetchRetries 一页抓取失败后按采集间隔重试的次数。用完仍失败才记失败页。
	pageFetchRetries = 2
)

// Deps 采集编排层注入的外部能力；全部为必需项，缺项会在调用处 panic。
type Deps struct {
	// GetPageCount 探测站点总页数。
	GetPageCount func(s *model.FilmSource, r utils.RequestInfo) (int, error)
	// GetFilmDetail 拉取单页影片明细。
	GetFilmDetail func(s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error)
	// WaitTurn 等到本站可以发请求。不占下一次许可。
	WaitTurn func(ctx context.Context, s *model.FilmSource, tag string) (func(error), error)
	// ClaimTurn 拿到全局取页名额后占本站下一次许可。
	// false 表示许可已被别的请求占上，调用方应放开名额再等。
	// 为空表示 WaitTurn 已经占过许可。
	ClaimTurn func(s *model.FilmSource) bool
	// SavePage 单页落库（由写调度器串行化后调用）。
	SavePage func(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error)
	// SkipPublishOnError 该站该采集窗口出错时是否跳过发布。
	SkipPublishOnError func(s model.FilmSource, h int) bool
	// ResolveSpiderProxy 获取全局爬虫通道代理配置（纯内存读取，避免逐页查库）。
	ResolveSpiderProxy func() (bool, string, error)
	// ResolveSourceProxy 查询站点是否启用代理。配置读取失败必须返回错误。
	ResolveSourceProxy func(sourceID string) (bool, string, error)
	// OnSourceFailed 连续抓取失败且本轮还没有成功页时的通知。有成功页只写入采集记录。
	OnSourceFailed func(sourceID, sourceName, reason string)
}

var (
	deps           Deps
	fetchSlotsOnce sync.Once
	fetchSlots     *fetchGate
)

type fetchGate struct {
	mu       sync.Mutex
	cond     *sync.Cond
	inflight int
	limit    func() int
}

func newFetchGate(limit func() int) *fetchGate {
	gate := &fetchGate{limit: limit}
	gate.cond = sync.NewCond(&gate.mu)
	return gate
}

func (g *fetchGate) acquire(ctx context.Context) error {
	stop := context.AfterFunc(ctx, func() {
		g.mu.Lock()
		g.cond.Broadcast()
		g.mu.Unlock()
	})
	defer stop()
	g.mu.Lock()
	defer g.mu.Unlock()
	for {
		if err := ctx.Err(); err != nil {
			return err
		}
		limit := g.limit()
		if limit < 1 {
			limit = 1
		}
		if g.inflight < limit {
			g.inflight++
			return nil
		}
		// 上限变大时不会有 release。最多 20ms 再看一次当前限额。
		timer := time.AfterFunc(20*time.Millisecond, func() {
			g.mu.Lock()
			g.cond.Broadcast()
			g.mu.Unlock()
		})
		g.cond.Wait()
		timer.Stop()
	}
}

func (g *fetchGate) release() {
	g.mu.Lock()
	if g.inflight > 0 {
		g.inflight--
	}
	g.cond.Broadcast()
	g.mu.Unlock()
}

// Configure 注入外部能力，由采集编排层在 init 阶段调用一次。
func Configure(d Deps) {
	deps = d
}

func globalFetchSlots() *fetchGate {
	fetchSlotsOnce.Do(func() {
		fetchSlots = newFetchGate(config.FetchInFlightLimit)
	})
	return fetchSlots
}

func acquireSlot(ctx context.Context, slots *fetchGate) error {
	return slots.acquire(ctx)
}

func releaseSlot(slots *fetchGate) {
	slots.release()
}

// withFetchSlot 先等到本站许可到期，再占全局取页名额，马上发 HTTP 之前才占下一次许可。
// 等名额期间不推进许可。占许可失败就放开名额，重新等间隔。HTTP 结束立即释放名额。
func withFetchSlot(ctx context.Context, s *model.FilmSource, tag string, call func(context.Context) error) error {
	if deps.WaitTurn == nil {
		return errors.New("WaitTurn 未注入")
	}
	for {
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
		if deps.ClaimTurn != nil && !deps.ClaimTurn(s) {
			releaseSlot(globalFetchSlots())
			continue
		}
		callErr := func() error {
			defer releaseSlot(globalFetchSlots())
			err := callHeldFetch(ctx, call)
			release(err)
			return err
		}()
		return callErr
	}
}

func callHeldFetch(ctx context.Context, call func(context.Context) error) error {
	reqCtx, cancel := context.WithTimeout(ctx, singlePageFetchTimeout)
	defer cancel()
	return call(reqCtx)
}

func GetPageCountWithRetry(ctx context.Context, s *model.FilmSource, r utils.RequestInfo) (int, error) {
	pageCount, err := fetchPageCountOnce(ctx, s, r)
	if err == nil || ctx.Err() != nil {
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
	page := r.Params.Get("pg")
	if page == "" {
		page = "-"
	}
	srcName := ""
	if s != nil {
		srcName = s.Name
	}
	list, err := fetchFilmDetailOnce(ctx, s, r)
	for attempt := 1; attempt <= pageFetchRetries && err != nil && ctx.Err() == nil; attempt++ {
		log.Printf("[Spider] 站点 %s 第 %s 页抓取失败，按采集间隔重试 %d/%d: %v", srcName, page, attempt, pageFetchRetries, err)
		list, err = fetchFilmDetailOnce(ctx, s, r)
		if err == nil {
			log.Printf("[Spider] 站点 %s 第 %s 页第 %d 次重试成功", srcName, page, attempt)
		}
	}
	return list, err
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
