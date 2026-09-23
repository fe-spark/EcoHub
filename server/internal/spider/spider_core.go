package spider

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"server/internal/model"
	"server/internal/spider/converter"
	"server/internal/utils"
)

/*
	Spider 数据 爬取 & 处理 & 转换
*/

// Collector 采集引擎统一接口，支持 JSON 与 XML 等多协议接入。
type Collector interface {
	GetCategoryTree(r utils.RequestInfo) (*model.CategoryTree, error)
	GetPageCount(r utils.RequestInfo) (int, error)
	GetFilmDetail(r utils.RequestInfo) ([]model.MovieDetail, error)
}

var (
	jsonCollector = &JsonCollect{}
	xmlCollector  = &XmlCollect{}
)

// ResolveCollector 根据源站配置的格式返回对应采集器实例，默认返回 JSON 采集器。
func ResolveCollector(format string) Collector {
	if format == model.SourceFormatXML {
		return xmlCollector
	}
	return jsonCollector
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
		errMsg := r.Err
		if errMsg == "" {
			errMsg = "response is empty"
		}
		log.Printf("filmListPage 数据获取异常: %s", errMsg)
		return nil, fmt.Errorf("filmListPage 数据获取异常: %s", errMsg)
	}
	if err := json.Unmarshal(r.Resp, &filmListPage); err != nil {
		return nil, fmt.Errorf("filmListPage JSON unmarshal error: %w", err)
	}
	// 获取分类列表信息
	cl := filmListPage.Class
	if len(cl) == 0 {
		return &model.CategoryTree{}, nil
	}
	// 组装分类数据信息树形结构
	tree := converter.GenCategoryTreeWithParentHints(cl, nil)

	return tree, nil
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
		errMsg := r.Err
		if errMsg == "" {
			errMsg = "response is empty"
		}
		err = errors.New(errMsg)
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
		errMsg := r.Err
		if errMsg == "" {
			errMsg = "response is empty"
		}
		err = errors.New(errMsg)
		return
	}
	// 序列化详情数据
	if err = json.Unmarshal(r.Resp, &detailPage); err != nil {
		return
	}

	// 处理details信息
	list = converter.ConvertFilmDetails(detailPage.List)
	return
}
