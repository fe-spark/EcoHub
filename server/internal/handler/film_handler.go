package handler

import (
	"fmt"
	"strconv"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"

	"github.com/gin-gonic/gin"
)

type FilmHandler struct{}

var FilmHd = new(FilmHandler)

// FilmSearchPage 获取影视分页数据
func (h *FilmHandler) FilmSearchPage(c *gin.Context) {
	var s = model.SearchVo{Paging: &dto.Page{}}
	var err error

	s.SourceId = strings.TrimSpace(c.DefaultQuery("sourceId", ""))
	s.Name = c.DefaultQuery("name", "")
	s.Pid, err = strconv.ParseInt(c.DefaultQuery("pid", "0"), 10, 64)
	if err != nil {
		dto.Failed("影片分页数据获取失败, 请求参数异常", c)
		return
	}
	s.Cid, err = strconv.ParseInt(c.DefaultQuery("cid", "0"), 10, 64)
	if err != nil {
		dto.Failed("影片分页数据获取失败, 请求参数异常", c)
		return
	}
	s.Plot = c.DefaultQuery("plot", "")
	s.Area = c.DefaultQuery("area", "")
	s.Language = c.DefaultQuery("language", "")
	year := c.DefaultQuery("year", "")
	if year == "" {
		s.Year = 0
	} else {
		s.Year, err = strconv.ParseInt(year, 10, 64)
		if err != nil {
			dto.Failed("影片分页数据获取失败, 请求参数异常", c)
			return
		}
	}

	begin := c.DefaultQuery("beginTime", "")
	if begin == "" {
		s.BeginTime = 0
	} else {
		beginTime, e := time.ParseInLocation(time.DateTime, begin, time.Local)
		if e != nil {
			dto.Failed("影片分页数据获取失败, 请求参数异常", c)
			return
		}
		s.BeginTime = beginTime.Unix()
	}
	end := c.DefaultQuery("endTime", "")
	if end == "" {
		s.EndTime = 0
	} else {
		endTime, e := time.ParseInLocation(time.DateTime, end, time.Local)
		if e != nil {
			dto.Failed("影片分页数据获取失败, 请求参数异常", c)
			return
		}
		s.EndTime = endTime.Unix()
	}

	s.Paging = dto.GetPageParams(c)
	sl, err := service.FilmSvc.GetFilmPage(&s)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(gin.H{
		"params": s,
		"list":   sl,
	}, "影片分页信息获取成功", c)
}

// FilmSearchOptions 获取影片检索筛选项（按 sourceId 联动返回对应站点的分类树与配置项）
func (h *FilmHandler) FilmSearchOptions(c *gin.Context) {
	sourceId := strings.TrimSpace(c.DefaultQuery("sourceId", ""))
	options, err := service.FilmSvc.GetSearchOptions(sourceId)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(options, "影片检索筛选项获取成功", c)
}

// FilmSave 保存影片信息（支持手动添加或编辑更新）
func (h *FilmHandler) FilmSave(c *gin.Context) {
	var fd = model.FilmDetailVo{}
	if err := c.ShouldBindJSON(&fd); err != nil {
		dto.Failed("影片保存失败, 影片参数提交异常", c)
		return
	}

	if err := service.FilmSvc.SaveFilmDetail(fd); err != nil {
		dto.Failed(fmt.Sprint("影片保存失败, 影片信息保存错误: ", err.Error()), c)
		return
	}
	dto.SuccessOnlyMsg("影片信息已保存", c)
}

// FilmDelete 删除影片检索信息（兼容单个与批量）
func (h *FilmHandler) FilmDelete(c *gin.Context) {
	var req struct {
		Ids []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数格式异常", c)
		return
	}
	if len(req.Ids) == 0 {
		dto.Failed("缺少影片 ID", c)
		return
	}

	if err := service.FilmSvc.DelFilms(req.Ids); err != nil {
		dto.Failed("删除失败: "+err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("影片删除成功", c)
}

// ----------------------------------------------------影片分类处理----------------------------------------------------

// FilmClassTree 影片分类树数据。scoped=1 时只返回指定采集站的分类，并带上站点选项。
func (h *FilmHandler) FilmClassTree(c *gin.Context) {
	if c.Query("scoped") != "1" {
		tree := service.FilmSvc.GetFilmClassTree()
		dto.Success(tree, "影片分类信息获取成功", c)
		return
	}
	view, err := service.FilmSvc.GetScopedClassTree(c.Query("sourceId"))
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	sources := make([]gin.H, 0, len(view.Sources))
	for _, source := range view.Sources {
		sources = append(sources, gin.H{"id": source.Id, "name": source.Name, "state": source.State})
	}
	dto.Success(gin.H{
		"id":              view.Tree.Id,
		"pid":             view.Tree.Pid,
		"name":            view.Tree.Name,
		"show":            view.Tree.Show,
		"children":        view.Tree.Children,
		"sources":         sources,
		"defaultSourceId": view.DefaultSourceId,
		"currentSourceId": view.CurrentSourceId,
	}, "影片分类信息获取成功", c)
}

// FilmRefresh 批量重新采集指定影片
func (h *FilmHandler) FilmRefresh(c *gin.Context) {
	var req struct {
		Ids []int64 `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数格式异常", c)
		return
	}
	cleanIds := make([]int64, 0, len(req.Ids))
	for _, id := range req.Ids {
		if id > 0 {
			cleanIds = append(cleanIds, id)
		}
	}
	if len(cleanIds) == 0 {
		dto.Failed("缺少影片 ID", c)
		return
	}
	service.SpiderSvc.CollectFilms(cleanIds)
	dto.SuccessOnlyMsg("更新任务已开启", c)
}

func (h *FilmHandler) SaveFilmClassTree(c *gin.Context) {
	var req struct {
		Children []*model.CategoryTree `json:"children"`
		SourceId string                `json:"sourceId"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("保存失败, 请求参数异常", c)
		return
	}
	if len(req.Children) == 0 {
		dto.Failed("保存失败, 分类结构不能为空", c)
		return
	}
	var err error
	if strings.TrimSpace(req.SourceId) != "" {
		err = service.FilmSvc.SaveSourceClassTree(req.SourceId, req.Children)
	} else {
		err = service.FilmSvc.SaveClassTree(req.Children)
	}
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("分类变更已保存", c)
}
