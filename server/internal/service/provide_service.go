package service

import (
	"encoding/json"
	"fmt"
	"math/rand"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmshared "server/internal/repository/film/shared"
	filmsnapshot "server/internal/repository/film/snapshot"
)

type ProvideService struct{}

var (
	ProvideSvc       = new(ProvideService)
	tvboxListSfGroup singleflight.Group
)

// GetClassList 获取格式化的分类列表和筛选条件
func (p *ProvideService) GetClassList(sourceIdOpt ...string) ([]model.FilmClass, map[string][]map[string]any) {
	var sourceId string
	if len(sourceIdOpt) > 0 {
		sourceId = sourceIdOpt[0]
	}
	sourceId = resolveProvideSourceID(sourceId)
	// 1. 尝试从 Redis 获取缓存 (TVBox 配置缓存 5 分钟)
	categoryVersion := repository.GetCategoryVersion()
	cacheKey := fmt.Sprintf("%s:v%s", config.TVBoxConfigCacheKey, categoryVersion)
	if sourceId != "" {
		cacheKey = fmt.Sprintf("%s:src_%s:v%s", config.TVBoxConfigCacheKey, sourceId, categoryVersion)
	}
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var res struct {
				ClassList []model.FilmClass
				Filters   map[string][]map[string]any
			}
			if json.Unmarshal([]byte(data), &res) == nil {
				return res.ClassList, res.Filters
			}
		}
	}

	var classList []model.FilmClass
	filters := make(map[string][]map[string]any)

	tree := repository.GetActiveCategoryTree(sourceId)

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

			searchTags := filmsnapshot.GetFilterOptionSnapshot(filmsnapshot.GetActiveReadModelVersion(), category.Id, sourceId)
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
	if db.Rdb != nil {
		res := struct {
			ClassList []model.FilmClass
			Filters   map[string][]map[string]any
		}{classList, filters}
		if data, err := json.Marshal(res); err == nil {
			db.Rdb.Set(db.Cxt, cacheKey, string(data), time.Minute*5)
		}
	}

	return classList, filters
}

// GetVodList 获取视频列表 (支持多维度筛选与采集源隔离)
func (p *ProvideService) GetVodList(sourceId string, t int, cid int64, pg int, wd string, h int, year string, area, lang, plot, sort string, limit int) (int, int, int, []model.FilmList, error) {
	if limit <= 0 {
		limit = 20
	}
	wd = strings.TrimSpace(wd)
	sourceId = resolveProvideSourceID(sourceId)
	// 快速过滤非正常片名（例如 URL 或长度过长字符串），避免无意义全表扫描
	if len([]rune(wd)) > 64 || strings.HasPrefix(wd, "http://") || strings.HasPrefix(wd, "https://") {
		return 1, 1, 0, []model.FilmList{}, nil
	}
	version := filmsnapshot.GetActiveReadModelVersion()
	ruleVersion := repository.GetRuleVersion()
	categoryVersion := repository.GetCategoryVersion()

	type tvboxListResult struct {
		Current   int
		PageCount int
		Total     int
		VodList   []model.FilmList
	}

	// 1. 常规列表页尝试 Redis 缓存，采集写库期间避免 TVBox 翻页反复压 MySQL。
	cacheKey := ""
	if wd == "" && h == 0 && year == "" && area == "" && lang == "" && plot == "" {
		if sourceId != "" {
			cacheKey = fmt.Sprintf("%s:v%s:r%s:c%s:src_%s:T%d:C%d:P%d:S%s:L%d", config.TVBoxList, version, ruleVersion, categoryVersion, sourceId, t, cid, pg, sort, limit)
		} else {
			cacheKey = fmt.Sprintf("%s:v%s:r%s:c%s:T%d:C%d:P%d:S%s:L%d", config.TVBoxList, version, ruleVersion, categoryVersion, t, cid, pg, sort, limit)
		}
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				var res tvboxListResult
				if json.Unmarshal([]byte(data), &res) == nil {
					return res.Current, res.PageCount, res.Total, res.VodList, nil
				}
			}
		}
	}

	fetchList := func() (tvboxListResult, error) {
		listGen := filmsnapshot.GetSearchCacheVersion()
		page := dto.Page{PageSize: limit, Current: pg}
		if page.Current <= 0 {
			page.Current = 1
		}

		pid := int64(t)
		if cid == model.TagUncategorizedValue && pid <= 0 {
			return tvboxListResult{Current: 1, PageCount: 1, Total: 0, VodList: []model.FilmList{}}, nil
		}

		searchTags := model.SearchTagsVO{
			Pid:      pid,
			Cid:      cid,
			Area:     strings.TrimSpace(area),
			Language: strings.TrimSpace(lang),
			Plot:     strings.TrimSpace(plot),
			Year:     strings.TrimSpace(year),
			Sort:     strings.TrimSpace(sort),
			SourceId: strings.TrimSpace(sourceId),
		}
		if err := validateReadModelSearchTags(searchTags); err != nil {
			return tvboxListResult{Current: page.Current, PageCount: 1, Total: 0, VodList: []model.FilmList{}}, err
		}
		sl, err := filmsnapshot.ListProvideSnapshotsFast(version, searchTags, wd, h, &page)
		if err != nil {
			return tvboxListResult{}, err
		}
		sl = filmshared.ApplyPosterSourcePictures(sl)

		var vodList []model.FilmList
		for _, s := range sl {
			typeID, typeName := resolveProvideListedType(s, sourceId)
			vodList = append(vodList, model.FilmList{
				VodID:       s.Mid,
				VodName:     s.Name,
				TypeID:      typeID,
				TypeName:    typeName,
				VodEn:       s.Initial,
				VodTime:     resolveProvideSnapshotVodTime(s),
				VodRemarks:  s.Remarks,
				VodPlayFrom: resolveProvideSnapshotPlayFromSummary(s),
				VodPic:      s.DisplayPicture(),
			})
		}

		res := tvboxListResult{
			Current:   page.Current,
			PageCount: page.PageCount,
			Total:     page.Total,
			VodList:   vodList,
		}

		// 2. 写入 Redis 缓存 (加随机打散防雪崩，针对空列表使用短 TTL 防长期穿透)
		if cacheKey != "" && db.Rdb != nil {
			if data, err := json.Marshal(res); err == nil {
				jitter := time.Duration(rand.Intn(900)) * time.Second
				ttl := time.Hour*12 + jitter
				if res.Total == 0 || len(res.VodList) == 0 {
					ttl = time.Minute
				}
				filmsnapshot.StoreCacheIfCurrent(cacheKey, string(data), ttl, listGen)
			}
		}

		return res, nil
	}

	if cacheKey != "" {
		val, err, _ := tvboxListSfGroup.Do(cacheKey, func() (any, error) {
			// Double check 缓存
			if db.Rdb != nil {
				if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
					var res tvboxListResult
					if json.Unmarshal([]byte(data), &res) == nil {
						return res, nil
					}
				}
			}
			return fetchList()
		})
		if err != nil {
			return pg, 1, 0, []model.FilmList{}, err
		}
		res := val.(tvboxListResult)
		return res.Current, res.PageCount, res.Total, res.VodList, nil
	}

	res, err := fetchList()
	return res.Current, res.PageCount, res.Total, res.VodList, err
}

// GetTVBoxConfig 获取 TVBox 网络配置（sites 源站列表，支持缓存）
func (p *ProvideService) GetTVBoxConfig(baseURL, key string) (map[string]any, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	cacheKey := config.TVBoxNetworkConfigCacheKey + ":" + url.QueryEscape(baseURL)
	if key != "" {
		cacheKey += ":" + url.QueryEscape(key)
	}
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached map[string]any
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached, nil
			}
		}
	}

	apiPath := baseURL + "/api/provide/vod"
	sites := make([]map[string]any, 0)
	for _, source := range repository.GetEnabledCollectSourceList() {
		sourceApi := apiPath + "?source=" + url.QueryEscape(source.Id)
		if key != "" {
			sourceApi += "&key=" + url.QueryEscape(key)
		}
		sites = append(sites, map[string]any{
			"key":         "source_" + source.Id,
			"name":        "📡 " + source.Name,
			"type":        1,
			"api":         sourceApi,
			"searchable":  1,
			"quickSearch": 1,
			"filterable":  1,
		})
	}

	configJson := map[string]any{
		"spider":    "",
		"wallpaper": "",
		"logo":      "",
		"sites":     sites,
	}
	if db.Rdb != nil {
		if data, err := json.Marshal(configJson); err == nil {
			db.Rdb.Set(db.Cxt, cacheKey, string(data), time.Minute*30)
		}
	}

	return configJson, nil
}

// GetAppSourceConfig 提供给 EcoHub 官方原生客户端（OHOS / Android）的软件源描述配置
func (p *ProvideService) GetAppSourceConfig(baseURL string) (map[string]any, error) {
	baseURL = strings.TrimRight(baseURL, "/")
	basic := repository.GetSiteBasic()
	siteName := strings.TrimSpace(basic.SiteName)
	if siteName == "" {
		siteName = "EcoHub"
	}

	return map[string]any{
		"code": 1,
		"msg":  "EcoHub 客户端软件源在线",
		"data": map[string]any{
			"site_name":      siteName,
			"site_url":       baseURL,
			"api_base":       baseURL + "/api",
			"version":        config.Version,
			"private_access": basic.PrivateAccess,
			"status":         "online",
		},
	}, nil
}
