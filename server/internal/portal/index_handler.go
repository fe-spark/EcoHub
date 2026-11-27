package portal

import (
	"log"
	"server/internal/film/catalog"
	"server/internal/mediaproxy"
	"server/internal/sourcecatalog"
	"strconv"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

type IndexHandler struct{}

var IndexHd = new(IndexHandler)

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
	data, err := IndexSvc.IndexPage()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(WrapHomeData(data), "首页数据获取成功", c)
}

// DailyUpdates 近 24h 更新：
//
//	pid=0 全部；pid=-1 其他；pid>0 导航大类
//	current/page + pageSize/size 标准分页
//	random=1 时按分类随机抽样，exclude 排除已出现的 mid
func (h *IndexHandler) DailyUpdates(c *gin.Context) {
	result, err := IndexSvc.DailyUpdates(DailyUpdateListReq{
		Pid: parseDailyUpdatePid(queryFirst(c, "pid", "Pid")),
		Page: &dto.Page{
			Current:  parseQueryInt(queryFirst(c, "page", "current"), 1),
			PageSize: parseQueryInt(queryFirst(c, "pageSize", "size"), 21),
		},
		Random:  parseQueryBool(c.Query("random")),
		Exclude: parseDailyUpdateExclude(c.Query("exclude")),
	})
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(WrapDailyUpdateResult(result), "获取每日更新成功", c)
}

func pageSizeSpecified(c *gin.Context) bool {
	return c.Query("pageSize") != "" || c.Query("pagesize") != "" || c.Query("size") != "" || c.Query("limit") != ""
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
	// sourceId 可选：后台一键排片按站点列一级分类；不传时用首选站，行为不变。
	data, err := IndexSvc.GetNavCategory(c.Query("sourceId"))
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	if len(data) <= 0 {
		dto.Success([]*model.Category{}, "暂无分类信息", c)
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
	result := IndexSvc.SearchFilm(trimmed, "", sortField, page)
	if strings.TrimSpace(result.Error) != "" {
		dto.Failed(result.Error, c)
		return
	}
	if result.List == nil {
		result.List = []model.MovieBasicInfo{}
	}
	if result.Sources == nil {
		result.Sources = []model.SearchSourceTab{}
	}
	dto.Success(gin.H{"list": mediaproxy.WrapMovieBasicInfos(result.List), "page": page, "sort": sortField, "sources": result.Sources}, "影片搜索成功", c)
}

// HotKeywords 获取当前全站热门搜索推荐词
func (h *IndexHandler) HotKeywords(c *gin.Context) {
	limit := parseHotKeywordsLimit(c.Query("limit"))
	list, err := IndexSvc.GetHotSearchKeywords(limit)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(list, "热门搜索获取成功", c)
}

func buildFilterParamsMap(pidStr, cidStr, yStr string, params model.SearchTagsVO) map[string]string {
	return map[string]string{
		"pid":      pidStr,
		"category": cidStr,
		"plot":     params.Plot,
		"area":     params.Area,
		"language": params.Language,
		"year":     yStr,
		"sort":     params.Sort,
	}
}

func emptyClassifyContent() map[string]any {
	return map[string]any{
		"news":   []model.MovieBasicInfo{},
		"top":    []model.MovieBasicInfo{},
		"recent": []model.MovieBasicInfo{},
	}
}

// FilmTagSearch 通过tag获取满足条件的对应影片 (GET /api/films/filter)
func (h *IndexHandler) FilmTagSearch(c *gin.Context) {
	params := model.SearchTagsVO{}
	pidStr := queryFirst(c, "pid", "Pid")
	cidStr := queryFirst(c, "category", "Category", "cid", "Cid")
	yStr := queryFirst(c, "year", "Year")
	if pidStr == "" {
		dto.Failed("缺少分类信息", c)
		return
	}
	params.Pid, _ = strconv.ParseInt(pidStr, 10, 64)
	params.Cid, _ = strconv.ParseInt(cidStr, 10, 64)
	if params.Cid < 0 && params.Cid != model.TagUncategorizedValue {
		dto.Failed("分类参数无效", c)
		return
	}
	params.Plot = queryFirst(c, "plot", "Plot")
	params.Area = queryFirst(c, "area", "Area")
	params.Language = queryFirst(c, "language", "Language")
	params.Year = yStr
	params.Sort = utils.NormalizeSearchSortField(queryFirst(c, "sort", "Sort"))
	params.SourceId = sourcecatalog.CurrentPrimaryID()

	page := dto.GetPageParams(c)
	if !pageSizeSpecified(c) {
		page.PageSize = 48
	}

	cat := IndexSvc.GetPidCategory(params.Pid, params.SourceId)
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
	paramsMap := buildFilterParamsMap(pidStr, cidStr, yStr, params)
	respondSuccess := func(title *model.Category, list []model.MovieBasicInfo, searchTags map[string]any, indexReady *bool, msg string) {
		if list == nil {
			list = make([]model.MovieBasicInfo, 0)
		}
		payload := gin.H{
			"title":  title,
			"list":   list,
			"params": paramsMap,
			"page":   page,
		}
		if indexReady != nil {
			payload["indexReady"] = *indexReady
		}
		if hasSearchOptions(searchTags) {
			payload["search"] = searchTags
		}
		dto.Success(WrapFilmTagSearchResult(payload), msg, c)
	}
	boolPtr := func(v bool) *bool { return &v }

	// 分类不存在或已隐藏时直接返回空 title，避免被未就绪的检索索引报错掩盖失效状态
	if titleObj == nil {
		respondSuccess(nil, nil, nil, nil, "分类影片数据获取成功")
		return
	}

	list, err := IndexSvc.GetFilmsByTags(params, page)
	if err != nil {
		if IsSearchIndexNotReadyError(err) {
			respondSuccess(titleObj, nil, nil, boolPtr(false), "检索索引未就绪")
			return
		}
		dto.Failed(err.Error(), c)
		return
	}
	searchTags, err := IndexSvc.SearchTags(params)
	if err != nil {
		if IsSearchIndexNotReadyError(err) {
			respondSuccess(titleObj, list, nil, boolPtr(false), "检索索引未就绪")
			return
		}
		dto.Failed(err.Error(), c)
		return
	}

	respondSuccess(titleObj, list, searchTags, boolPtr(true), "分类影片数据获取成功")
}

// FilmClassify 影片分类首页数据展示 (GET /api/categories/overview)
func (h *IndexHandler) FilmClassify(c *gin.Context) {
	sourceId := sourcecatalog.CurrentPrimaryID()
	pidStr := queryFirst(c, "pid", "Pid")
	var pid int64
	if pidStr != "" {
		pid, _ = strconv.ParseInt(pidStr, 10, 64)
	}
	title := IndexSvc.GetPidCategory(pid, sourceId)
	// 没带 pid 才落到第一个可见一级分类。带了却查不到说明已隐藏或失效，不再换成别的分类。
	if title == nil && pidStr == "" {
		tree, err := catalog.GetActiveCategoryTree(sourceId)
		if err == nil && len(tree.Children) > 0 {
			for _, child := range tree.Children {
				if child != nil && child.Show && child.Id > 0 {
					pid = child.Id
					title = child
					break
				}
			}
		}
	}
	if title == nil {
		dto.Failed("主分类信息获取异常", c)
		return
	}
	page := dto.GetPageParams(c)
	if !pageSizeSpecified(c) {
		page.PageSize = 24
	}
	content, err := IndexSvc.GetFilmClassify(pid, page, sourceId)
	if err != nil {
		if IsSearchIndexNotReadyError(err) {
			dto.Success(gin.H{
				"title":      title,
				"indexReady": false,
				"content":    WrapFilmClassifyContent(emptyClassifyContent()),
			}, "检索索引未就绪", c)
			return
		}
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(gin.H{
		"title":      title,
		"indexReady": true,
		"content":    WrapFilmClassifyContent(content),
	}, "分类影片信息获取成功", c)
}
