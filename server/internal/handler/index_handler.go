package handler

import (
	"encoding/json"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"

	"github.com/gin-gonic/gin"
)

type IndexHandler struct{}

var IndexHd = new(IndexHandler)

func Health(c *gin.Context) {
	dto.Success(gin.H{"status": "ok"}, "服务正常", c)
}

func hasSearchOptions(searchTags map[string]any) bool {
	tags, ok := searchTags["tags"].(map[string]any)
	if !ok {
		return false
	}
	for key, value := range tags {
		if key == "Sort" {
			continue
		}
		if hasRealSearchTagList(value) {
			return true
		}
	}
	return false
}

func hasRealSearchTagList(value any) bool {
	list, ok := value.([]map[string]string)
	if ok {
		for _, item := range list {
			if strings.TrimSpace(item["Value"]) != "" {
				return true
			}
		}
		return false
	}

	rawList, ok := value.([]any)
	if !ok {
		return false
	}
	for _, raw := range rawList {
		item, ok := raw.(map[string]any)
		if !ok {
			continue
		}
		if value, ok := item["Value"].(string); ok && strings.TrimSpace(value) != "" {
			return true
		}
	}
	return false
}

func resolvePlayableSourceID(playSources []model.PlayLinkVo, preferred string) string {
	if preferred != "" {
		for _, source := range playSources {
			if source.Id == preferred && len(source.LinkList) > 0 {
				return source.Id
			}
		}

		for _, source := range playSources {
			if source.SourceId == preferred && len(source.LinkList) > 0 {
				return source.Id
			}
		}
	}

	for _, source := range playSources {
		if len(source.LinkList) > 0 {
			return source.Id
		}
	}

	if len(playSources) > 0 {
		return playSources[0].Id
	}

	return ""
}

func logSlowIndexStep(name string, startedAt time.Time, fields ...any) {
	cost := time.Since(startedAt)
	if cost < 500*time.Millisecond {
		return
	}
	args := append([]any{"[IndexHandler][Slow]", name, "cost", cost}, fields...)
	log.Println(args...)
}

// Index 首页数据
func (h *IndexHandler) Index(c *gin.Context) {
	data := service.IndexSvc.IndexPage()
	dto.Success(data, "首页数据获取成功", c)
}

// DailyUpdates 近 24h 更新（保持 beta.3 原始接口契约：不传 limit 返回全部；传 limit 则随机抽取，exclude 排除当前批次）。
func (h *IndexHandler) DailyUpdates(c *gin.Context) {
	data := service.IndexSvc.HomeDailyUpdates(parseDailyUpdateLimit(c.Query("limit")), parseDailyUpdateExclude(c.Query("exclude")))
	if data == nil {
		data = make([]model.MovieBasicInfo, 0)
	}
	dto.Success(data, "每日更新获取成功", c)
}

// DailyUpdatesV2 新每日更新接口（全新独立接口，无需兼容老接口）：
// 1. 支持流式传输（stream=1 或 stream=true，使用 NDJSON/chunked 逐步输出）
// 2. 支持标准分页（page/pageSize 或 current/size）
// 3. 支持随机轮换（random=1 或 random=true，搭配 exclude 参数）
func (h *IndexHandler) DailyUpdatesV2(c *gin.Context) {
	stream := c.Query("stream") == "true" || c.Query("stream") == "1"
	if stream {
		batchSize, _ := strconv.Atoi(c.Query("batchSize"))
		if batchSize <= 0 {
			batchSize, _ = strconv.Atoi(c.Query("pageSize"))
		}
		if batchSize <= 0 {
			batchSize, _ = strconv.Atoi(c.Query("size"))
		}
		if batchSize <= 0 {
			batchSize = 50
		}

		c.Header("Content-Type", "application/x-ndjson; charset=utf-8")
		c.Header("Transfer-Encoding", "chunked")
		c.Header("Cache-Control", "no-cache")
		c.Header("Connection", "keep-alive")

		w := c.Writer
		flusher, ok := w.(http.Flusher)

		err := service.IndexSvc.StreamDailyUpdates(c.Request.Context(), batchSize, func(batch []model.MovieBasicInfo, currentBatch int, totalCount int) error {
			chunk := gin.H{
				"batch": currentBatch,
				"total": totalCount,
				"count": len(batch),
				"list":  batch,
			}
			raw, marshalErr := json.Marshal(chunk)
			if marshalErr != nil {
				return marshalErr
			}
			if _, writeErr := w.Write(append(raw, '\n')); writeErr != nil {
				return writeErr
			}
			if ok {
				flusher.Flush()
			}
			return nil
		})
		if err != nil {
			log.Printf("[DailyUpdatesV2] stream error: %v", err)
			errChunk, _ := json.Marshal(gin.H{"error": err.Error()})
			_, _ = w.Write(append(errChunk, '\n'))
			if ok {
				flusher.Flush()
			}
		}
		return
	}

	pageParam := parseDailyUpdatePage(c)
	if pageParam == nil {
		pageParam = &dto.Page{Current: 1, PageSize: 20}
	}
	random := c.Query("random") == "true" || c.Query("random") == "1"
	exclude := parseDailyUpdateExclude(c.Query("exclude"))

	list, page, err := service.IndexSvc.DailyUpdatesPaged(service.DailyUpdateListReq{
		Page:    pageParam,
		Random:  random,
		Exclude: exclude,
	})
	if err != nil {
		dto.Failed("获取每日更新失败", c)
		return
	}

	dto.Success(gin.H{
		"list": list,
		"page": page,
	}, "获取每日更新成功", c)
}

func parseDailyUpdatePage(c *gin.Context) *dto.Page {
	pageRaw := strings.TrimSpace(c.Query("page"))
	if pageRaw == "" {
		pageRaw = strings.TrimSpace(c.Query("current"))
	}
	sizeRaw := strings.TrimSpace(c.Query("pageSize"))
	if sizeRaw == "" {
		sizeRaw = strings.TrimSpace(c.Query("size"))
	}

	// 如果没有显式传分页参数，返回 nil 保持兼容首页卡片
	if pageRaw == "" && sizeRaw == "" {
		return nil
	}

	current, _ := strconv.Atoi(pageRaw)
	if current <= 0 {
		current = 1
	}
	pageSize, _ := strconv.Atoi(sizeRaw)
	if pageSize <= 0 {
		// 若传了 limit，优先作为 pageSize
		if limit, _ := strconv.Atoi(c.Query("limit")); limit > 0 {
			pageSize = limit
		} else {
			pageSize = 20
		}
	}
	return &dto.Page{Current: current, PageSize: pageSize}
}

func parseDailyUpdateLimit(raw string) int {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return 0
	}
	return n
}

func parseDailyUpdateExclude(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, len(parts))
	seen := make(map[int64]struct{}, len(parts))
	for _, part := range parts {
		id, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
		if err != nil || id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		out = append(out, id)
	}
	return out
}

// CategoriesInfo 分类信息获取
func (h *IndexHandler) CategoriesInfo(c *gin.Context) {
	data := service.IndexSvc.GetNavCategory()
	if len(data) <= 0 {
		dto.Failed("暂无分类信息", c)
		return
	}
	dto.Success(data, "分类信息获取成功", c)
}

// FilmPlayInfo 影视播放页数据
func (h *IndexHandler) FilmPlayInfo(c *gin.Context) {
	totalStartedAt := time.Now()
	id, err := strconv.Atoi(c.DefaultQuery("id", "0"))
	if err != nil {
		dto.Failed("请求异常,暂无影片信息!!!", c)
		return
	}
	playFrom := c.DefaultQuery("playFrom", "")
	episode, err := strconv.Atoi(c.DefaultQuery("episode", "0"))
	if err != nil {
		dto.Failed("请求异常,暂无影片信息!!!", c)
		return
	}
	detailStartedAt := time.Now()
	detail, err := service.IndexSvc.GetFilmDetail(id)
	logSlowIndexStep("FilmPlayInfo.GetFilmDetail", detailStartedAt, "id", id)
	if err != nil {
		dto.Failed("影片详情数据异常", c)
		return
	}
	if detail.Id == 0 {
		dto.Failed("暂无影片信息", c)
		return
	}
	for i := range detail.List {
		var valid []model.MovieUrlInfo
		for _, ep := range detail.List[i].LinkList {
			if ep.Link != "" {
				valid = append(valid, ep)
			}
		}
		detail.List[i].LinkList = valid
	}
	if len(detail.List) > 0 {
		playFrom = resolvePlayableSourceID(detail.List, playFrom)
	}
	var currentPlay model.MovieUrlInfo
	for _, v := range detail.List {
		if v.Id == playFrom {
			if len(v.LinkList) > 0 {
				if episode < len(v.LinkList) {
					currentPlay = v.LinkList[episode]
				} else {
					currentPlay = v.LinkList[0]
					episode = 0
				}
			}
			break
		}
	}

	logSlowIndexStep("FilmPlayInfo.total", totalStartedAt, "id", id)
	dto.Success(gin.H{
		"detail":          detail,
		"current":         currentPlay,
		"currentPlayFrom": playFrom,
		"currentEpisode":  episode,
		"relate":          []model.MovieBasicInfo{},
	}, "影片播放信息获取成功", c)
}

// FilmRelate 影视播放页相关推荐数据
func (h *IndexHandler) FilmRelate(c *gin.Context) {
	startedAt := time.Now()
	id, err := strconv.Atoi(c.DefaultQuery("id", "0"))
	if err != nil || id <= 0 {
		dto.Failed("请求异常,暂无影片信息!!!", c)
		return
	}

	page := dto.Page{Current: 0, PageSize: 14}
	relateMovie := service.IndexSvc.RelateMovie(int64(id), &page)
	logSlowIndexStep("FilmRelate.total", startedAt, "id", id)
	dto.Success(relateMovie, "相关推荐获取成功", c)
}

// SearchFilm 通过片名模糊匹配库存中的信息
func (h *IndexHandler) SearchFilm(c *gin.Context) {
	keyword := c.DefaultQuery("keyword", "")
	page := dto.GetPageParams(c)
	page.PageSize = 10
	bl := service.IndexSvc.SearchFilmInfo(strings.TrimSpace(keyword), page)
	if page.Total <= 0 {
		dto.Failed("暂无相关影片信息", c)
		return
	}

	dto.Success(gin.H{"list": bl, "page": page}, "影片搜索成功", c)
}

// FilmTagSearch 通过tag获取满足条件的对应影片
func (h *IndexHandler) FilmTagSearch(c *gin.Context) {
	params := model.SearchTagsVO{}
	pidStr := c.DefaultQuery("Pid", "")
	cidStr := c.DefaultQuery("Category", "")
	yStr := c.DefaultQuery("Year", "")
	if pidStr == "" {
		dto.Failed("缺少分类信息", c)
		return
	}
	params.Pid, _ = strconv.ParseInt(pidStr, 10, 64)
	params.Cid, _ = strconv.ParseInt(cidStr, 10, 64)
	params.Plot = c.DefaultQuery("Plot", "")
	params.Area = c.DefaultQuery("Area", "")
	params.Language = c.DefaultQuery("Language", "")
	params.Year = yStr
	params.Sort = c.DefaultQuery("Sort", "update_stamp")

	page := dto.GetPageParams(c)
	page.PageSize = 49

	cat := service.IndexSvc.GetPidCategory(params.Pid)

	list, err := service.IndexSvc.GetFilmsByTags(params, page)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	if list == nil {
		list = make([]model.MovieBasicInfo, 0)
	}
	searchTags := service.IndexSvc.SearchTags(params)

	var titleObj *model.Category
	if cat != nil {
		titleObj = &model.Category{
			Id:        cat.Id,
			Pid:       cat.Pid,
			Name:      cat.Name,
			Alias:     cat.Alias,
			Show:      cat.Show,
			Sort:      cat.Sort,
			CreatedAt: cat.CreatedAt,
			UpdatedAt: cat.UpdatedAt,
		}
	}

	response := gin.H{
		"title": titleObj,
		"list":  list,
		"params": map[string]string{
			"Pid":      pidStr,
			"Category": cidStr,
			"Plot":     params.Plot,
			"Area":     params.Area,
			"Language": params.Language,
			"Year":     yStr,
			"Sort":     params.Sort,
		},
		"page": page,
	}
	if hasSearchOptions(searchTags) {
		response["search"] = searchTags
	}
	dto.Success(response, "分类影片数据获取成功", c)
}

// FilmClassify  影片分类首页数据展示
func (h *IndexHandler) FilmClassify(c *gin.Context) {
	pidStr := c.DefaultQuery("Pid", "")
	if pidStr == "" {
		dto.Failed("主分类信息获取异常", c)
		return
	}
	pid, _ := strconv.ParseInt(pidStr, 10, 64)
	title := service.IndexSvc.GetPidCategory(pid)
	page := dto.GetPageParams(c)
	page.PageSize = 21
	dto.Success(gin.H{
		"title":   title,
		"content": service.IndexSvc.GetFilmClassify(pid, page),
	}, "分类影片信息获取成功", c)
}
