package spider

import (
	"bytes"
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"strconv"
	"strings"

	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/utils"
)

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
