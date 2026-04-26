package spider

import (
	"encoding/json"
	"errors"
	"log"
	"net/url"
	"strconv"

	"server/internal/model"
	"server/internal/spider/conver"
	"server/internal/utils"
)

const (
	categoryHintMinPages        = 1
	categoryHintMaxPages        = 10
	categoryHintStablePageLimit = 3
	categoryHintTargetCoverage  = 0.7
)

/*
	Spider 数据 爬取 & 处理 & 转换
*/

type FilmCollect interface {
	// GetCategoryTree 获取影视分类数据
	GetCategoryTree(r utils.RequestInfo) (*model.CategoryTree, error)
	// GetPageCount 获取API接口的分页页数
	GetPageCount(r utils.RequestInfo) (count int, err error)
	// GetFilmDetail 获取影片详情信息,返回影片详情列表
	GetFilmDetail(r utils.RequestInfo) (list []model.MovieDetail, err error)
}

// ------------------------------------------------- JSON Collect -------------------------------------------------

// JsonCollect 处理返回值为JSON格式的采集数据
type JsonCollect struct{}

// GetCategoryTree 获取分类树形数据
func (jc *JsonCollect) GetCategoryTree(r utils.RequestInfo) (*model.CategoryTree, error) {
	// 设置请求参数信息
	r.Params.Set(`ac`, "list")
	r.Params.Set(`pg`, "1")
	// 执行请求, 获取一次list数据
	utils.ApiGet(&r)
	// 解析resp数据
	filmListPage := model.FilmListPage{}
	if len(r.Resp) <= 0 {
		log.Println("filmListPage 数据获取异常 : Resp Is Empty")
		return nil, errors.New("filmListPage 数据获取异常 : Resp Is Empty")
	}
	err := json.Unmarshal(r.Resp, &filmListPage)
	// 获取分类列表信息
	cl := filmListPage.Class
	parentHints := jc.inferCategoryParents(r, cl)
	// 组装分类数据信息树形结构
	tree := conver.GenCategoryTreeWithParentHints(cl, parentHints)

	return tree, err
}

func (jc *JsonCollect) inferCategoryParents(r utils.RequestInfo, classes []model.FilmClass) map[int64]int64 {
	if !needsCategoryParentInference(classes) {
		return nil
	}

	known := make(map[int64]struct{}, len(classes))
	for _, item := range classes {
		known[item.ID] = struct{}{}
	}

	hints := make(map[int64]int64)
	stablePages := 0
	totalPages := 0
	targetHintCount := resolveCategoryHintTarget(len(classes))
	for page := 1; page <= categoryHintMaxPages; page++ {
		details, pageCount, err := jc.fetchFilmDetailsPage(r, page)
		if err != nil {
			log.Printf("[Spider] 分类父级推断失败: page=%d err=%v", page, err)
			continue
		}
		if pageCount > 0 {
			totalPages = pageCount
		}
		beforeCount := len(hints)
		for _, item := range details {
			if item.TypeID <= 0 || item.TypeID1 <= 0 || item.TypeID == item.TypeID1 {
				continue
			}
			if _, ok := known[item.TypeID]; !ok {
				continue
			}
			if _, ok := known[item.TypeID1]; !ok {
				continue
			}
			if existedPid, ok := hints[item.TypeID]; ok && existedPid != item.TypeID1 {
				continue
			}
			hints[item.TypeID] = item.TypeID1
		}

		if len(hints) == beforeCount {
			stablePages++
		} else {
			stablePages = 0
		}

		if page >= categoryHintMinPages && len(hints) >= targetHintCount && stablePages >= categoryHintStablePageLimit {
			break
		}
		if totalPages > 0 && page >= totalPages {
			break
		}
	}

	if len(hints) == 0 {
		return nil
	}
	return hints
}

func resolveCategoryHintTarget(classCount int) int {
	if classCount <= 1 {
		return classCount
	}
	target := int(float64(classCount) * categoryHintTargetCoverage)
	if target < categoryHintMinPages {
		return categoryHintMinPages
	}
	if target >= classCount {
		return classCount - 1
	}
	return target
}

func needsCategoryParentInference(classes []model.FilmClass) bool {
	if len(classes) == 0 {
		return false
	}
	for _, item := range classes {
		if item.Pid > 0 {
			return false
		}
	}
	return true
}

func (jc *JsonCollect) fetchFilmDetailsPage(r utils.RequestInfo, page int) ([]model.FilmDetail, int, error) {
	params := cloneURLValues(r.Params)
	params.Set("ac", "detail")
	params.Set("pg", strconv.Itoa(page))

	request := utils.RequestInfo{Uri: r.Uri, Params: params, Header: r.Header}
	utils.ApiGet(&request)
	if len(request.Resp) == 0 {
		if request.Err == "" {
			request.Err = "response is empty"
		}
		return nil, 0, errors.New(request.Err)
	}

	pageData := model.FilmDetailLPage{}
	if err := json.Unmarshal(request.Resp, &pageData); err != nil {
		return nil, 0, err
	}
	return pageData.List, pageData.PageCount, nil
}

func cloneURLValues(src url.Values) url.Values {
	cloned := url.Values{}
	for key, values := range src {
		copied := make([]string, len(values))
		copy(copied, values)
		cloned[key] = copied
	}
	return cloned
}

// GetPageCount 获取分页总页数
func (jc *JsonCollect) GetPageCount(r utils.RequestInfo) (count int, err error) {
	// 发送请求获取pageCount, 默认为获取 ac = detail
	if len(r.Params.Get("ac")) <= 0 {
		r.Params.Set("ac", "detail")
	}
	r.Params.Set("pg", "1")
	utils.ApiGet(&r)
	//  判断请求结果是否为空, 如果为空直接输出错误并终止
	if len(r.Resp) <= 0 {
		err = errors.New("response is empty")
		return
	}
	// 获取pageCount
	res := model.CommonPage{}
	err = json.Unmarshal(r.Resp, &res)
	if err != nil {
		return
	}
	count = int(res.PageCount)
	return
}

// GetFilmDetail 通过 RequestInfo 获取并解析出对应的 MovieDetail list
func (jc *JsonCollect) GetFilmDetail(r utils.RequestInfo) (list []model.MovieDetail, err error) {
	// 防止json解析异常引发panic
	defer func() {
		if e := recover(); e != nil {
			log.Println("GetMovieDetail Failed : ", e)
		}
	}()
	// 设置分页请求参数
	r.Params.Set(`ac`, `detail`)
	utils.ApiGet(&r)
	// 影视详情信息
	detailPage := model.FilmDetailLPage{}
	// details := repository.DetailListInfo{}
	// 如果返回数据为空则直接结束本次循环
	if len(r.Resp) <= 0 {
		err = errors.New(r.Err)
		return
	}
	// 序列化详情数据
	if err = json.Unmarshal(r.Resp, &detailPage); err != nil {
		return
	}

	// 处理details信息
	list = conver.ConvertFilmDetails(detailPage.List)
	return
}
