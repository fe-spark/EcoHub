package spider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/spider/conver"
	"server/internal/utils"

	"golang.org/x/time/rate"
)

/*
	采集逻辑 v3

*/

var spiderCore = &JsonCollect{}

const (
	pageCountRetryTimes  = 2
	filmDetailRetryTimes = 2
)

var retryBackoffs = []time.Duration{
	2 * time.Second,
	5 * time.Second,
}

// activeTasks 存储当前活跃采集任务的信息
var activeTasks sync.Map

// taskMu 保护同一站点 cancel+Store 的原子性，防止并发截停竞态
var taskMu sync.Mutex

// limiters 存储各站点的限流器
var limiters sync.Map

func ClearLimiter(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	limiters.Delete(sourceID)
}

type collectTask struct {
	cancel context.CancelFunc
	reqId  string
}

// getLimiter 获取指定站点的限流器，如果不存在则基于站点的 Interval 配置创建一个
// Interval 单位为毫秒，表示两次请求间的最小间隔
func getLimiter(s *model.FilmSource) *rate.Limiter {
	if s == nil {
		return rate.NewLimiter(rate.Every(config.DefaultSpiderInterval*time.Millisecond), 1)
	}
	if val, ok := limiters.Load(s.Id); ok {
		return val.(*rate.Limiter)
	}

	// 优先使用站点配置的 Interval，否则使用全局默认配置
	interval := int64(config.DefaultSpiderInterval)
	if s.Interval > 0 {
		interval = int64(s.Interval)
	}

	r := rate.Every(time.Duration(interval) * time.Millisecond)

	// 允许最多 1 个令牌的突发流量（Burst = 1，即严格控制间隔）
	l := rate.NewLimiter(r, 1)
	limiters.Store(s.Id, l)
	return l
}

func getPageCountWithRetry(ctx context.Context, s *model.FilmSource, r utils.RequestInfo) (int, error) {
	var lastErr error
	for attempt := 1; attempt <= pageCountRetryTimes; attempt++ {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		if err := getLimiter(s).Wait(ctx); err != nil {
			return 0, err
		}
		pageCount, err := spiderCore.GetPageCount(r)
		if err == nil {
			return pageCount, nil
		}
		lastErr = err
		if attempt < pageCountRetryTimes && utils.IsRateLimitedErr(err) {
			if waitErr := waitRetryBackoff(ctx, attempt); waitErr != nil {
				return 0, waitErr
			}
		}
	}
	return 0, lastErr
}

func getFilmDetailWithRetry(ctx context.Context, s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
	var lastErr error
	for attempt := 1; attempt <= filmDetailRetryTimes; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		if err := getLimiter(s).Wait(ctx); err != nil {
			return nil, err
		}
		list, err := spiderCore.GetFilmDetail(r)
		if err == nil && len(list) > 0 {
			return list, nil
		}
		if err != nil {
			lastErr = err
		} else {
			lastErr = errors.New("response list is empty")
		}
		if attempt < filmDetailRetryTimes && utils.IsRateLimitedErr(lastErr) {
			if waitErr := waitRetryBackoff(ctx, attempt); waitErr != nil {
				return nil, waitErr
			}
		}
	}
	return nil, lastErr
}

func waitRetryBackoff(ctx context.Context, attempt int) error {
	if attempt <= 0 {
		attempt = 1
	}
	delayIndex := attempt - 1
	if delayIndex >= len(retryBackoffs) {
		delayIndex = len(retryBackoffs) - 1
	}
	timer := time.NewTimer(retryBackoffs[delayIndex])
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func runSourcesWithLimit(sources []model.FilmSource, h int, tag string) {
	if len(sources) == 0 {
		return
	}
	limit := config.MAXGoroutine
	if limit <= 0 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup

	for _, src := range sources {
		wg.Add(1)
		sem <- struct{}{}
		go func(fs model.FilmSource) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := HandleCollect(fs.Id, h); err != nil {
				log.Printf("[%s] 采集站点 %s 失败: %v", tag, fs.Name, err)
			}
		}(src)
	}
	wg.Wait()
}

// ======================================================= 通用采集方法  =======================================================

// HandleCollect 影视采集  id-采集站ID h-时长/h
func HandleCollect(id string, h int) error {
	// 同站跳过：如果该站点已有采集任务在运行，则跳过此次采集任务
	reqId := utils.GenerateSalt()

	taskMu.Lock()
	if _, ok := activeTasks.Load(id); ok {
		taskMu.Unlock()
		log.Printf("[Spider] 站点 %s 已有任务正在运行，跳过本次采集...\n", id)
		return fmt.Errorf("站点 %s 已有任务正在运行，已跳过本次采集", id)
	}
	ctx, cancel := context.WithCancel(context.Background())
	activeTasks.Store(id, collectTask{cancel: cancel, reqId: reqId})
	taskMu.Unlock()

	// 任务完成后清理（仅当当前任务仍是自己时）
	defer func() {
		if val, ok := activeTasks.Load(id); ok {
			if val.(collectTask).reqId == reqId {
				activeTasks.Delete(id)
				log.Printf("[Spider] 站点 %s 任务结束\n", id)
			}
		}
	}()

	log.Printf("[Spider] 站点 %s 任务启动 (reqId: %s)\n", id, reqId)

	// 1. 首先通过ID获取对应采集站信息
	s := repository.FindCollectSourceById(id)
	if s == nil {
		return errors.New("采集站点不存在")
	} else if !s.State {
		return errors.New("采集站点已停用")
	}

	// 如果是主站点且状态为启用则先获取分类tree信息
	if s.Grade == model.MasterCollect && s.State {
		// 是否存在分类树信息, 不存在则获取
		if !repository.ExistsCategoryTree() {
			CollectCategory(s)
		}
	}

	// 生成 RequestInfo
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	// 如果 h == 0 则直接返回错误信息
	if h == 0 {
		return errors.New("采集时长不能为 0")
	}
	// 如果 h = -1 则进行全量采集
	if h > 0 {
		r.Params.Set("h", fmt.Sprint(h))
	}
	// 2. 首先获取分页采集的页数
	pageCount, err := getPageCountWithRetry(ctx, s, r)
	if err != nil {
		return err
	}
	// pageCount = 0 说明该站点在当前时间段内无新数据，任务无需执行
	if pageCount <= 0 {
		log.Printf("[Spider] 站点 %s 无需采集 (pageCount=%d，可能该时间段内无新内容)\n", s.Name, pageCount)
		return nil
	}
	log.Printf("[Spider] 站点 %s 共 %d 页，开始采集...\n", s.Name, pageCount)

	if pageCount <= config.MAXGoroutine*2 {
		for i := 1; i <= pageCount; i++ {
			select {
			case <-ctx.Done():
				log.Printf("[Spider] 站点 %s 采集任务被中断(同步模式)\n", s.Name)
				return nil
			default:
				collectFilm(ctx, s, h, i)
			}
		}
	} else {
		ConcurrentPageSpider(ctx, pageCount, s, h, collectFilm)
	}

	if s.Grade == model.MasterCollect && s.SyncPictures {
		repository.SyncFilmPicture()
	}
	return nil
}

// CollectCategory 影视分类采集
func CollectCategory(s *model.FilmSource) {
	// 获取分类树形数据
	categoryTree, err := spiderCore.GetCategoryTree(utils.RequestInfo{Uri: s.Uri, Params: url.Values{}})
	if err != nil {
		log.Println("GetCategoryTree Error: ", err)
		return
	}
	// 保存 tree 到 MySQL (方案B: 传入 sourceId 建立映射)
	err = repository.SaveCategoryTree(s.Id, categoryTree)
	if err != nil {
		log.Println("SaveCategoryTree Error: ", err)
		return
	}
}

// saveCollectedFilm 将已采集的 list 按站点类型写入存储，消除 collectFilm/collectFilmById 中的重复 switch 块。
// saveMaster 由调用方注入，区分批量(SaveDetails)与单条(SaveDetail)两种写入策略。
func saveCollectedFilm(s *model.FilmSource, list []model.MovieDetail, saveMaster func(string, []model.MovieDetail) error) error {
	var err error
	switch s.Grade {
	case model.MasterCollect:
		if err = saveMaster(s.Id, list); err != nil {
			return fmt.Errorf("save master details failed: %w", err)
		}
		if s.SyncPictures {
			if err = repository.SaveVirtualPic(conver.ConvertVirtualPicture(list)); err != nil {
				return fmt.Errorf("save virtual pictures failed: %w", err)
			}
		}
	case model.SlaveCollect:
		if err = filmrepo.SaveSitePlayList(s.Id, list); err != nil {
			return fmt.Errorf("save slave playlists failed: %w", err)
		}
	}
	return nil
}

func saveFilmPageFailure(s *model.FilmSource, h, pg int, err error) {
	repository.SaveFailureRecord(model.FailureRecord{
		OriginId:   s.Id,
		OriginName: s.Name,
		Uri:        s.Uri,
		PageNumber: pg,
		Hour:       h,
		Cause:      fmt.Sprintln(err),
		Status:     1,
	})
}

// collectFilm 影视详情采集 (单一源分页全采集)
func collectFilm(ctx context.Context, s *model.FilmSource, h, pg int) {
	select {
	case <-ctx.Done():
		return
	default:
	}
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("pg", fmt.Sprint(pg))
	if h > 0 {
		r.Params.Set("h", fmt.Sprint(h))
	}

	// collectFilm 本身作为并发 Worker 或同步循环的一部分
	// 具体的 Wait 逻辑已由调用方（如 ConcurrentPageSpider 或 HandleCollect 循环）控制，
	// 此处仅执行请求，保证原子请求的纯粹性
	list, err := getFilmDetailWithRetry(ctx, s, r)
	if err != nil || len(list) <= 0 {
		saveFilmPageFailure(s, h, pg, err)
		log.Println("GetMovieDetail Error: ", err)
		return
	}
	if err = saveCollectedFilm(s, list, filmrepo.SaveDetails); err != nil {
		saveFilmPageFailure(s, h, pg, err)
		log.Println("saveCollectedFilm Error: ", err)
	}
}

// collectFilmById 采集指定ID的影片信息
func collectFilmById(ids string, s *model.FilmSource) error {
	if err := getLimiter(s).Wait(context.Background()); err != nil {
		return err
	}
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("pg", "1")
	r.Params.Set("ids", ids)
	list, err := spiderCore.GetFilmDetail(r)
	if err != nil {
		return fmt.Errorf("get movie detail failed: %w", err)
	}
	if len(list) <= 0 {
		return errors.New("get movie detail failed: response list is empty")
	}
	return saveCollectedFilm(s, list, func(id string, l []model.MovieDetail) error {
		return filmrepo.SaveDetail(id, l[0])
	})
}

// ConcurrentPageSpider 并发分页采集, 不限类型
func ConcurrentPageSpider(ctx context.Context, capacity int, s *model.FilmSource, h int, collectFunc func(ctx context.Context, s *model.FilmSource, hour, pageNumber int)) {
	// 开启协程并发执行
	ch := make(chan int, capacity)
	for i := 1; i <= capacity; i++ {
		ch <- i
	}
	close(ch)
	GoroutineNum := min(capacity, config.MAXGoroutine)
	// waitCh 必须带缓冲(容量=GoroutineNum)：ctx 取消时等待循环提前退出，
	// worker 仍会执行 waitCh<-0，无缓冲则永久阻塞导致 goroutine 泄漏
	waitCh := make(chan int, GoroutineNum)
	for i := 0; i < GoroutineNum; i++ {
		go func() {
			defer func() { waitCh <- 0 }()
			for {
				select {
				case <-ctx.Done():
					return
				case pg, ok := <-ch:
					if !ok {
						return
					}
					// 执行对应的采集方法
					collectFunc(ctx, s, h, pg)
				}
			}
		}()
	}
	for i := 0; i < GoroutineNum; i++ {
		select {
		case <-waitCh:
		case <-ctx.Done():
			log.Printf("[Spider] 站点 %s 并发采集任务被中断\n", s.Name)
			return
		}
	}
}

// BatchCollect 批量采集, 采集指定的所有站点最近x小时内更新的数据
func BatchCollect(h int, ids ...string) {
	sources := make([]model.FilmSource, 0)
	for _, id := range ids {
		if fs := repository.FindCollectSourceById(id); fs != nil && fs.State {
			sources = append(sources, *fs)
		}
	}

	if len(sources) == 0 {
		return
	}

	runSourcesWithLimit(sources, h, "Batch-Collect")
}

func getEnabledSourcesByGrade(grade model.SourceGrade) []model.FilmSource {
	sources := repository.GetCollectSourceListByGrade(grade)
	enabled := make([]model.FilmSource, 0, len(sources))
	for _, s := range sources {
		if s.State {
			enabled = append(enabled, s)
		}
	}
	return enabled
}

// AutoCollect 自动进行对所有已启用站点的采集任务
func AutoCollect(h int) {
	sources := repository.GetCollectSourceList()
	enabled := make([]model.FilmSource, 0, len(sources))
	for _, source := range sources {
		if source.State {
			enabled = append(enabled, source)
		}
	}
	if len(enabled) == 0 {
		log.Println("[Spider] 自动采集：未找到任何启用的站点")
		return
	}

	runSourcesWithLimit(enabled, h, "Auto-Collect")
}

// ClearSpider 删除所有已采集的影片信息
func ClearSpider() {
	filmrepo.FilmZero()
}

// CollectSingleFilm 通过全局影片 ID 更新所有启用站点的对应影片。
// 主站会优先使用自身映射，附属站则通过 MovieSourceMapping 取回各自的 source_mid。
func CollectSingleFilm(ids string) {
	globalMid, err := strconv.ParseInt(strings.TrimSpace(ids), 10, 64)
	if err != nil || globalMid <= 0 {
		log.Printf("[Spider] CollectSingleFilm: 非法影片 ID %q\n", ids)
		return
	}

	all := repository.GetCollectSourceList()
	enabled := make([]model.FilmSource, 0, len(all))
	for _, source := range all {
		if source.State {
			enabled = append(enabled, source)
		}
	}
	if len(enabled) == 0 {
		log.Println("[Spider] CollectSingleFilm: 未找到任何启用的站点")
		return
	}

	var wg sync.WaitGroup
	for _, source := range enabled {
		requestID := resolveSingleCollectSourceMid(globalMid, source)
		if requestID == "" {
			continue
		}

		wg.Add(1)
		go func(src model.FilmSource, sourceMid string) {
			defer wg.Done()
			if err := collectFilmById(sourceMid, &src); err != nil {
				log.Printf("[Spider] CollectSingleFilm 站点 %s 更新失败: %v", src.Name, err)
			}
		}(source, requestID)
	}
	wg.Wait()
}

func resolveSingleCollectSourceMid(globalMid int64, source model.FilmSource) string {
	if globalMid <= 0 {
		return ""
	}
	sourceMid := filmrepo.LoadSourceMidByGlobalMid(globalMid, source.Id)
	if sourceMid > 0 {
		return strconv.FormatInt(sourceMid, 10)
	}
	if source.Grade == model.MasterCollect {
		return strconv.FormatInt(globalMid, 10)
	}
	return ""
}

// recoverFilmPage 重试单条失败页：成功后标记原记录已处理，失败则更新待处理记录
func recoverFilmPage(ctx context.Context, s *model.FilmSource, fr *model.FailureRecord) {
	if s == nil || fr == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("pg", fmt.Sprint(fr.PageNumber))
	if fr.Hour > 0 {
		r.Params.Set("h", fmt.Sprint(fr.Hour))
	}

	list, err := getFilmDetailWithRetry(ctx, s, r)
	if err != nil || len(list) <= 0 {
		saveFilmPageFailure(s, fr.Hour, fr.PageNumber, err)
		log.Println("Recover GetMovieDetail Error: ", err)
		return
	}

	if err = saveCollectedFilm(s, list, filmrepo.SaveDetails); err != nil {
		saveFilmPageFailure(s, fr.Hour, fr.PageNumber, err)
		log.Println("Recover saveCollectedFilm Error: ", err)
		return
	}
	repository.ChangeRecord(fr, 0)
}

// ======================================================= 采集拓展内容  =======================================================

// SingleRecoverSpider 二次采集
func SingleRecoverSpider(fr *model.FailureRecord) {
	// 仅对当前失败记录所属站点+失败页进行重试，不干扰正在运行的采集任务
	s := repository.FindCollectSourceById(fr.OriginId)
	if s == nil {
		log.Printf("[Spider] 重试失败: 站点 %s 不存在\n", fr.OriginId)
		return
	}
	recoverFilmPage(context.Background(), s, fr)
}

// FullRecoverSpider 扫描记录表中的失败记录, 并发重试各失败页
func FullRecoverSpider() {
	list := repository.PendingRecord()
	limit := config.MAXGoroutine
	if limit <= 0 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := range list {
		fr := list[i]
		s := repository.FindCollectSourceById(fr.OriginId)
		if s == nil {
			log.Printf("[Spider] 重试失败: 站点 %s 不存在\n", fr.OriginId)
			continue
		}
		wg.Add(1)
		sem <- struct{}{}
		go func(src *model.FilmSource, record model.FailureRecord) {
			defer wg.Done()
			defer func() { <-sem }()
			recoverFilmPage(context.Background(), src, &record)
		}(s, fr)
	}
	wg.Wait()
}

// ======================================================= 公共方法  =======================================================

// CollectApiTest 测试采集接口是否可用
func CollectApiTest(s model.FilmSource) error {
	// 使用 ac=list 测试：获取分类列表，所有标准 Mac CMS 站均支持，
	// 且不需要额外过滤参数（ac=detail 在无 h/t 参数时部分站点会返回 400）
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("ac", "list")
	r.Params.Set("pg", "1")
	err := utils.ApiTest(&r)
	// 首先核对接口返回值类型
	if err == nil {
		lp := model.FilmListPage{}
		if err = json.Unmarshal(r.Resp, &lp); err != nil {
			return errors.New(fmt.Sprint("测试失败, 返回数据异常, JSON序列化失败: ", err))
		}
		return nil
	}
	return errors.New(fmt.Sprint("测试失败, 请求响应异常 : ", err.Error()))
}

// GetActiveTasks 返回当前正在采集的任务 ID 列表
func GetActiveTasks() []string {
	ids := make([]string, 0)
	activeTasks.Range(func(key, value any) bool {
		ids = append(ids, key.(string))
		return true
	})
	return ids
}

// StopAllTasks 强制停止当前系统中所有正在进行的采集任务
func StopAllTasks() {
	count := 0
	activeTasks.Range(func(key, value any) bool {
		if ct, ok := value.(collectTask); ok {
			ct.cancel()
			count++
		}
		activeTasks.Delete(key)
		return true
	})
	if count > 0 {
		log.Printf("[Spider] 已强制停止 %d 个活跃采集任务\n", count)
	}
}

// StopTask 强行停止指定站点的采集任务
func StopTask(id string) {
	if val, ok := activeTasks.Load(id); ok {
		val.(collectTask).cancel()
		activeTasks.Delete(id)
	}
}

// IsTaskRunning 查询指定站点的采集任务是否正在运行
func IsTaskRunning(id string) bool {
	_, ok := activeTasks.Load(id)
	return ok
}

// IsAnyTaskRunning 查询系统中是否有任何采集任务正在进行
func IsAnyTaskRunning() bool {
	running := false
	activeTasks.Range(func(key, value any) bool {
		running = true
		return false // 找到一个就退出循环
	})
	return running
}
