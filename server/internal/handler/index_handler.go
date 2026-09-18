package handler

import (
	"log"
	"strconv"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

type IndexHandler struct{}

var IndexHd = new(IndexHandler)

// Health 健康检查接口
// Deprecated: 后续主版本计划移除。
// 该接口仅返回静态健康状态，无法校验私有化密钥安全与站点核心依赖。
// 探活与站点公开基础信息请统一使用 /api/config/basic；EcoHub 客户端软件源鉴权测通统一使用 /api/provide/app。
func Health(c *gin.Context) {
	dto.Success(gin.H{"status": "ok"}, "服务正常", c)
}

func hasSearchOptions(searchTags map[string]any) bool {
	if len(searchTags) == 0 {
		return false
	}
	tags, ok := searchTags["tags"].(map[string]any)
	if !ok {
		return false
	}
	for _, value := range tags {
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

// DailyUpdates 近 24h 更新。
// Deprecated: 保持早期版本（beta.3）原始接口契约（不传 limit 返回全部；传 limit 则随机抽取，exclude 排除当前批次）。
// 后续主版本计划移除，请迁移至 /api/dailyUpdates (DailyUpdatesV2)，支持 pid 大类分类、标准分页、排除已出现项与随机抽样。
func (h *IndexHandler) DailyUpdates(c *gin.Context) {
	data := service.IndexSvc.HomeDailyUpdates(parseDailyUpdateLimit(c.Query("limit")), parseDailyUpdateExclude(c.Query("exclude")))
	if data == nil {
		data = make([]model.MovieBasicInfo, 0)
	}
	dto.Success(data, "每日更新获取成功", c)
}

// DailyUpdatesV2 近 24h 更新：
//
//	pid=0 全部；pid=-1 其他；pid>0 导航大类
//	current/page + pageSize/size 标准分页
//	random=1 时按分类随机抽样，exclude 排除已出现的 mid
func (h *IndexHandler) DailyUpdatesV2(c *gin.Context) {
	result, err := service.IndexSvc.DailyUpdatesV2(service.DailyUpdateListReq{
		Pid: parseDailyUpdatePid(queryFirst(c, "pid", "Pid")),
		Page: &dto.Page{
			Current:  parseQueryInt(queryFirst(c, "page", "current"), 1),
			PageSize: parseQueryInt(queryFirst(c, "pageSize", "size"), 21),
		},
		Random:  parseQueryBool(c.Query("random")),
		Exclude: parseDailyUpdateExclude(c.Query("exclude")),
	})
	if err != nil {
		dto.Failed("获取每日更新失败", c)
		return
	}
	dto.Success(result, "获取每日更新成功", c)
}

func queryFirst(c *gin.Context, keys ...string) string {
	for _, key := range keys {
		if v := strings.TrimSpace(c.Query(key)); v != "" {
			return v
		}
	}
	return ""
}

func parseQueryInt(raw string, fallback int) int {
	if raw == "" {
		return fallback
	}
	n, err := strconv.Atoi(raw)
	if err != nil {
		return fallback
	}
	return n
}

const (
	hotKeywordsDefaultLimit = 8
	hotKeywordsMaxLimit     = 20
)

func parseHotKeywordsLimit(raw string) int {
	n := parseQueryInt(raw, hotKeywordsDefaultLimit)
	if n <= 0 {
		return hotKeywordsDefaultLimit
	}
	if n > hotKeywordsMaxLimit {
		return hotKeywordsMaxLimit
	}
	return n
}

func parseQueryBool(raw string) bool {
	raw = strings.ToLower(strings.TrimSpace(raw))
	return raw == "1" || raw == "true"
}

func parseDailyUpdatePid(raw string) int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return 0
	}
	n, err := strconv.ParseInt(raw, 10, 64)
	if err != nil {
		return 0
	}
	return n
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

const dailyUpdateExcludeCap = 500

func parseDailyUpdateExclude(raw string) []int64 {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return nil
	}
	parts := strings.Split(raw, ",")
	out := make([]int64, 0, min(len(parts), dailyUpdateExcludeCap))
	seen := make(map[int64]struct{}, min(len(parts), dailyUpdateExcludeCap))
	for _, part := range parts {
		if len(out) >= dailyUpdateExcludeCap {
			break
		}
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


const (
	searchFilmDefaultPageSize = 12
	searchFilmMaxPageSize     = 50
)

func clampSearchFilmPageSize(pageSize int, specified bool) int {
	if !specified || pageSize <= 0 {
		return searchFilmDefaultPageSize
	}
	if pageSize > searchFilmMaxPageSize {
		return searchFilmMaxPageSize
	}
	return pageSize
}

func resolveSearchFilmPageSize(c *gin.Context, page *dto.Page) {
	specified := c.Query("pageSize") != "" || c.Query("pagesize") != "" || c.Query("limit") != ""
	page.PageSize = clampSearchFilmPageSize(page.PageSize, specified)
}

// SearchFilm 通过全维智能模糊检索库存中的信息（支持相关度/热度/最新/评分排序）
func (h *IndexHandler) SearchFilm(c *gin.Context) {
	keyword := c.DefaultQuery("keyword", "")
	sortField := utils.NormalizeSearchSortField(c.DefaultQuery("sort", ""))
	page := dto.GetPageParams(c)
	resolveSearchFilmPageSize(c, page)
	trimmed := strings.TrimSpace(keyword)
	sourceID := strings.TrimSpace(c.Query("source"))
	result := service.IndexSvc.SearchFilm(trimmed, sourceID, sortField, page)
	if result.List == nil {
		result.List = []model.MovieBasicInfo{}
	}
	if result.Sources == nil {
		result.Sources = []model.SearchSourceTab{}
	}

	dto.Success(gin.H{"list": result.List, "page": page, "sort": sortField, "sources": result.Sources}, "影片搜索成功", c)
}

// HotKeywords 获取当前全站热门搜索推荐词
func (h *IndexHandler) HotKeywords(c *gin.Context) {
	limit := parseHotKeywordsLimit(c.Query("limit"))
	list := service.IndexSvc.GetHotSearchKeywords(limit)
	dto.Success(list, "热门搜索获取成功", c)
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
	if c.Query("pageSize") == "" && c.Query("pagesize") == "" && c.Query("limit") == "" {
		page.PageSize = 48
	}

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
