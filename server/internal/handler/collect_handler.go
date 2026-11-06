package handler

import (
	"context"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/spider"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

type CollectHandler struct{}

var CollectHd = new(CollectHandler)

func (h *CollectHandler) FilmSourceList(c *gin.Context) {
	start := time.Now()
	log.Printf("[Collect][List] 进入")
	list, err := service.CollectSvc.GetFilmSourceList()
	cost := time.Since(start).Round(time.Millisecond)
	if err != nil {
		log.Printf("[Collect][List] 失败 cost=%s err=%v", cost, err)
		dto.Failed(err.Error(), c)
		return
	}
	log.Printf("[Collect][List] 完成 count=%d cost=%s", len(list), cost)
	dto.Success(list, "影视源站点信息获取成功", c)
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

// FilmSourceSave 保存采集源（支持新增或编辑更新）
func (h *CollectHandler) FilmSourceSave(c *gin.Context) {
	body, err := bindFilmSourceBody(c)
	if err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	s := body.FilmSource
	if err := validFilmSource(s); err != nil {
		dto.Failed(err.Error(), c)
		return
	}

	// 若传了非空 Id，判断是否为已有站点更新
	if strings.TrimSpace(s.Id) != "" {
		fs := service.CollectSvc.GetFilmSource(s.Id)
		if fs != nil {
			if !fs.CreatedAt.IsZero() && s.CreatedAt.IsZero() {
				s.CreatedAt = fs.CreatedAt
			}
			if spider.IsTaskRunning(s.Id) {
				dto.Failed("站点正在采集, 请先停止采集后再尝试编辑操作", c)
				return
			}
			isUriChanged := fs.Uri != s.Uri
			isFormatChanged := fs.ResolveFormat() != s.ResolveFormat()
			if err := service.CollectSvc.UpdateFilmSource(s); err != nil {
				dto.Failed(fmt.Sprint("资源站更新失败: ", err.Error()), c)
				return
			}
			if isUriChanged || isFormatChanged {
				service.SpiderSvc.SyncSourceCategoriesAsync(s.Id)
			}
			dto.Success(gin.H{"id": s.Id}, "更新成功", c)
			return
		}
	}

	// 否则作为新增处理
	if err := service.CollectSvc.SaveFilmSource(s); err != nil {
		dto.Failed(fmt.Sprint("资源站添加失败: ", err.Error()), c)
		return
	}
	id := s.Id
	if id == "" {
		id = utils.GenerateHashKey(s.Uri)
	}
	service.SpiderSvc.SyncSourceCategoriesAsync(id)
	dto.Success(gin.H{"id": id}, "添加成功", c)
}

func (h *CollectHandler) FilmSourceSort(c *gin.Context) {
	var req struct {
		Ids []string `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if err := service.CollectSvc.SortFilmSources(req.Ids); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("排序保存成功", c)
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
	dto.SuccessOnlyMsg("更新成功", c)
}

// ------------------------------------------------------ 失效源检测与清理 ------------------------------------------------------

// FilmSourceCheckAll 批量对每个采集站执行接口测试，返回无法正常采集的站点列表。
// 正在采集中的站点与主站不参与测试，会以 skipped 形式返回并附原因。
func (h *CollectHandler) FilmSourceCheckAll(c *gin.Context) {
	sources, err := service.CollectSvc.GetFilmSourceList()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}

	const concurrency = 10
	ctx, cancel := context.WithTimeout(c.Request.Context(), 30*time.Second)
	defer cancel()
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
				Sort:  src.Sort,
				State: src.State,
			}
			if spider.IsTaskRunning(src.Id) {
				item.Reason = "站点正在采集，已跳过检测"
				mu.Lock()
				skipped = append(skipped, item)
				mu.Unlock()
				return
			}
			if err := spider.CollectApiTestCtx(ctx, src.FilmSource); err != nil {
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
		"checked":  len(sources),
		"ok":       okCount,
		"failed":   failed,
		"skipped":  skipped,
		"timedOut": ctx.Err() != nil,
	}, "站点连通性检测完成", c)
}

// FilmSourceDelBatch 批量删除采集站：触发即返回，通过 SSE 广播 deleting 与 deleted 状态
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

	if err := service.CollectSvc.TriggerDelFilmSourceBatch(ids); err != nil {
		dto.Failed(err.Error(), c)
		return
	}

	dto.SuccessOnlyMsg("已提交批量删除", c)
}

func (h *CollectHandler) FilmSourceTest(c *gin.Context) {
	var body struct {
		model.FilmSource
		UseProxy *bool `json:"useProxy"`
	}
	if err := c.ShouldBindJSON(&body); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if err := validFilmSource(body.FilmSource); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	var err error
	if body.UseProxy != nil {
		err = spider.CollectApiTestChoosingProxy(body.FilmSource, *body.UseProxy)
	} else {
		err = spider.CollectApiTest(body.FilmSource)
	}
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("测试成功!!!", c)
}

func (h *CollectHandler) RefreshSitePageTotal(c *gin.Context) {
	var req struct {
		Id string `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	req.Id = strings.TrimSpace(req.Id)
	if req.Id == "" {
		dto.Failed("采集站标识不能为空", c)
		return
	}
	total, err := service.CollectSvc.RefreshSitePageTotal(req.Id)
	if err != nil {
		dto.Failed(fmt.Sprint("获取采集站总页失败: ", err.Error()), c)
		return
	}
	dto.Success(gin.H{"pageTotal": total}, fmt.Sprintf("最新总页：%d 页", total), c)
}

func (h *CollectHandler) GetNormalFilmSource(c *gin.Context) {
	includeDisabled := c.Query("all") == "1"
	var sources []model.FilmSource
	var err error
	if includeDisabled {
		sources, err = service.CollectSvc.GetAllFilmSources()
	} else {
		sources, err = service.CollectSvc.GetEnabledFilmSources()
	}
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	l := make([]model.FilmTaskOptions, 0, len(sources))
	for _, v := range sources {
		item := model.FilmTaskOptions{Id: v.Id, Name: v.Name}
		if includeDisabled {
			state := v.State
			item.State = &state
		}
		l = append(l, item)
	}
	dto.Success(l, "影视源信息获取成功", c)
}

type filmSourceBody struct {
	model.FilmSource
	UseProxy *bool `json:"useProxy"`
}

func bindFilmSourceBody(c *gin.Context) (filmSourceBody, error) {
	var body filmSourceBody
	err := c.ShouldBindJSON(&body)
	return body, err
}

func testFilmSourceAPI(s model.FilmSource, useProxy *bool) error {
	if useProxy != nil {
		return spider.CollectApiTestChoosingProxy(s, *useProxy)
	}
	return spider.CollectApiTest(s)
}


