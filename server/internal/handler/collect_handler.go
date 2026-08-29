package handler

import (
	"fmt"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/spider"

	"github.com/gin-gonic/gin"
)

type CollectHandler struct{}

var CollectHd = new(CollectHandler)

func (h *CollectHandler) FilmSourceList(c *gin.Context) {
	dto.Success(service.CollectSvc.GetFilmSourceList(), "影视源站点信息获取成功", c)
}

func (h *CollectHandler) FindFilmSource(c *gin.Context) {
	id := c.Query("id")
	if id == "" {
		dto.Failed("参数异常, 资源站标识不能为空", c)
		return
	}
	fs := service.CollectSvc.GetFilmSource(id)
	if fs == nil {
		dto.Failed("数据异常,资源站信息不存在", c)
		return
	}
	dto.Success(fs, "原站点详情信息查找成功", c)
}

func (h *CollectHandler) FilmSourceAdd(c *gin.Context) {
	s := model.FilmSource{}
	if err := c.ShouldBindJSON(&s); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if err := validFilmSource(s); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	if err := spider.CollectApiTest(s); err != nil {
		dto.Failed(fmt.Sprint("资源接口测试失败: ", err.Error()), c)
		return
	}
	if err := service.CollectSvc.SaveFilmSource(s); err != nil {
		dto.Failed(fmt.Sprint("资源站添加失败: ", err.Error()), c)
		return
	}
	dto.SuccessOnlyMsg("添加成功", c)
}

func (h *CollectHandler) FilmSourceUpdate(c *gin.Context) {
	s := model.FilmSource{}
	if err := c.ShouldBindJSON(&s); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if err := validFilmSource(s); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	if s.Id == "" {
		dto.Failed("参数异常, 资源站标识不能为空", c)
		return
	}
	fs := service.CollectSvc.GetFilmSource(s.Id)
	if fs == nil {
		dto.Failed("数据异常,资源站信息不存在", c)
		return
	}
	if spider.IsTaskRunning(s.Id) {
		dto.Failed("站点正在采集, 请先停止采集后再尝试编辑操作", c)
		return
	}
	if fs.Uri != s.Uri {
		if err := spider.CollectApiTest(s); err != nil {
			dto.Failed(fmt.Sprint("资源接口测试失败: ", err.Error()), c)
			return
		}
	}
	if err := service.CollectSvc.UpdateFilmSource(s); err != nil {
		dto.Failed(fmt.Sprint("资源站更新失败: ", err.Error()), c)
		return
	}
	dto.SuccessOnlyMsg("更新成功", c)
}

func (h *CollectHandler) FilmSourceChange(c *gin.Context) {
	s := model.FilmSource{}
	if err := c.ShouldBindJSON(&s); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if s.Id == "" {
		dto.Failed("参数异常, 资源站标识不能为空", c)
		return
	}
	fs := service.CollectSvc.GetFilmSource(s.Id)
	if fs == nil {
		dto.Failed("数据异常,资源站信息不存在", c)
		return
	}
	if s.State != fs.State {
		upds := model.FilmSource{
			Id:       fs.Id,
			Name:     fs.Name,
			Uri:      fs.Uri,
			Grade:    fs.Grade,
			State:    s.State,
			Interval: fs.Interval,
			Cd:       fs.Cd,
		}
		if err := service.CollectSvc.UpdateFilmSource(upds); err != nil {
			dto.Failed(fmt.Sprint("资源站更新失败: ", err.Error()), c)
			return
		}
	}
	dto.SuccessOnlyMsg("更新成功", c)
}

func (h *CollectHandler) FilmSourceBatchChange(c *gin.Context) {
	req := model.FilmSourceStateBatchRequest{}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	ids := make([]string, 0, len(req.Ids))
	seen := make(map[string]struct{}, len(req.Ids))
	for _, id := range req.Ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		dto.Failed("请选择需要更新的采集站", c)
		return
	}
	if err := service.CollectSvc.BatchUpdateFilmSourceState(ids, req.State); err != nil {
		dto.Failed(fmt.Sprint("资源站批量更新失败: ", err.Error()), c)
		return
	}
	dto.SuccessOnlyMsg("批量更新成功", c)
}

func (h *CollectHandler) FilmSourceDel(c *gin.Context) {
	var req struct {
		Id string `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	id := strings.TrimSpace(req.Id)
	if len(id) <= 0 {
		dto.Failed("资源站ID信息不能为空", c)
		return
	}
	if spider.IsTaskRunning(id) {
		dto.Failed("站点正在采集, 请先停止采集后再尝试删除操作", c)
		return
	}
	if err := service.CollectSvc.DelFilmSource(id); err != nil {
		dto.Failed("删除资源站失败", c)
		return
	}
	dto.SuccessOnlyMsg("删除成功", c)
}

// ------------------------------------------------------ 失效源检测与清理 ------------------------------------------------------

// FilmSourceCheckAll 批量对每个采集站执行接口测试，返回无法正常采集的站点列表。
// 正在采集中的站点与主站不参与测试，会以 skipped 形式返回并附原因。
func (h *CollectHandler) FilmSourceCheckAll(c *gin.Context) {
	sources := service.CollectSvc.GetFilmSourceList()

	const concurrency = 10
	sem := make(chan struct{}, concurrency)
	var mu sync.Mutex
	failed := make([]model.SourceHealthItem, 0)
	skipped := make([]model.SourceHealthItem, 0)
	okCount := 0

	var wg sync.WaitGroup
	for _, src := range sources {
		src := src
		wg.Add(1)
		go func() {
			defer wg.Done()
			sem <- struct{}{}
			defer func() { <-sem }()

			item := model.SourceHealthItem{
				Id:    src.Id,
				Name:  src.Name,
				Uri:   src.Uri,
				Grade: src.Grade,
				State: src.State,
			}
			if spider.IsTaskRunning(src.Id) {
				item.Reason = "站点正在采集，已跳过检测"
				mu.Lock()
				skipped = append(skipped, item)
				mu.Unlock()
				return
			}
			if src.Grade == model.MasterCollect {
				item.Reason = "主站无法直接删除，请先降级为附属站"
				mu.Lock()
				skipped = append(skipped, item)
				mu.Unlock()
				return
			}
			if err := spider.CollectApiTest(src.FilmSource); err != nil {
				item.Reason = err.Error()
				mu.Lock()
				failed = append(failed, item)
				mu.Unlock()
				return
			}
			mu.Lock()
			okCount++
			mu.Unlock()
		}()
	}
	wg.Wait()

	dto.Success(gin.H{
		"checked": len(sources),
		"ok":      okCount,
		"failed":  failed,
		"skipped": skipped,
	}, "站点连通性检测完成", c)
}

// FilmSourceDelBatch 批量删除采集站，返回实际删除与跳过（含原因）的站点。
func (h *CollectHandler) FilmSourceDelBatch(c *gin.Context) {
	req := model.FilmSourceDelBatchRequest{}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	ids := make([]string, 0, len(req.Ids))
	seen := make(map[string]struct{}, len(req.Ids))
	for _, id := range req.Ids {
		id = strings.TrimSpace(id)
		if id == "" {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		ids = append(ids, id)
	}
	if len(ids) == 0 {
		dto.Failed("请选择需要删除的采集站", c)
		return
	}

	type deleteResult struct {
		Id     string `json:"id"`
		Name   string `json:"name"`
		Reason string `json:"reason"`
	}
	deleted := make([]string, 0, len(ids))
	skipped := make([]deleteResult, 0)

	for _, id := range ids {
		src := service.CollectSvc.GetFilmSource(id)
		if src == nil {
			skipped = append(skipped, deleteResult{Id: id, Reason: "采集站不存在，可能已被删除"})
			continue
		}
		if spider.IsTaskRunning(id) {
			skipped = append(skipped, deleteResult{Id: id, Name: src.Name, Reason: "站点正在采集，请先停止后再删除"})
			continue
		}
		if err := service.CollectSvc.DelFilmSource(id); err != nil {
			skipped = append(skipped, deleteResult{Id: id, Name: src.Name, Reason: err.Error()})
			continue
		}
		deleted = append(deleted, id)
	}

	dto.Success(gin.H{"deleted": deleted, "skipped": skipped}, "批量删除完成", c)
}

func (h *CollectHandler) FilmSourceTest(c *gin.Context) {
	s := model.FilmSource{}
	if err := c.ShouldBindJSON(&s); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if err := validFilmSource(s); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	if err := spider.CollectApiTest(s); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("测试成功!!!", c)
}

func (h *CollectHandler) GetNormalFilmSource(c *gin.Context) {
	var l []model.FilmTaskOptions
	for _, v := range service.CollectSvc.GetEnabledFilmSources() {
		l = append(l, model.FilmTaskOptions{Id: v.Id, Name: v.Name})
	}
	dto.Success(l, "影视源信息获取成功", c)
}

// ------------------------------------------------------ 失败采集记录 ------------------------------------------------------

func (h *CollectHandler) FailureRecordList(c *gin.Context) {
	params := model.RecordRequestVo{Paging: &dto.Page{}}
	params.OriginId = c.DefaultQuery("originId", "")
	params.Hour, _ = strconv.Atoi(c.DefaultQuery("hour", "0"))
	params.Status, _ = strconv.Atoi(c.DefaultQuery("status", "-1"))

	begin := c.DefaultQuery("beginTime", "")
	if begin != "" {
		beginTime, e := time.ParseInLocation(time.DateTime, begin, time.Local)
		if e != nil {
			dto.Failed("影片分页数据获取失败, 请求参数异常", c)
			return
		}
		params.BeginTime = beginTime
	}
	end := c.DefaultQuery("endTime", "")
	if end != "" {
		endTime, e := time.ParseInLocation(time.DateTime, end, time.Local)
		if e != nil {
			dto.Failed("影片分页数据获取失败, 请求参数异常", c)
			return
		}
		params.EndTime = endTime
	}

	params.Paging = dto.GetPageParams(c)

	options := service.CollectSvc.GetRecordOptions()
	list := service.CollectSvc.GetRecordList(params)
	dto.Success(gin.H{"params": params, "list": list, "options": options}, "影片分页信息获取成功", c)
}

func (h *CollectHandler) CollectRecover(c *gin.Context) {
	var req struct {
		Id int `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	id := req.Id
	if id <= 0 {
		dto.Failed("采集重试开启失败, 采集记录ID参数异常", c)
		return
	}
	err := service.CollectSvc.CollectRecover(id)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("采集重试已开启, 请勿重复操作", c)
}

func (h *CollectHandler) CollectRecoverAll(c *gin.Context) {
	service.CollectSvc.RecoverAll()
	dto.SuccessOnlyMsg("恢复任务已成功开启!!!", c)
}

func (h *CollectHandler) ClearRetriedRecords(c *gin.Context) {
	service.CollectSvc.ClearRetriedRecords()
	dto.SuccessOnlyMsg("已有重试结果的记录已删除", c)
}

func (h *CollectHandler) ClearAllRecord(c *gin.Context) {
	service.CollectSvc.ClearAllRecord()
	dto.SuccessOnlyMsg("采集异常记录信息已清空!!!", c)
}
