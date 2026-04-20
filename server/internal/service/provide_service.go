package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/utils"
)

type ProvideService struct{}

var ProvideSvc = new(ProvideService)

func resolveProvideType(search model.SearchInfo) (int64, string) {
	if search.Cid > 0 {
		if name := repository.GetCategoryNameById(search.Cid); name != "" {
			return search.Cid, name
		}
		return search.Cid, search.CName
	}
	if search.Pid > 0 {
		if name := repository.GetMainCategoryName(search.Pid); name != "" {
			return search.Pid, name
		}
		return search.Pid, search.CName
	}
	return 0, search.CName
}

// GetVodDirectBySource 获取指定采集站直连原始数据(MacCMS 兼容)
func (p *ProvideService) GetVodDirectBySource(sourceId, ac string, t int, pg int, wd string, h int, ids string, year int, area, lang, plot, sort string) ([]byte, error) {
	if sourceId == "" {
		return nil, errors.New("source is required")
	}
	s := repository.FindCollectSourceById(sourceId)
	if s == nil || !s.State {
		return nil, errors.New("collect source not found or disabled")
	}
	if s.ResultModel != model.JsonResult {
		return nil, errors.New("collect source is not json result")
	}

	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	if ac == "" {
		ac = "list"
	}
	r.Params.Set("ac", ac)
	if t > 0 {
		r.Params.Set("t", strconv.Itoa(t))
	}
	if pg > 0 {
		r.Params.Set("pg", strconv.Itoa(pg))
	}
	if wd != "" {
		r.Params.Set("wd", wd)
	}
	if h > 0 {
		r.Params.Set("h", strconv.Itoa(h))
	}
	if ids != "" {
		r.Params.Set("ids", ids)
	}
	if year > 0 {
		r.Params.Set("year", strconv.Itoa(year))
	}
	if area != "" {
		r.Params.Set("area", area)
	}
	if lang != "" {
		r.Params.Set("lang", lang)
		r.Params.Set("language", lang)
	}
	if plot != "" {
		r.Params.Set("plot", plot)
	}
	if sort != "" {
		r.Params.Set("sort", sort)
	}

	utils.ApiGet(&r)
	if len(r.Resp) > 0 {
		return r.Resp, nil
	}
	if r.Err != "" {
		return nil, errors.New(r.Err)
	}
	return nil, errors.New("empty response from collect source")
}

// GetClassList 获取格式化的分类列表和筛选条件
func (p *ProvideService) GetClassList() ([]model.FilmClass, map[string][]map[string]any) {
	// 1. 尝试从 Redis 获取缓存 (TVBox 配置缓存 5 分钟)
	cacheKey := config.TVBoxConfigCacheKey
	if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
		var res struct {
			ClassList []model.FilmClass
			Filters   map[string][]map[string]any
		}
		if json.Unmarshal([]byte(data), &res) == nil {
			return res.ClassList, res.Filters
		}
	}

	var classList []model.FilmClass
	filters := make(map[string][]map[string]any)

	tree := repository.GetActiveCategoryTree()

	type categoryResult struct {
		index   int
		item    model.FilmClass
		filters []map[string]any
	}

	resultChan := make(chan categoryResult, len(tree.Children))
	var wg sync.WaitGroup

	for i, c := range tree.Children {
		if !c.Show {
			continue
		}
		wg.Add(1)
		go func(index int, category *model.CategoryTree) {
			defer wg.Done()

			searchTags := filmrepo.GetSearchTag(model.SearchTagsVO{Pid: category.Id})
			tvboxFilters := make([]map[string]any, 0)

			// Robustly get metadata from searchTags
			titles := make(map[string]string)
			if tIf, ok := searchTags["titles"]; ok {
				if tMap, ok := tIf.(map[string]any); ok {
					for k, v := range tMap {
						if vStr, ok := v.(string); ok {
							titles[k] = vStr
						}
					}
				} else if tStrMap, ok := tIf.(map[string]string); ok {
					titles = tStrMap
				}
			}

			var sortList []string
			if sIf, ok := searchTags["sortList"]; ok {
				if sArr, ok := sIf.([]any); ok {
					for _, v := range sArr {
						if vStr, ok := v.(string); ok {
							sortList = append(sortList, vStr)
						}
					}
				} else if sStrArr, ok := sIf.([]string); ok {
					sortList = sStrArr
				}
			}

			var tags map[string]any
			if tMap, ok := searchTags["tags"].(map[string]any); ok {
				tags = tMap
			}

			for _, key := range sortList {
				name, ok := titles[key]
				if !ok {
					continue
				}

				var values []map[string]string
				tagDataIf := tags[key]
				if tagDataIf == nil {
					continue
				}

				switch td := tagDataIf.(type) {
				case []map[string]string:
					for _, item := range td {
						values = append(values, map[string]string{"n": item["Name"], "v": item["Value"]})
					}
				case []any:
					for _, item := range td {
						if m, ok := item.(map[string]any); ok {
							nStr, _ := m["Name"].(string)
							vStr, _ := m["Value"].(string)
							values = append(values, map[string]string{"n": nStr, "v": vStr})
						}
					}
				}

				if len(values) > 0 {
					tvboxKey := strings.ToLower(key)
					if key == "Category" {
						tvboxKey = "cid"
					}
					tvboxFilters = append(tvboxFilters, map[string]any{
						"key": tvboxKey, "name": name, "value": values,
					})
				}
			}

			resultChan <- categoryResult{
				index:   index,
				item:    model.FilmClass{ID: category.Id, Name: category.Name},
				filters: tvboxFilters,
			}
		}(i, c)
	}

	go func() {
		wg.Wait()
		close(resultChan)
	}()

	// 收集并保持顺序 (或根据分类权重排序，这里尝试保持原有 tree.Children 顺序)
	type finalItem struct {
		index   int
		item    model.FilmClass
		filters []map[string]any
	}
	var finals []finalItem
	for res := range resultChan {
		finals = append(finals, finalItem{res.index, res.item, res.filters})
	}

	// 按原始索引排序
	sort.Slice(finals, func(i, j int) bool {
		return finals[i].index < finals[j].index
	})

	for _, f := range finals {
		classList = append(classList, f.item)
		filters[strconv.FormatInt(f.item.ID, 10)] = f.filters
	}

	// 写入 Redis 缓存 (5 分钟)
	res := struct {
		ClassList []model.FilmClass
		Filters   map[string][]map[string]any
	}{classList, filters}
	if data, err := json.Marshal(res); err == nil {
		db.Rdb.Set(db.Cxt, cacheKey, string(data), time.Minute*5)
	}

	return classList, filters
}

// GetVodList 获取视频列表 (支持多维度筛选)
func (p *ProvideService) GetVodList(t int, cid int64, pg int, wd string, h int, year string, area, lang, plot, sort string, limit int) (int, int, int, []model.FilmList) {
	if limit <= 0 {
		limit = 20
	}
	if t <= 0 && cid == 0 && wd == "" && h == 0 && year == "" && area == "" && lang == "" && plot == "" {
		return 1, 1, 0, []model.FilmList{}
	}
	// 1. 针对第一页的首页请求尝试 Redis 缓存 (依赖主动失效，TTL 设为 12 小时作为兜底)
	cacheKey := ""
	if pg <= 1 && wd == "" && h == 0 && year == "" && area == "" && lang == "" && plot == "" && cid == 0 {
		cacheKey = fmt.Sprintf("%s:%d:C%d:S%s:L%d", config.TVBoxList, t, cid, sort, limit)
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var res struct {
				Current   int
				PageCount int
				Total     int
				VodList   []model.FilmList
			}
			if json.Unmarshal([]byte(data), &res) == nil {
				return res.Current, res.PageCount, res.Total, res.VodList
			}
		}
	}

	page := dto.Page{PageSize: limit, Current: pg}
	if page.Current <= 0 {
		page.Current = 1
	}

	query := db.Mdb.Model(&model.SearchInfo{})

	pid := int64(t)
	pid = repository.ResolveCategoryID(pid)
	if cid > 0 {
		cid = repository.ResolveCategoryID(cid)
	}
	if cid == model.TagUncategorizedValue && pid <= 0 {
		return 1, 1, 0, []model.FilmList{}
	}
	query = filmrepo.ApplyCategoryFilter(query, pid, cid)

	if wd != "" {
		query = query.Where("name LIKE ? OR sub_title LIKE ?", "%"+wd+"%", "%"+wd+"%")
	}

	if h > 0 {
		timeLimit := time.Now().Add(-time.Duration(h) * time.Hour).Unix()
		query = query.Where("update_stamp >= ?", timeLimit)
	}

	if year != "" && year != "全部" {
		switch year {
		case model.TagOthersValue, "其他", "其它":
			if pid > 0 {
				topVals := filmrepo.GetTopTagValues(pid, "Year")
				query = query.Where("year <> 0")
				if len(topVals) > 0 {
					query = query.Where("year NOT IN ?", topVals)
				}
			}
		case model.TagUnknownValue:
			query = query.Where("year = 0")
		default:
			if y, err := strconv.Atoi(year); err == nil && y > 0 {
				query = query.Where("year = ?", y)
			}
		}
	}

	// 统一处理“其它”逻辑
	// 2. 处理规范化维度 (Area, Language, Plot) - 全部切换到 MovieTagRel 索引查询
	dims := map[string]string{
		"Area":     area,
		"Language": lang,
		"Plot":     plot,
	}

	for dimType, val := range dims {
		if val == "" || val == "全部" {
			continue
		}
		switch {
		case (val == model.TagOthersValue || val == "其他" || val == "其它") && pid > 0:
			topVals := filmrepo.GetTopTagValues(pid, dimType)
			if dimType == "Plot" {
				query = query.Where("class_tag <> ''")
				maxPlotExcludes := 5
				if len(topVals) < maxPlotExcludes {
					maxPlotExcludes = len(topVals)
				}
				for i := 0; i < maxPlotExcludes; i++ {
					v := topVals[i]
					query = query.Where("class_tag NOT LIKE ?", fmt.Sprintf("%%%s%%", v))
				}
			} else {
				k := strings.ToLower(dimType)
				query = query.Where(fmt.Sprintf("%s <> ''", k))
				if len(topVals) > 0 {
					query = query.Where(fmt.Sprintf("%s NOT IN ?", k), topVals)
				}
			}
		case val == model.TagUnknownValue:
			if dimType == "Plot" {
				query = query.Where("(class_tag = '' OR class_tag IS NULL)")
			} else {
				k := strings.ToLower(dimType)
				query = query.Where(fmt.Sprintf("(%s = '' OR %s IS NULL)", k, k))
			}
		default:
			if dimType == "Plot" {
				query = query.Where("class_tag LIKE ?", fmt.Sprintf("%%%s%%", val))
			} else {
				k := strings.ToLower(dimType)
				query = query.Where(fmt.Sprintf("%s = ?", k), val)
			}
		}
	}

	dto.GetPage(query, &page)

	orderBy := "update_stamp DESC, mid DESC"
	switch sort {
	case "hits":
		orderBy = "hits DESC, mid DESC"
	case "score":
		orderBy = "score DESC, mid DESC"
	case "release_stamp":
		orderBy = "release_stamp DESC, mid DESC"
	}

	var sl []model.SearchInfo
	query.Limit(page.PageSize).Offset((page.Current - 1) * page.PageSize).Order(orderBy).Find(&sl)

	var vodList []model.FilmList
	for _, s := range sl {
		typeID, typeName := resolveProvideType(s)
		vodList = append(vodList, model.FilmList{
			VodID:       s.Mid,
			VodName:     s.Name,
			TypeID:      typeID,
			TypeName:    typeName,
			VodEn:       s.Initial,
			VodTime:     time.Unix(s.UpdateStamp, 0).Format("2006-01-02 15:04:05"),
			VodRemarks:  s.Remarks,
			VodPlayFrom: resolveProvidePlayFromSummary(s),
			VodPic:      s.Picture,
		})
	}

	// 2. 写入 Redis 缓存
	if cacheKey != "" {
		res := struct {
			Current   int
			PageCount int
			Total     int
			VodList   []model.FilmList
		}{page.Current, page.PageCount, page.Total, vodList}
		if data, err := json.Marshal(res); err == nil {
			db.Rdb.Set(db.Cxt, cacheKey, string(data), time.Hour*12)
		}
	}

	return page.Current, page.PageCount, page.Total, vodList
}

func resolveProvidePlayFromSummary(search model.SearchInfo) string {
	if strings.TrimSpace(search.PlayFromSummary) == "" {
		return config.PlayFormCloud
	}
	return search.PlayFromSummary
}

// GetVodDetail 获取视频详情（带播放列表）
func (p *ProvideService) GetVodDetail(ids []string) []model.FilmDetail {
	var detailList []model.FilmDetail

	for _, idStr := range ids {
		idInt, err := strconv.Atoi(idStr)
		if err != nil {
			continue
		}
		var s model.SearchInfo
		if err := db.Mdb.Where("mid = ?", idStr).First(&s).Error; err != nil {
			continue
		}

		movieDetailVo := IndexSvc.GetFilmDetail(idInt)

		if movieDetailVo.Id == 0 && movieDetailVo.Name == "" {
			continue
		}
		typeID, typeName := resolveProvideType(s)

		var playFromList []string
		var playUrlList []string

		for _, source := range movieDetailVo.List {
			playFromList = append(playFromList, source.Name)

			var linkStrs []string
			for _, link := range source.LinkList {
				playLink := link.Link
				linkStrs = append(linkStrs, fmt.Sprintf("%s$%s", link.Episode, strings.ReplaceAll(playLink, "$", "")))
			}
			playUrlList = append(playUrlList, strings.Join(linkStrs, "#"))
		}

		detail := model.FilmDetail{
			VodID:       s.Mid,
			TypeID:      typeID,
			TypeID1:     s.Pid,
			TypeName:    typeName,
			VodName:     s.Name,
			VodEn:       s.Initial,
			VodTime:     time.Unix(s.UpdateStamp, 0).Format("2006-01-02 15:04:05"),
			VodRemarks:  s.Remarks,
			VodPlayFrom: strings.Join(playFromList, "$$$"),
			VodPlayURL:  strings.Join(playUrlList, "$$$"),
			VodPic:      movieDetailVo.Picture,
			VodSub:      movieDetailVo.SubTitle,
			VodClass:    movieDetailVo.ClassTag,
			VodActor:    movieDetailVo.Actor,
			VodDirector: movieDetailVo.Director,
			VodWriter:   movieDetailVo.Writer,
			VodBlurb:    movieDetailVo.Blurb,
			VodPubDate:  movieDetailVo.ReleaseDate,
			VodArea:     movieDetailVo.Area,
			VodLang:     movieDetailVo.Language,
			VodYear:     movieDetailVo.Year,
			VodState:    movieDetailVo.State,
			VodHits:     s.Hits,
			VodScore:    movieDetailVo.DbScore,
			VodContent:  movieDetailVo.Content,
		}
		detailList = append(detailList, detail)
	}

	return detailList
}
