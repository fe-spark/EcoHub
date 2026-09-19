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
	return strings.Contains(strings.TrimSpace(body), "暂不支持搜索")
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
		Uri:    uri,
		Params: url.Values{},
		Header: http.Header{},
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
	if detail == "" {
		return ErrCMSSearchUnsupported
	}
	return fmt.Errorf("%w: %s", ErrCMSSearchUnsupported, detail)
}

// FetchSourceDetails 按源站 vod_id 拉详情（ac=detail&ids=）。不写库。
func FetchSourceDetails(uri, ids string) ([]model.MovieDetail, error) {
	uri = strings.TrimSpace(uri)
	ids = strings.TrimSpace(ids)
	if uri == "" || ids == "" {
		return nil, errors.New("source uri and ids are required")
	}
	r := utils.RequestInfo{
		Uri:    uri,
		Params: url.Values{},
		Header: http.Header{},
	}
	r.Header.Set("timeout", strconv.Itoa(cmsQueryTimeoutSec))
	r.Params.Set("ids", ids)
	return spiderCore.GetFilmDetail(r)
}

// FetchSourceCategoryDetails 按源站分类 ID (t=) 或最新页码拉取明细（ac=detail）。不写库。
func FetchSourceCategoryDetails(uri string, cid int64, page int) ([]model.MovieDetail, error) {
	uri = strings.TrimSpace(uri)
	if uri == "" {
		return nil, errors.New("source uri is required")
	}
	if page <= 0 {
		page = 1
	}
	r := utils.RequestInfo{
		Uri:    uri,
		Params: url.Values{},
		Header: http.Header{},
	}
	r.Header.Set("timeout", strconv.Itoa(cmsQueryTimeoutSec))
	r.Params.Set("ac", "detail")
	if cid > 0 {
		r.Params.Set("t", strconv.FormatInt(cid, 10))
	}
	r.Params.Set("pg", strconv.Itoa(page))
	return spiderCore.GetFilmDetail(r)
}

