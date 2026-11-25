package collect

import (
	"server/internal/repobase"
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/model"
	filmrepo "server/internal/film"
	"server/internal/collect/scheduler"
	"server/internal/utils"
)

var (
	siteResetting        atomic.Bool
	fullResetRunningMu   sync.RWMutex
	fullResetRunningFn   func() bool
)

// RegisterSiteResetChecker 注册全站重置生命周期状态检查器（覆盖至数据回写初始化完成）。
func RegisterSiteResetChecker(fn func() bool) {
	fullResetRunningMu.Lock()
	fullResetRunningFn = fn
	fullResetRunningMu.Unlock()
}

// IsSiteResetRunning 片库数据是否正在清空/重置中。
func IsSiteResetRunning() bool {
	if siteResetting.Load() {
		return true
	}
	fullResetRunningMu.RLock()
	fn := fullResetRunningFn
	fullResetRunningMu.RUnlock()
	return fn != nil && fn()
}

const resetReadyWaitTimeout = 2 * time.Minute

func ClearSpider() error {
	siteResetting.Store(true)
	defer siteResetting.Store(false)
	filmrepo.ReportResetProgress(5, "正在停止采集任务")
	StopAllCollect("数据重置，停止采集")
	clearPendingCronDispatch()
	if err := clearAndWaitMaintenanceQueue(resetReadyWaitTimeout); err != nil {
		return err
	}
	filmrepo.ReportResetProgress(10, "正在等待在途写入与索引结束")
	if err := waitResetReady(resetReadyWaitTimeout); err != nil {
		return err
	}
	filmrepo.ReportResetProgress(15, "正在清空数据")
	return collectLifecycle.runExclusive(func() error {
		ClearAllCrons()
		ClearAllLimiters()
		ResetCollectStatsCoalescer()
		return filmrepo.FilmZero()
	})
}

// waitResetReady 等待在途写库与检索索引重建结束，避免清表与长查询争夺表元数据锁。
func waitResetReady(timeout time.Duration) error {
	ctx, cancel := context.WithTimeout(context.Background(), timeout)
	defer cancel()
	if err := scheduler.WaitPendingWrites(ctx); err != nil {
		return fmt.Errorf("等待在途写入结束失败: %w", err)
	}
	for {
		reason := SearchWorkBusyReason()
		if reason == "" {
			return nil
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待检索索引空闲超时: %s", reason)
		case <-time.After(100 * time.Millisecond):
		}
	}
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
	if (proxyChosen && useProxy) || (!proxyChosen && s.ProxyCollect) {
		ok, proxy, err := repobase.ResolveSpiderProxy()
		if err != nil {
			return err
		}
		if !ok || strings.TrimSpace(proxy) == "" {
			return errors.New("测试失败, 全局代理或采集代理模块未开启")
		}
		r.ProxyURL = proxy
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
