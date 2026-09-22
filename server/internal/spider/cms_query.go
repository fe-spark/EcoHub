package spider

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"
	"unicode/utf8"

	"server/internal/model"
	"server/internal/utils"
)

const cmsQueryTimeoutSec = 8
const cmsSearchUnsupportedTTL = 30 * time.Minute

// ErrCMSSearchUnsupported 源站明确关闭了 ac=list&wd= 搜索（例如返回「暂不支持搜索」）。
var ErrCMSSearchUnsupported = errors.New("cms search unsupported")

var cmsSearchUnsupported sync.Map // uri -> expiry time.Time

func rememberCMSSearchUnsupported(uri string) {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return
	}
	cmsSearchUnsupported.Store(uri, time.Now().Add(cmsSearchUnsupportedTTL))
}

func isCMSSearchUnsupportedCached(uri string) bool {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return false
	}
	v, ok := cmsSearchUnsupported.Load(uri)
	if !ok {
		return false
	}
	until, ok := v.(time.Time)
	if !ok || time.Now().After(until) {
		cmsSearchUnsupported.Delete(uri)
		return false
	}
	return true
}

func isCMSSearchUnsupportedBody(body string) bool {
	trimmed := strings.TrimSpace(body)
	if strings.Contains(trimmed, "暂不支持搜索") {
		return true
	}
	// 部分 MacCMS 关闭 wd 搜索时返回纯文本，原文带拼写错误 serarch。
	lower := strings.ToLower(trimmed)
	return strings.Contains(lower, "err not serarch") || strings.Contains(lower, "err not search")
}

func truncateCMSBody(body string, maxRunes int) string {
	trimmed := strings.Join(strings.Fields(strings.TrimSpace(body)), " ")
	if maxRunes <= 0 || utf8.RuneCountInString(trimmed) <= maxRunes {
		return trimmed
	}
	runes := []rune(trimmed)
	return string(runes[:maxRunes]) + "..."
}

// SearchSourceList 按关键词请求采集源 MacCMS 列表搜索（ac=list&wd=）。不写库。
func SearchSourceList(uri, keyword string, page int) (model.FilmListPage, error) {
	return SearchSourceListWithProxy(uri, keyword, page, "")
}

// SearchSourceListWithProxy 支持携带网络代理的 MacCMS 列表搜索。不写库。
func SearchSourceListWithProxy(uri, keyword string, page int, proxyURL string) (model.FilmListPage, error) {
	var empty model.FilmListPage
	uri = strings.TrimSpace(uri)
	keyword = strings.TrimSpace(keyword)
	if uri == "" || keyword == "" {
		return empty, errors.New("source uri and keyword are required")
	}
	if isCMSSearchUnsupportedCached(uri) {
		return empty, ErrCMSSearchUnsupported
	}
	if page <= 0 {
		page = 1
	}
	r := utils.RequestInfo{
		Uri:      uri,
		Params:   url.Values{},
		Header:   http.Header{},
		ProxyURL: proxyURL,
	}
	r.Header.Set("timeout", strconv.Itoa(cmsQueryTimeoutSec))
	r.Params.Set("ac", "list")
	r.Params.Set("wd", keyword)
	r.Params.Set("pg", strconv.Itoa(page))
	utils.ApiGet(&r)
	if len(r.Resp) == 0 {
		errMsg := r.Err
		if errMsg == "" {
			errMsg = "response is empty"
		}
		return empty, errors.New(errMsg)
	}
	body := string(r.Resp)
	if isCMSSearchUnsupportedBody(body) {
		rememberCMSSearchUnsupported(uri)
		return empty, fmtCMSSearchUnsupported(body)
	}
	var pageData model.FilmListPage
	if err := json.Unmarshal(r.Resp, &pageData); err != nil {
		return empty, err
	}
	return pageData, nil
}

func fmtCMSSearchUnsupported(body string) error {
	detail := truncateCMSBody(body, 40)
	if detail == "" || !strings.Contains(detail, "暂不支持搜索") {
		return ErrCMSSearchUnsupported
	}
	return fmt.Errorf("%w: %s", ErrCMSSearchUnsupported, detail)
}

// FetchSourceDetails 按源站 vod_id 拉详情（ac=detail&ids=）。不写库。
func FetchSourceDetails(uri, ids string) ([]model.MovieDetail, error) {
	return FetchSourceDetailsWithProxy(uri, ids, "")
}

// FetchSourceDetailsWithProxy 支持携带网络代理的源站详情拉取。不写库。
func FetchSourceDetailsWithProxy(uri, ids string, proxyURL string) ([]model.MovieDetail, error) {
	uri = strings.TrimSpace(uri)
	ids = strings.TrimSpace(ids)
	if uri == "" || ids == "" {
		return nil, errors.New("source uri and ids are required")
	}
	r := utils.RequestInfo{
		Uri:      uri,
		Params:   url.Values{},
		Header:   http.Header{},
		ProxyURL: proxyURL,
	}
	r.Header.Set("timeout", strconv.Itoa(cmsQueryTimeoutSec))
	r.Params.Set("ids", ids)
	return spiderCore.GetFilmDetail(r)
}

// FetchSourceCategoryDetails 按源站分类 ID (t=) 或最新页码拉取明细（ac=detail）。不写库。
func FetchSourceCategoryDetails(uri string, cid int64, page int) ([]model.MovieDetail, error) {
	return FetchSourceCategoryDetailsWithProxy(uri, cid, page, "")
}

// FetchSourceCategoryDetailsWithProxy 支持携带网络代理的分类明细拉取。不写库。
func FetchSourceCategoryDetailsWithProxy(uri string, cid int64, page int, proxyURL string) ([]model.MovieDetail, error) {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return nil, errors.New("source uri is required")
	}
	if page <= 0 {
		page = 1
	}
	r := utils.RequestInfo{
		Uri:      uri,
		Params:   url.Values{},
		Header:   http.Header{},
		ProxyURL: proxyURL,
	}
	r.Header.Set("timeout", strconv.Itoa(cmsQueryTimeoutSec))
	r.Params.Set("ac", "detail")
	if cid > 0 {
		r.Params.Set("t", strconv.FormatInt(cid, 10))
	}
	r.Params.Set("pg", strconv.Itoa(page))
	return spiderCore.GetFilmDetail(r)
}
