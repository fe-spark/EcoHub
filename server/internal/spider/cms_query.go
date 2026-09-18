package spider

import (
	"encoding/json"
	"errors"
	"net/http"
	"net/url"
	"strconv"
	"strings"

	"server/internal/model"
	"server/internal/utils"
)

const cmsQueryTimeoutSec = 8

// SearchSourceList 按关键词请求采集源 MacCMS 列表搜索（ac=list&wd=）。不写库。
func SearchSourceList(uri, keyword string, page int) (model.FilmListPage, error) {
	var empty model.FilmListPage
	uri = strings.TrimSpace(uri)
	keyword = strings.TrimSpace(keyword)
	if uri == "" || keyword == "" {
		return empty, errors.New("source uri and keyword are required")
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
	var pageData model.FilmListPage
	if err := json.Unmarshal(r.Resp, &pageData); err != nil {
		return empty, err
	}
	return pageData, nil
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
