package repository

import (
	"encoding/json"
	"fmt"
	"log"
	"reflect"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

// ExistSearchTable 检查搜索表是否存在
func ExistSearchTable() bool {
	return db.Mdb.Migrator().HasTable(&model.SearchInfo{})
}

func ExistSearchInMid(mid int64) bool {
	var count int64
	db.Mdb.Model(&model.SearchInfo{}).Where("mid = ?", mid).Count(&count)
	return count > 0
}

// ========= Upsert Logic =========

var upsertColumns = []string{
	"mid", "cid", "pid", "root_category_key", "category_key", "name", "sub_title", "c_name", "class_tag",
	"area", "language", "year", "initial", "score",
	"update_stamp", "hits", "state", "remarks", "release_stamp",
	"picture", "actor", "director", "blurb", "updated_at", "deleted_at",
}

func BatchSaveOrUpdate(list []model.SearchInfo) map[string]int64 {
	// 过滤无意义的空记录
	validList := make([]model.SearchInfo, 0, len(list))
	for _, v := range list {
		if strings.TrimSpace(v.Name) != "" {
			validList = append(validList, v)
		}
	}
	if len(validList) == 0 {
		return nil
	}
	list = validList

	// 1. 基于 ContentKey 进行冲突检测，实现内容级的去重
	if err := db.Mdb.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "content_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"source_id", "cid", "pid", "root_category_key", "category_key", "name", "sub_title", "c_name", "class_tag",
			"area", "language", "year", "initial", "score",
			"update_stamp", "hits", "state", "remarks", "release_stamp",
			"picture", "actor", "director", "blurb", "updated_at",
		}),
	}).CreateInBatches(&list, 200).Error; err != nil {
		log.Printf("BatchSaveOrUpdate upsert 失败: %v\n", err)
		return nil
	}

	// 清除相关分类的搜索标签缓存
	pidSet := make(map[int64]struct{})
	for _, v := range list {
		pidSet[v.Pid] = struct{}{}
	}
	for pid := range pidSet {
		ClearSearchTagsCache(pid)
	}
	// 清除首页活跃分类树缓存，防止处于采集早期时前台只能看到空缓存
	db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)

	// 2. 建立来源映射关系 (获取最终生效的 GlobalMid)
	var contentKeys []string
	for _, v := range list {
		contentKeys = append(contentKeys, v.ContentKey)
	}

	var latestInfos []model.SearchInfo
	db.Mdb.Where("content_key IN ?", contentKeys).Find(&latestInfos)

	keyToMid := make(map[string]int64)
	for _, info := range latestInfos {
		keyToMid[info.ContentKey] = info.Mid
	}

	var mappings []model.MovieSourceMapping
	for _, v := range list {
		if globalMid, ok := keyToMid[v.ContentKey]; ok {
			mappings = append(mappings, model.MovieSourceMapping{
				SourceId:  v.SourceId,
				SourceMid: v.Mid,
				GlobalMid: globalMid,
			})
		}
	}

	if len(mappings) > 0 {
		db.Mdb.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_mid"}},
			DoUpdates: clause.AssignmentColumns([]string{"global_mid", "updated_at"}),
		}).CreateInBatches(&mappings, 200)
	}

	BatchHandleSearchTag(list...)
	return keyToMid
}

func SaveSearchInfo(s model.SearchInfo) error {
	// 同样采用 ContentKey 去重策略，确保 Mid 唯一归约
	err := db.Mdb.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "content_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"source_id", "cid", "pid", "root_category_key", "category_key", "name", "sub_title", "c_name", "class_tag",
			"area", "language", "year", "initial", "score",
			"update_stamp", "hits", "state", "remarks", "release_stamp",
			"picture", "actor", "director", "blurb", "updated_at",
		}),
	}).Create(&s).Error

	if err == nil {
		// 记录映射
		var info model.SearchInfo
		db.Mdb.Where("content_key = ?", s.ContentKey).First(&info)
		db.Mdb.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_mid"}},
			DoUpdates: clause.AssignmentColumns([]string{"global_mid", "updated_at"}),
		}).Create(&model.MovieSourceMapping{
			SourceId:  s.SourceId,
			SourceMid: s.Mid,
			GlobalMid: info.Mid,
		})
	}

	BatchHandleSearchTag(s)
	// 单条记录更新也需要清除该分类的搜索标签缓存，防止联动数据过时
	ClearSearchTagsCache(s.Pid)
	return err
}

func SaveDetails(id string, list []model.MovieDetail) error {
	var infoList []model.SearchInfo
	infoByKey := make(map[string]model.SearchInfo, len(list))
	for _, v := range list {
		info := ConvertSearchInfo(id, v)
		infoList = append(infoList, info)
		infoByKey[info.ContentKey] = info
	}
	keyToMid := BatchSaveOrUpdate(infoList)

	var details []model.MovieDetailInfo
	for _, v := range list {
		// 获取内容标识
		info := infoByKey[buildContentKey(v)]
		globalMid, ok := keyToMid[info.ContentKey]
		if !ok {
			globalMid = v.Id
		}

		applyResolvedCategory(&v, info)
		v.Id = globalMid // 将详情内的 ID 也归约到 Global ID
		data, _ := json.Marshal(v)
		details = append(details, model.MovieDetailInfo{Mid: globalMid, SourceId: id, Content: string(data)})
	}

	if len(details) > 0 {
		return db.Mdb.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "mid"}},
			DoUpdates: clause.AssignmentColumns([]string{"source_id", "content", "updated_at"}),
		}).Create(&details).Error
	}
	return nil
}

func SaveDetail(id string, detail model.MovieDetail) error {
	searchInfo := ConvertSearchInfo(id, detail)
	// 模拟 BatchSaveOrUpdate 逻辑获取 GlobalMid
	if err := db.Mdb.Clauses(clause.OnConflict{
		Columns: []clause.Column{{Name: "content_key"}},
		DoUpdates: clause.AssignmentColumns([]string{
			"source_id", "cid", "pid", "root_category_key", "category_key", "name", "sub_title", "c_name", "class_tag",
			"area", "language", "year", "initial", "score",
			"update_stamp", "hits", "state", "remarks", "release_stamp",
			"picture", "actor", "director", "blurb", "updated_at",
		}),
	}).Create(&searchInfo).Error; err != nil {
		return err
	}

	// 查回最终生效的 Mid
	var info model.SearchInfo
	db.Mdb.Where("content_key = ?", searchInfo.ContentKey).First(&info)
	globalMid := info.Mid

	// 映射记录
	db.Mdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_mid"}},
		DoUpdates: clause.AssignmentColumns([]string{"global_mid", "updated_at"}),
	}).Create(&model.MovieSourceMapping{
		SourceId:  id,
		SourceMid: detail.Id,
		GlobalMid: globalMid,
	})

	applyResolvedCategory(&detail, searchInfo)
	detail.Id = globalMid
	data, _ := json.Marshal(detail)
	err := db.Mdb.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "mid"}},
		DoUpdates: clause.AssignmentColumns([]string{"source_id", "content", "updated_at"}),
	}).Create(&model.MovieDetailInfo{Mid: globalMid, SourceId: id, Content: string(data)}).Error

	if err == nil {
		ClearSearchTagsCache(detail.Pid)
		db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
	}
	return err
}

func SaveSitePlayList(id string, list []model.MovieDetail) error {
	if len(list) <= 0 {
		return nil
	}
	var playlists []model.MoviePlaylist
	latestByContentKey := make(map[string]int64)
	for _, d := range list {
		if len(d.PlayList) == 0 || strings.Contains(d.CName, "解说") {
			continue
		}
		data, _ := json.Marshal(d.PlayList)
		stamp, err := time.ParseInLocation(time.DateTime, d.UpdateTime, time.Local)
		if err == nil {
			contentKey := buildContentKey(d)
			if contentKey != "" && stamp.Unix() > latestByContentKey[contentKey] {
				latestByContentKey[contentKey] = stamp.Unix()
			}
		}

		if d.DbId != 0 {
			playlists = append(playlists, model.MoviePlaylist{
				SourceId: id,
				MovieKey: utils.GenerateHashKey(d.DbId),
				Content:  string(data),
			})
		}
		playlists = append(playlists, model.MoviePlaylist{
			SourceId: id,
			MovieKey: utils.GenerateHashKey(d.Name),
			Content:  string(data),
		})
	}
	if len(playlists) > 0 {
		if err := db.Mdb.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "source_id"}, {Name: "movie_key"}},
			DoUpdates: clause.AssignmentColumns([]string{"content", "updated_at"}),
		}).Create(&playlists).Error; err != nil {
			log.Printf("SaveSitePlayList Error: %v", err)
			return err
		}
		if err := refreshLatestSourceStampByContentKeys(latestByContentKey); err != nil {
			log.Printf("refreshLatestSourceStampByContentKeys Error: %v", err)
			return err
		}
		log.Printf("[Playlist] 为站点 %s 保存了 %d 条记录\n", id, len(playlists))
	}
	return nil
}

func refreshLatestSourceStampByContentKeys(latestByContentKey map[string]int64) error {
	if len(latestByContentKey) == 0 {
		return nil
	}

	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		for contentKey, stamp := range latestByContentKey {
			if stamp <= 0 {
				continue
			}
			if err := tx.Model(&model.SearchInfo{}).
				Where("content_key = ?", contentKey).
				Where("latest_source_stamp < ? OR latest_source_stamp IS NULL", stamp).
				Update("latest_source_stamp", stamp).Error; err != nil {
				return err
			}
		}
		return nil
	})
}

// DeletePlaylistBySourceId 根据来源站点 ID 删除所有关联的播放列表资源
func DeletePlaylistBySourceId(sourceId string) error {
	return db.Mdb.Where("source_id = ?", sourceId).Delete(&model.MoviePlaylist{}).Error
}

// ========= Tag Operations =========

var initializedPids sync.Map

var defaultSortTagStrings = []string{"最新更新:latest_source_stamp", "人气:hits", "评分:score", "最新:release_stamp"}

const latestUpdateOrderSQL = "COALESCE(NULLIF(latest_source_stamp, 0), update_stamp) DESC, update_stamp DESC"

var allowedSearchSortColumns = map[string]string{
	"latest_source_stamp": "latest_source_stamp",
	"update_stamp":        "update_stamp",
	"hits":                "hits",
	"score":               "score",
	"release_stamp":       "release_stamp",
}

func BatchHandleSearchTag(infos ...model.SearchInfo) {
	if len(infos) == 0 {
		return
	}

	for pid := range collectSearchTagPids(infos) {
		ensureStaticTagsForPid(pid)
	}

	for _, info := range infos {
		handleDynamicSearchTags(info)
	}

	ClearAllSearchTagsCache()
}

func SaveSearchTag(search model.SearchInfo) {
	BatchHandleSearchTag(search)
}

func collectSearchTagPids(infos []model.SearchInfo) map[int64]bool {
	pids := make(map[int64]bool)
	for _, info := range infos {
		if info.Pid > 0 {
			pids[info.Pid] = true
		}
	}
	return pids
}

func handleDynamicSearchTags(info model.SearchInfo) {
	if info.Pid <= 0 {
		return
	}

	handleCategorySearchTag(info)
	handlePlotSearchTag(info)
	HandleSearchTags(info.Area, "Area", info.Pid)
	HandleSearchTags(info.Language, "Language", info.Pid)
	if info.Year > 0 {
		HandleSearchTags(fmt.Sprint(info.Year), "Year", info.Pid)
	}
}

func handleCategorySearchTag(info model.SearchInfo) {
	if info.Cid <= 0 {
		return
	}

	catName := GetCategoryNameById(info.Cid)
	if catName == "" {
		catName = info.CName
	}
	HandleSearchTags(catName, "Category", info.Pid, fmt.Sprint(info.Cid))
}

func handlePlotSearchTag(info model.SearchInfo) {
	mainCategoryName := GetMainCategoryName(info.Pid)
	cleanPlot := CleanPlotTags(info.ClassTag, info.Area, mainCategoryName, info.CName)
	HandleSearchTags(cleanPlot, "Plot", info.Pid)
}

func ensureStaticTagsForPid(pid int64) {
	// 1. 内存缓存检查，避免频繁查库
	if _, ok := initializedPids.Load(pid); ok {
		return
	}

	// 2. 初始化 Initial (A-Z)
	var initialItems []model.SearchTagItem
	for i := 65; i <= 90; i++ {
		v := string(rune(i))
		initialItems = append(initialItems, model.SearchTagItem{Pid: pid, TagType: "Initial", Name: v, Value: v, Score: int64(90 - i)})
	}
	db.Mdb.Clauses(clause.OnConflict{DoNothing: true}).Create(&initialItems)

	// 标记为已初始化
	initializedPids.Store(pid, true)
}

var (
	reTagCleanup = regexp.MustCompile(`[\s\n\r]+`)
	reTagSplit   = regexp.MustCompile(`[/,，、\s\.\+\|]`)
)

func HandleSearchTags(allTags string, tagType string, pid int64, customValues ...string) {
	allTags = reTagCleanup.ReplaceAllString(allTags, "")
	parts := reTagSplit.Split(allTags, -1)

	upsert := func(v string, customVal ...string) {
		v = strings.TrimSpace(v)
		if v == "" || v == model.TagOthersValue || v == "其他" || v == "其它" || v == "全部" || v == "完结" || v == "HD" || v == "解说" || v == "剧情" || v == "暂无" {
			return
		}

		val := v
		if len(customVal) > 0 {
			val = customVal[0]
		}

		// 核心逻辑：如果是 Category 类型，且其分类 ID (val) 等于大类 ID (pid)，说明是大类本身，不应入库。
		if tagType == "Category" && val == fmt.Sprint(pid) {
			return
		}

		// 年份合法性校验
		if tagType == "Year" {
			if y, _ := strconv.Atoi(v); y <= 0 {
				return
			}
		}

		db.Mdb.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "pid"}, {Name: "tag_type"}, {Name: "value"}},
			DoUpdates: clause.Assignments(map[string]any{
				"score": gorm.Expr("score + 1"),
				"name":  v, // 同时更新 Name，确保展示名称实时同步
			}),
		}).Create(&model.SearchTagItem{Pid: pid, TagType: tagType, Name: v, Value: val, Score: 1})
	}

	for _, t := range parts {
		if tagType == "Category" && len(customValues) > 0 {
			upsert(t, customValues[0])
		} else {
			upsert(t)
		}
	}
}

// ========= Queries =========

func ExistSearchInfo(mid int64) bool {
	var count int64
	db.Mdb.Model(&model.SearchInfo{}).Where("mid", mid).Count(&count)
	return count > 0
}

func resolveFallbackCid(pid int64, cName string) int64 {
	if pid <= 0 {
		return 0
	}
	cName = strings.TrimSpace(cName)
	if cName == "" {
		return 0
	}
	if cName == GetMainCategoryName(pid) {
		return 0
	}

	var sub model.Category
	if err := db.Mdb.Where("pid = ? AND name = ?", pid, cName).First(&sub).Error; err == nil && sub.Id > 0 {
		return sub.Id
	}

	sub = model.Category{Pid: pid, Name: cName, StableKey: buildCategoryStableKey(pid, cName), Show: true}
	if err := db.Mdb.Where("pid = ? AND name = ?", pid, cName).FirstOrCreate(&sub).Error; err == nil && sub.Id > 0 {
		if sub.StableKey == "" {
			db.Mdb.Model(&model.Category{}).Where("id = ?", sub.Id).Update("stable_key", buildCategoryStableKey(pid, sub.Name))
		}
		MarkCategoryChanged()
		return sub.Id
	}
	return 0
}

type resolvedSearchCategory struct {
	Pid   int64
	Cid   int64
	CName string
	PKey  string
	CKey  string
}

type normalizedSearchMeta struct {
	Score       float64
	UpdateStamp int64
	Year        int64
	Area        string
	Language    string
	ClassTag    string
}

func resolveSearchCategory(sourceId string, detail model.MovieDetail) resolvedSearchCategory {
	result := resolvedSearchCategory{
		CName: detail.CName,
	}

	// 1. 优先映射子类 ID
	result.Cid = GetLocalCategoryId(sourceId, detail.Cid)
	if result.Cid > 0 {
		// 如果子类映射成功，直接从本地缓存获取其根类 ID。
		// 这比依赖采集站详情里的 detail.Pid 更可靠，因为部分源详情里 TypeID1 为 0。
		result.Pid = GetRootId(result.Cid)
	}

	// 2. 如果通过子类没拿到 Pid，尝试映射源端大类 ID
	if result.Pid == 0 {
		result.Pid = GetRootId(GetLocalCategoryId(sourceId, detail.Pid))
	}

	// 3. 最终兜底：如果还是没拿到，根据名称推断根类
	if result.Pid == 0 {
		standardName := GetCategoryBucketRole(detail.CName)
		result.Pid = GetStandardIdByRole(standardName)
	}

	// 4. 仅尝试匹配已有本地子类，不在热路径建类
	if result.Cid == 0 {
		result.Cid = resolveFallbackCid(result.Pid, detail.CName)
	}
	if result.Pid > 0 {
		result.PKey = GetCategoryStableKeyByID(result.Pid)
	}
	if result.Cid > 0 {
		result.CKey = GetCategoryStableKeyByID(result.Cid)
	}

	return result
}

func normalizeSearchMetadata(detail model.MovieDetail, category resolvedSearchCategory) normalizedSearchMeta {
	score, _ := strconv.ParseFloat(detail.DbScore, 64)
	stamp, _ := time.ParseInLocation(time.DateTime, detail.UpdateTime, time.Local)
	year, err := strconv.ParseInt(regexp.MustCompile(`[1-9][0-9]{3}`).FindString(detail.ReleaseDate), 10, 64)
	if err != nil {
		year = 0
	}

	finalArea := NormalizeArea(detail.Area)
	finalLang := NormalizeLanguage(detail.Language)
	mainCategoryName := GetMainCategoryName(category.Pid)

	return normalizedSearchMeta{
		Score:       score,
		UpdateStamp: stamp.Unix(),
		Year:        year,
		Area:        finalArea,
		Language:    finalLang,
		ClassTag:    CleanPlotTags(detail.ClassTag, finalArea, mainCategoryName, category.CName),
	}
}

func buildSearchInfo(sourceId string, detail model.MovieDetail, category resolvedSearchCategory, meta normalizedSearchMeta) model.SearchInfo {
	return model.SearchInfo{
		Mid:               detail.Id,
		ContentKey:        buildContentKey(detail),
		SourceId:          sourceId,
		Cid:               category.Cid,
		Pid:               category.Pid,
		RootCategoryKey:   category.PKey,
		CategoryKey:       category.CKey,
		Name:              detail.Name,
		SubTitle:          detail.SubTitle,
		CName:             category.CName,
		ClassTag:          meta.ClassTag,
		Area:              meta.Area,
		Language:          meta.Language,
		Year:              meta.Year,
		Initial:           detail.Initial,
		Score:             meta.Score,
		Hits:              detail.Hits,
		UpdateStamp:       meta.UpdateStamp,
		LatestSourceStamp: meta.UpdateStamp,
		DbId:              detail.DbId,
		State:             detail.State,
		Remarks:           detail.Remarks,
		ReleaseStamp:      detail.AddTime,
		Picture:           detail.Picture,
		Actor:             detail.Actor,
		Director:          detail.Director,
		Blurb:             detail.Blurb,
	}
}

func ConvertSearchInfo(sourceId string, detail model.MovieDetail) model.SearchInfo {
	category := resolveSearchCategory(sourceId, detail)
	meta := normalizeSearchMetadata(detail, category)
	return buildSearchInfo(sourceId, detail, category, meta)
}

func buildContentKey(detail model.MovieDetail) string {
	// 生成内容指纹：优先使用豆瓣 ID，无豆瓣 ID 则使用名称哈希
	if detail.DbId != 0 {
		return fmt.Sprintf("dbid_%d", detail.DbId)
	}
	return fmt.Sprintf("name_%s", utils.GenerateHashKey(detail.Name))
}

func applyResolvedCategory(detail *model.MovieDetail, info model.SearchInfo) {
	if detail == nil {
		return
	}
	detail.Pid = info.Pid
	detail.Cid = info.Cid
}

func ApplyCategoryFilter(query *gorm.DB, pid int64, cid int64) *gorm.DB {
	isUncategorized := cid == model.TagUncategorizedValue
	pid = ResolveCategoryID(pid)
	if cid > 0 {
		cid = ResolveCategoryID(cid)
	}
	switch {
	case isUncategorized && pid > 0:
		return query.Where("pid = ? AND cid = 0", pid)
	case cid > 0 && IsRootCategory(cid):
		return query.Where("pid = ?", cid)
	case cid > 0:
		return query.Where("cid = ?", cid)
	case pid > 0:
		return query.Where("pid = ?", pid)
	default:
		return query
	}
}

// GetMovieListByPid 获取指定父类 ID 的影片基本信息
func GetMovieListByPid(pid int64, page *dto.Page) []model.MovieBasicInfo {
	pid = ResolveCategoryID(pid)
	var count int64
	db.Mdb.Model(&model.SearchInfo{}).Where("pid = ?", pid).Count(&count)
	page.Total = int(count)
	page.PageCount = int((page.Total + page.PageSize - 1) / page.PageSize)

	return GetMovieListByPidLimit(pid, page.PageSize, (page.Current-1)*page.PageSize)
}

// GetMovieListByPidLimit 轻量级获取指定父类 ID 列表 (无 Count)
func GetMovieListByPidLimit(pid int64, limit, offset int) []model.MovieBasicInfo {
	pid = ResolveCategoryID(pid)
	var s []model.SearchInfo
	if err := db.Mdb.Limit(limit).Offset(offset).Where("pid = ?", pid).Order(latestUpdateOrderSQL).Find(&s).Error; err != nil {
		log.Printf("GetMovieListByPidLimit Error: %v", err)
		return nil
	}

	return GetBasicInfoBySearchInfos(s...)
}

// GetMovieListByCid 获取指定子类 ID 的影片基本信息
func GetMovieListByCid(cid int64, page *dto.Page) []model.MovieBasicInfo {
	cid = ResolveCategoryID(cid)
	var count int64
	db.Mdb.Model(&model.SearchInfo{}).Where("cid = ?", cid).Count(&count)
	page.Total = int(count)
	page.PageCount = int((page.Total + page.PageSize - 1) / page.PageSize)

	return GetMovieListByCidLimit(cid, page.PageSize, (page.Current-1)*page.PageSize)
}

// GetMovieListByCidLimit 轻量级获取指定子类 ID 列表 (无 Count)
func GetMovieListByCidLimit(cid int64, limit, offset int) []model.MovieBasicInfo {
	cid = ResolveCategoryID(cid)
	var s []model.SearchInfo
	if err := db.Mdb.Limit(limit).Offset(offset).Where("cid = ?", cid).Order(latestUpdateOrderSQL).Find(&s).Error; err != nil {
		log.Printf("GetMovieListByCidLimit Error: %v", err)
		return nil
	}

	return GetBasicInfoBySearchInfos(s...)
}

func SearchFilmKeyword(keyword string, page *dto.Page) []model.SearchInfo {
	var searchList []model.SearchInfo
	var count int64
	db.Mdb.Model(&model.SearchInfo{}).Where("name LIKE ?", fmt.Sprint(`%`, keyword, `%`)).Or("sub_title LIKE ?", fmt.Sprint(`%`, keyword, `%`)).Count(&count)
	page.Total = int(count)
	page.PageCount = int((page.Total + page.PageSize - 1) / page.PageSize)

	db.Mdb.Limit(page.PageSize).Offset((page.Current-1)*page.PageSize).
		Where("name LIKE ?", fmt.Sprintf(`%%%s%%`, keyword)).Or("sub_title LIKE ?", fmt.Sprintf(`%%%s%%`, keyword)).Order("year DESC, " + latestUpdateOrderSQL).Find(&searchList)

	return searchList
}

func GetRelateMovieBasicInfo(search model.SearchInfo, page *dto.Page) []model.MovieBasicInfo {
	offset := page.Current
	if offset <= 0 {
		offset = 0
	} else {
		offset = (offset - 1) * page.PageSize
	}
	// 1. 基于分词 (Tokenization) 的核心特征字提取
	rawName := search.Name
	// 定义常用切割符 (如遇到冒号、破折号、空格、括号、特定关键字等，即认为其前部为核心标题)
	delimiters := []string{"：", ":", "·", " - ", "—", " ", "（", "(", "[", "【", "第", "剧场版", "部", "季", "之"}
	coreToken := rawName

	// 寻找最早出现的切割符位置
	minIdx := len(rawName)
	for _, d := range delimiters {
		if idx := strings.Index(rawName, d); idx > 0 && idx < minIdx {
			minIdx = idx
		}
	}

	if minIdx < len(rawName) {
		coreToken = rawName[:minIdx]
	}

	coreToken = strings.TrimSpace(coreToken)

	// 最小长度保障：若因连续符号导致拆分后无词，回退到原名前4个字符
	if len([]rune(coreToken)) < 1 && len([]rune(rawName)) > 2 {
		coreToken = string([]rune(rawName)[:4])
	}

	// 如果拆分出来的 coreToken 只有 1 个字符（例如中文单字），可能区分度不够，
	// 回退到至少取两个字符（如果有的话）。
	if len([]rune(coreToken)) == 1 && len([]rune(rawName)) > 1 {
		coreToken = string([]rune(rawName)[:2])
	}

	nameLike := fmt.Sprintf("%%%s%%", coreToken)
	prefixLike := fmt.Sprintf("%s%%", coreToken)

	// 2. 构造查询条件
	// 基础池：扩大候选集（名称包含核心词，或者标签相似）
	condition := db.Mdb.Where("name LIKE ? OR sub_title LIKE ?", nameLike, nameLike)

	search.ClassTag = strings.ReplaceAll(search.ClassTag, " ", "")
	classTags := make([]string, 0)
	if strings.Contains(search.ClassTag, ",") {
		classTags = strings.Split(search.ClassTag, ",")
	} else if strings.Contains(search.ClassTag, "/") {
		classTags = strings.Split(search.ClassTag, "/")
	} else if strings.TrimSpace(search.ClassTag) != "" {
		classTags = []string{search.ClassTag}
	}

	for _, tag := range classTags {
		tag = strings.TrimSpace(tag)
		if tag == "" {
			continue
		}
		condition = condition.Or("class_tag LIKE ?", fmt.Sprintf("%%%s%%", tag))
	}

	// 3. 执行带高阶权重的原生 SQL 排序查询 (彻底解决 Gorm 链式参数错位 Bug)
	var list []model.SearchInfo
	args := []interface{}{search.Pid, search.Mid}

	// 构建 WHERE 子句
	whereSQL := "WHERE pid = ? AND mid != ? AND deleted_at IS NULL"
	whereSQL += " AND ((name LIKE ? OR sub_title LIKE ?)"
	args = append(args, nameLike, nameLike)

	tags := strings.Split(search.ClassTag, ",")
	for _, t := range tags {
		if tag := strings.TrimSpace(t); tag != "" {
			whereSQL += " OR class_tag LIKE ?"
			args = append(args, "%"+tag+"%")
		}
	}
	whereSQL += ")"

	// 构建 ORDER BY 子句
	sortSQL := `ORDER BY 
		(name = ?) DESC, 
		(name LIKE ?) DESC, 
		(name LIKE ?) DESC`
	args = append(args, coreToken, prefixLike, nameLike)
	if search.Cid > 0 {
		sortSQL += `,
		(cid = ?) DESC`
		args = append(args, search.Cid)
	}
	sortSQL += `,
		update_stamp DESC`

	// 最终 SQL 组装 (不使用 Gorm Builder 而是手动注入参数列表)
	finalSQL := fmt.Sprintf("SELECT * FROM search_info %s %s LIMIT ? OFFSET ?", whereSQL, sortSQL)
	args = append(args, page.PageSize, offset)

	if err := db.Mdb.Raw(finalSQL, args...).Scan(&list).Error; err != nil {
		log.Println("GetRelateMovieBasicInfo Raw SQL Error:", err)
		return make([]model.MovieBasicInfo, 0)
	}

	// 4. 重大兜底机制：如果没有匹配到任何相关结果 (Cid/Name/Tags 全落空)，推荐同二级分类的最新影片
	if len(list) == 0 && search.Cid > 0 {
		db.Mdb.Model(&model.SearchInfo{}).
			Where("cid = ? AND mid != ?", search.Cid, search.Mid).
			Order(latestUpdateOrderSQL).
			Offset(offset).Limit(page.PageSize).
			Find(&list)
	}

	basicList := make([]model.MovieBasicInfo, 0)
	for _, s := range list {
		basicList = append(basicList, GetBasicInfoByKey(s.Cid, s.Mid))
	}
	return basicList
}

// GetBasicInfoByKey 获取影片的基本信息
func GetBasicInfoByKey(cid int64, mid int64) model.MovieBasicInfo {
	var info model.MovieDetailInfo
	if err := db.Mdb.Where("mid = ?", mid).First(&info).Error; err == nil {
		var detail model.MovieDetail
		_ = json.Unmarshal([]byte(info.Content), &detail)
		return model.MovieBasicInfo{
			Id: detail.Id, Cid: detail.Cid, Pid: detail.Pid, Name: detail.Name,
			SubTitle: detail.SubTitle, CName: detail.CName, State: detail.State,
			Picture: detail.Picture, Actor: detail.Actor, Director: detail.Director,
			Blurb: detail.Blurb, Remarks: detail.Remarks, Area: detail.Area, Year: detail.Year,
		}
	}
	return model.MovieBasicInfo{}
}

// GetMovieDetail 获取影片详情信息
func GetMovieDetail(cid int64, mid int64) *model.MovieDetail {
	var movieDetailInfo model.MovieDetailInfo
	if err := db.Mdb.Where("mid = ?", mid).First(&movieDetailInfo).Error; err != nil {
		log.Printf("GetMovieDetail Error: %v", err)
		return nil
	}
	var detail model.MovieDetail
	if err := json.Unmarshal([]byte(movieDetailInfo.Content), &detail); err != nil {
		log.Printf("Unmarshal MovieDetail Error: %v", err)
		return nil
	}

	// 统一将 nil slice 初始化为空 slice，保证前端始终收到 [] 而非 null
	if detail.PlayFrom == nil {
		detail.PlayFrom = []string{}
	}
	if detail.PlayList == nil {
		detail.PlayList = [][]model.MovieUrlInfo{}
	} else {
		for i, inner := range detail.PlayList {
			if inner == nil {
				detail.PlayList[i] = []model.MovieUrlInfo{}
			}
		}
	}
	if detail.DownloadList == nil {
		detail.DownloadList = [][]model.MovieUrlInfo{}
	} else {
		for i, inner := range detail.DownloadList {
			if inner == nil {
				detail.DownloadList[i] = []model.MovieUrlInfo{}
			}
		}
	}
	return &detail
}

func GetMovieDetailByDBID(mid int64, name string) []model.MoviePlaySource {
	var mps []model.MoviePlaySource
	sources := GetCollectSourceList()
	for _, s := range sources {
		if s.Grade == model.SlaveCollect && s.State {
			var playlist model.MoviePlaylist
			key := utils.GenerateHashKey(mid)
			if mid == 0 {
				key = utils.GenerateHashKey(name)
			}
			if err := db.Mdb.Where("source_id = ? AND movie_key = ?", s.Id, key).First(&playlist).Error; err != nil && mid != 0 {
				db.Mdb.Where("source_id = ? AND movie_key = ?", s.Id, utils.GenerateHashKey(name)).First(&playlist)
			}
			if playlist.ID > 0 {
				var playLists [][]model.MovieUrlInfo
				if jsonErr := json.Unmarshal([]byte(playlist.Content), &playLists); jsonErr == nil {
					for _, pl := range playLists {
						if len(pl) > 0 {
							mps = append(mps, model.MoviePlaySource{SiteName: s.Name, PlayList: pl})
						}
					}
				}
			}
		}
	}
	return mps
}

func GetTagsByTitle(pid int64, tagType string, stickyValue string) []map[string]string {
	pid = ResolveCategoryID(pid)
	var tags []string
	var items []model.SearchTagItem

	db.Mdb.Where("pid = ? AND tag_type = ? AND score > 5", pid, tagType).
		Order("score DESC").Limit(30).Find(&items)

	for _, item := range items {
		tags = append(tags, fmt.Sprintf("%s:%s", item.Name, item.Value))
	}

	if len(tags) == 0 && tagType == "Sort" {
		tags = defaultSortTagStrings
	}
	return HandleTagStr(tagType, stickyValue, true, tags...)
}

func GetTopTagValues(pid int64, tagType string) []string {
	pid = ResolveCategoryID(pid)
	var vals []string
	db.Mdb.Model(&model.SearchTagItem{}).
		Select("value").
		Where("pid = ? AND tag_type = ? AND score >= 5", pid, tagType).
		Order("score DESC").
		Limit(12).
		Find(&vals)
	return vals
}

func HandleTagStr(title string, stickyValue string, withAll bool, tags ...string) []map[string]string {
	list := make([]map[string]string, 0)

	// 1. 添加“全部”选项 (除排序外)
	if withAll && !strings.EqualFold(title, "Sort") {
		list = append(list, map[string]string{"Name": "全部", "Value": ""})
	}

	// 2. 添加实际业务标签
	for _, t := range tags {
		if sl := strings.Split(t, ":"); len(sl) > 1 {
			list = append(list, map[string]string{"Name": sl[0], "Value": sl[1]})
		}
	}

	return list
}

func appendSearchOption(options []map[string]string, option map[string]string) []map[string]string {
	if option == nil {
		return options
	}
	for _, item := range options {
		if item["Value"] == option["Value"] {
			return options
		}
	}
	return append(options, option)
}

func buildSearchTagCacheKey(st model.SearchTagsVO) string {
	st = normalizeSearchTagsVO(st)
	return fmt.Sprintf("%s:%d:%d:%s:%s:%s:%s",
		config.SearchTags,
		st.Pid, st.Cid,
		st.Area, st.Language, st.Year, st.Plot,
	)
}

func normalizeSearchTagsVO(st model.SearchTagsVO) model.SearchTagsVO {
	st.Pid = ResolveCategoryID(st.Pid)
	if st.Cid > 0 {
		st.Cid = ResolveCategoryID(st.Cid)
	}
	return st
}

func loadSearchTagItemsByType(pid int64) map[string][]model.SearchTagItem {
	pid = ResolveCategoryID(pid)
	var allItems []model.SearchTagItem
	db.Mdb.Where("pid = ? AND score > 0", pid).Order("score DESC").Find(&allItems)

	itemsByType := make(map[string][]model.SearchTagItem)
	for _, item := range allItems {
		itemsByType[item.TagType] = append(itemsByType[item.TagType], item)
	}
	return itemsByType
}

func getStickySearchTagValue(st model.SearchTagsVO, tagType string) string {
	switch tagType {
	case "Category":
		return fmt.Sprint(st.Cid)
	case "Plot":
		return st.Plot
	case "Area":
		return st.Area
	case "Language":
		return st.Language
	case "Year":
		return st.Year
	default:
		return ""
	}
}

func appendStickySearchTag(items []model.SearchTagItem, sticky string, topCount int) []model.SearchTagItem {
	if sticky == "" || sticky == model.TagOthersValue || len(items) <= topCount {
		if topCount > len(items) {
			topCount = len(items)
		}
		return items[:topCount]
	}

	displayItems := items[:topCount]
	for _, item := range displayItems {
		if item.Value == sticky {
			return displayItems
		}
	}
	for _, item := range items[topCount:] {
		if item.Value == sticky {
			return append(displayItems, item)
		}
	}
	return displayItems
}

func formatSearchTagItems(tagType string, items []model.SearchTagItem, sticky string) []map[string]string {
	topCount := 12
	if len(items) < topCount {
		topCount = len(items)
	}
	displayItems := appendStickySearchTag(items, sticky, topCount)
	hasMore := len(items) > 12

	tagStrs := make([]string, 0, len(displayItems))
	for _, item := range displayItems {
		tagStrs = append(tagStrs, fmt.Sprintf("%s:%s", item.Name, item.Value))
	}

	formatted := HandleTagStr(tagType, sticky, true, tagStrs...)
	if hasMore {
		formatted = append(formatted, map[string]string{"Name": model.TagOthersName, "Value": model.TagOthersValue})
	}
	return formatted
}

func hasUncategorizedSearchInfo(pid int64) bool {
	pid = ResolveCategoryID(pid)
	if pid <= 0 {
		return false
	}
	var count int64
	db.Mdb.Model(&model.SearchInfo{}).Where("pid = ? AND cid = 0", pid).Count(&count)
	return count > 0
}

func hasUnknownYearSearchInfo(pid int64) bool {
	pid = ResolveCategoryID(pid)
	if pid <= 0 {
		return false
	}
	var count int64
	db.Mdb.Model(&model.SearchInfo{}).Where("pid = ? AND year = 0", pid).Count(&count)
	return count > 0
}

func hasUnknownTextSearchInfo(pid int64, column string) bool {
	pid = ResolveCategoryID(pid)
	if pid <= 0 || column == "" {
		return false
	}
	var count int64
	db.Mdb.Model(&model.SearchInfo{}).Where(fmt.Sprintf("pid = ? AND (%s = '' OR %s IS NULL)", column, column), pid).Count(&count)
	return count > 0
}

func appendSpecialSearchOptions(tagType string, formatted []map[string]string, st model.SearchTagsVO) []map[string]string {
	switch tagType {
	case "Category":
		if hasUncategorizedSearchInfo(st.Pid) || st.Cid == model.TagUncategorizedValue {
			return appendSearchOption(formatted, map[string]string{
				"Name":  model.TagUncategorizedName,
				"Value": fmt.Sprint(model.TagUncategorizedValue),
			})
		}
	case "Year":
		if hasUnknownYearSearchInfo(st.Pid) || st.Year == model.TagUnknownValue {
			return appendSearchOption(formatted, map[string]string{
				"Name":  model.TagUnknownName,
				"Value": model.TagUnknownValue,
			})
		}
	case "Area":
		if hasUnknownTextSearchInfo(st.Pid, "area") || st.Area == model.TagUnknownValue {
			return appendSearchOption(formatted, map[string]string{
				"Name":  model.TagUnknownName,
				"Value": model.TagUnknownValue,
			})
		}
	case "Language":
		if hasUnknownTextSearchInfo(st.Pid, "language") || st.Language == model.TagUnknownValue {
			return appendSearchOption(formatted, map[string]string{
				"Name":  model.TagUnknownName,
				"Value": model.TagUnknownValue,
			})
		}
	case "Plot":
		if hasUnknownTextSearchInfo(st.Pid, "class_tag") || st.Plot == model.TagUnknownValue {
			return appendSearchOption(formatted, map[string]string{
				"Name":  model.TagUnknownName,
				"Value": model.TagUnknownValue,
			})
		}
	}
	return formatted
}

// GetSearchTag 获取搜索标签 (带联动感知与复合 Redis 缓存)
func GetSearchTag(st model.SearchTagsVO) map[string]any {
	st = normalizeSearchTagsVO(st)
	pid := st.Pid
	cacheKey := buildSearchTagCacheKey(st)

	// 尝试从 Redis 获取缓存
	if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
		var res map[string]any
		if json.Unmarshal([]byte(data), &res) == nil {
			return res
		}
	}

	res := make(map[string]any)
	tagTypes := []string{"Category", "Plot", "Area", "Language", "Year", "Sort"}
	res["titles"] = map[string]string{
		"Category": "类型",
		"Plot":     "剧情",
		"Area":     "地区",
		"Language": "语言",
		"Year":     "年份",
		"Sort":     "排序",
	}

	tagMap := make(map[string]any)
	activeSortList := make([]string, 0)

	itemsByType := loadSearchTagItemsByType(pid)

	for _, t := range tagTypes {
		items := itemsByType[t]

		if t == "Sort" {
			tagMap[t] = HandleTagStr(t, st.Sort, false, defaultSortTagStrings...)
			activeSortList = append(activeSortList, t)
			continue
		}

		if len(items) == 0 {
			if t == "Category" || t == "Year" || t == "Area" || t == "Language" || t == "Plot" {
				tagMap[t] = appendSpecialSearchOptions(t, HandleTagStr(t, getStickySearchTagValue(st, t), true), st)
				activeSortList = append(activeSortList, t)
			}
			continue
		}
		sticky := getStickySearchTagValue(st, t)
		tagMap[t] = appendSpecialSearchOptions(t, formatSearchTagItems(t, items, sticky), st)
		activeSortList = append(activeSortList, t)
	}
	res["sortList"] = activeSortList
	res["tags"] = tagMap

	if data, err := json.Marshal(res); err == nil {
		db.Rdb.Set(db.Cxt, cacheKey, string(data), time.Hour*2)
	}

	return res
}

func GetSearchOptions(st model.SearchTagsVO) map[string]any {
	st = normalizeSearchTagsVO(st)
	// 复用 GetSearchTag 的逻辑
	full := GetSearchTag(st)
	if tags, ok := full["tags"].(map[string]any); ok {
		// 返回业务需要的四个核心维度
		res := make(map[string]any)
		for _, t := range []string{"Plot", "Area", "Language", "Year"} {
			res[t] = tags[t]
		}
		return res
	}

	// 回退逻辑 (兜底)
	tagMap := make(map[string]any)
	for _, t := range []string{"Plot", "Area", "Language", "Year"} {
		tags := GetTagsByTitle(st.Pid, t, "")
		tagMap[t] = tags
	}
	return tagMap
}

func GetSearchPage(s model.SearchVo) []model.SearchInfo {
	query := db.Mdb.Model(&model.SearchInfo{})
	if s.Name != "" {
		query = query.Where("name LIKE ?", fmt.Sprintf("%%%s%%", s.Name))
	}

	// 严格精准分类过滤
	query = ApplyCategoryFilter(query, s.Pid, s.Cid)

	if s.Plot != "" {
		query = query.Where("class_tag LIKE ?", fmt.Sprintf("%%%s%%", s.Plot))
	}
	if s.Area != "" {
		query = query.Where("area = ?", s.Area)
	}
	if s.Language != "" {
		query = query.Where("language = ?", s.Language)
	}
	if s.Year > 0 {
		query = query.Where("year = ?", s.Year)
	}
	switch s.Remarks {
	case "完结":
		query = query.Where("remarks IN ?", []string{"完结", "HD"})
	case "":
	default:
		query = query.Not(map[string]any{"remarks": []string{"完结", "HD"}})
	}
	if s.BeginTime > 0 {
		query = query.Where("update_stamp >= ? ", s.BeginTime)
	}
	if s.EndTime > 0 {
		query = query.Where("update_stamp <= ? ", s.EndTime)
	}

	dto.GetPage(query, s.Paging)
	var sl []model.SearchInfo
	if err := query.Limit(s.Paging.PageSize).Offset((s.Paging.Current - 1) * s.Paging.PageSize).Find(&sl).Error; err != nil {
		log.Printf("GetSearchPage Error: %v", err)
		return nil
	}
	return sl
}

func GetSearchInfosByTags(st model.SearchTagsVO, page *dto.Page) []model.SearchInfo {
	st = normalizeSearchTagsVO(st)
	qw := db.Mdb.Model(&model.SearchInfo{})
	t := reflect.TypeFor[model.SearchTagsVO]()
	v := reflect.ValueOf(st)

	// 记录是否已经处理了分类过滤，防止 Pid 和 Cid 产生冲突
	categoryFiltered := false

	for i := 0; i < t.NumField(); i++ {
		value := v.Field(i).Interface()
		fieldName := t.Field(i).Name
		k := strings.ToLower(fieldName)

		if !dto.IsEmpty(value) {
			switch k {
			case "pid", "cid":
				if categoryFiltered {
					continue
				}
				// 严格逻辑与 GetSearchPage 保持一致
				qw = ApplyCategoryFilter(qw, st.Pid, st.Cid)
				categoryFiltered = true
			case "year":
				if vStr, ok := value.(string); ok {
					if vStr == model.TagOthersValue {
						// 聚合查询：查询不在前12个中的所有内容
						topVals := GetTopTagValues(st.Pid, fieldName)
						qw = qw.Where("year <> 0")
						if len(topVals) > 0 {
							qw = qw.Where(fmt.Sprintf("%s NOT IN ?", k), topVals)
						}
						break
					}
					if vStr == model.TagUnknownValue {
						qw = qw.Where("year = 0")
						break
					}
				}
				qw = qw.Where(fmt.Sprintf("%s = ?", k), value)
			case "area", "language":
				if vStr, ok := value.(string); ok {
					if vStr == model.TagOthersValue {
						// 聚合查询：查询不在前12个中的所有内容
						topVals := GetTopTagValues(st.Pid, fieldName)
						qw = qw.Where(fmt.Sprintf("%s <> ''", k))
						if len(topVals) > 0 {
							qw = qw.Where(fmt.Sprintf("%s NOT IN ?", k), topVals)
						}
						break
					}
					if vStr == model.TagUnknownValue {
						qw = qw.Where(fmt.Sprintf("(%s = '' OR %s IS NULL)", k, k))
						break
					}
				}
				qw = qw.Where(fmt.Sprintf("%s = ?", k), value)
			case "plot":
				if vStr, ok := value.(string); ok {
					if vStr == model.TagOthersValue {
						// 优化： consolidated NOT LIKE 查询，减少 SQL 复杂度
						// 获取热门标签列表，排除这些标签的剧情，同时排除未知剧情
						topVals := GetTopTagValues(st.Pid, fieldName)
						qw = qw.Where("class_tag <> ''")
						if len(topVals) > 0 {
							// 限制 NOT LIKE 子句数量 (最多 5 个),防止 SQL 过于复杂影响性能
							maxPlotExcludes := 5
							if len(topVals) < maxPlotExcludes {
								maxPlotExcludes = len(topVals)
							}
							for i := 0; i < maxPlotExcludes; i++ {
								qw = qw.Where("class_tag NOT LIKE ?", fmt.Sprintf("%%%v%%", topVals[i]))
							}
						}
						break
					}
					if vStr == model.TagUnknownValue {
						qw = qw.Where("(class_tag = '' OR class_tag IS NULL)")
						break
					}
				}
				// 普通剧情标签使用 LIKE 查询
				qw = qw.Where("class_tag LIKE ?", fmt.Sprintf("%%%v%%", value))
			case "sort":
				sVal, ok := value.(string)
				if !ok {
					break
				}
				column, allowed := allowedSearchSortColumns[sVal]
				if !allowed {
					column = allowedSearchSortColumns["latest_source_stamp"]
				}
				if strings.EqualFold(column, "release_stamp") {
					qw.Order("year DESC, release_stamp DESC")
				} else {
					qw.Order(fmt.Sprintf("%s DESC", column))
				}
			default:
				break
			}
		}
	}

	dto.GetPage(qw, page)
	var sl []model.SearchInfo
	if err := qw.Limit(page.PageSize).Offset((page.Current - 1) * page.PageSize).Find(&sl).Error; err != nil {
		log.Printf("GetSearchInfosByTags Error: %v", err)
		return nil
	}
	return sl
}

func GetSearchInfoById(id int64) *model.SearchInfo {
	s := model.SearchInfo{}
	if err := db.Mdb.Where("mid = ?", id).First(&s).Error; err != nil {
		return nil
	}
	return &s
}

func DelFilmSearch(id int64) error {
	// 获取记录所在分类，以便后续清除缓存
	info := GetSearchInfoById(id)

	// 开启事务保证清理的一致性
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		// 1. 删除检索信息
		if err := tx.Where("mid = ?", id).Delete(&model.SearchInfo{}).Error; err != nil {
			return err
		}
		// 2. 删除主站详情记录
		if err := tx.Where("mid = ?", id).Delete(&model.MovieDetailInfo{}).Error; err != nil {
			return err
		}
		// 3. 删除来源映射记录
		if err := tx.Where("global_mid = ?", id).Delete(&model.MovieSourceMapping{}).Error; err != nil {
			return err
		}
		// 4. 删除相关的 Banner (横幅)
		if err := tx.Where("mid = ?", id).Delete(&model.Banner{}).Error; err != nil {
			return err
		}
		return nil
	})

	// 清除对应分类的搜索标签缓存
	if err == nil && info != nil {
		ClearSearchTagsCache(info.Pid)
	}

	return err
}

func ShieldFilmSearch(cid int64) error {
	// 获取相关 MID 列表以便从关系表中删除
	var mids []int64
	db.Mdb.Model(&model.SearchInfo{}).Where("cid = ?", cid).Pluck("mid", &mids)

	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		// 1. 软删除检索信息
		if err := tx.Where("cid = ?", cid).Delete(&model.SearchInfo{}).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Printf("ShieldFilmSearch Error: %v", err)
		return err
	}

	// 清除对应 Pid 的搜索标签缓存
	if pId := GetParentId(cid); pId > 0 {
		ClearSearchTagsCache(pId)
	}
	return nil
}

func RecoverFilmSearch(cid int64) error {
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		// 1. 恢复检索信息
		if err := tx.Model(&model.SearchInfo{}).Unscoped().Where("cid = ?", cid).Update("deleted_at", nil).Error; err != nil {
			return err
		}
		return nil
	})
	if err != nil {
		log.Printf("RecoverFilmSearch Error: %v", err)
		return err
	}

	// 清除对应 Pid 的搜索标签缓存
	if pId := GetParentId(cid); pId > 0 {
		ClearSearchTagsCache(pId)
	}
	return nil
}

// ClearSearchTagsCache 清除特定分类的所有复合搜索标签缓存
func ClearSearchTagsCache(pid int64) {
	// 使用通配符前缀：{Search:Tags}:{pid}:*
	pattern := fmt.Sprintf("%s:%d:*", config.SearchTags, pid)
	ctx := db.Cxt
	iter := db.Rdb.Scan(ctx, 0, pattern, config.MaxScanCount).Iterator()
	for iter.Next(ctx) {
		db.Rdb.Del(ctx, iter.Val())
	}
	// 同时兼容旧版/基础版 key: {Search:Tags}:{pid}
	db.Rdb.Del(ctx, fmt.Sprintf("%s:%d", config.SearchTags, pid))
}

// ClearTVBoxConfigCache 清除 TVBox 配置缓存
func ClearTVBoxConfigCache() {
	db.Rdb.Del(db.Cxt, config.TVBoxConfigCacheKey)
}

func ClearTVBoxListCache() {
	pattern := config.TVBoxList + ":*"
	iter := db.Rdb.Scan(db.Cxt, 0, pattern, config.MaxScanCount).Iterator()
	for iter.Next(db.Cxt) {
		db.Rdb.Del(db.Cxt, iter.Val())
	}
}

// ClearAllSearchTagsCache 清除所有分类的搜索标签缓存 (扫描清理)
func ClearAllSearchTagsCache() {
	// 基于 config 里的模板生成通配符，防止硬编码 prefix 不一致
	pattern := config.SearchTags + ":*"
	keys, err := db.Rdb.Keys(db.Cxt, pattern).Result()
	if err == nil && len(keys) > 0 {
		db.Rdb.Del(db.Cxt, keys...)
	}
	ClearTVBoxConfigCache()
}

// FilmZero 删除所有库存数据 (包含 MySQL 持久化表)
func FilmZero() {
	// 清理 MySQL
	tables := []string{
		model.TableMovieDetail,
		model.TableSearchInfo,
		model.TableMoviePlaylist,
		model.TableCategory,
		model.TableVirtualPicture,
		model.TableSearchTag,
	}
	for _, t := range tables {
		db.Mdb.Exec(fmt.Sprintf("TRUNCATE table %s", t))
	}
	db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.MovieSourceMapping{})
	db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.CategoryMapping{})
	time.Sleep(100 * time.Millisecond)

	// 3. 同步清理采集失败记录，确保彻底清空
	TruncateRecordTable()

	// 4. 清除所有 Redis 缓存
	ClearCategoryCache()
	ClearIndexPageCache()
	db.Rdb.Del(db.Cxt, config.VirtualPictureKey)
	ClearTVBoxListCache()
	InitMappingEngine()
}

// MasterFilmZero 仅清理主站相关数据 (search_infos / movie_detail_infos / category)
// 保留附属站 movie_playlists 数据，用于主站切换时防止附属站数据丢失
func MasterFilmZero() {
	tables := []string{
		model.TableSearchInfo,
		model.TableMovieDetail,
		model.TableCategory,
		model.TableVirtualPicture,
		model.TableSearchTag,
	}
	for _, t := range tables {
		db.Mdb.Exec(fmt.Sprintf("TRUNCATE table %s", t))
	}
	db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.MovieSourceMapping{})
	db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.CategoryMapping{})
	time.Sleep(100 * time.Millisecond)

	// 清除所有 Redis 缓存
	ClearCategoryCache()
	ClearIndexPageCache()
	db.Rdb.Del(db.Cxt, config.VirtualPictureKey)
	ClearTVBoxListCache()
	InitMappingEngine()
}

// CleanEmptyFilms 清理所有片名为空或无法识别大类(Pid=0)的垃圾记录
func CleanEmptyFilms() int64 {
	var infos []model.SearchInfo
	// 同时清理无名影片和 Pid=0 的无法映射垃圾数据
	db.Mdb.Where("name = ? OR name IS NULL OR pid = 0", "").Find(&infos)
	if len(infos) == 0 {
		return 0
	}
	for _, info := range infos {
		_ = DelFilmSearch(info.Mid)
		ClearSearchTagsCache(info.Pid)
	}
	// DelFilmSearch 内部会精确清除对应的 Pid 缓存
	return int64(len(infos))
}

// CleanOrphanPlaylists 清理 movie_playlists 中与 search_infos 不匹配的孤儿记录
// 仅当 search_infos 存在数据时执行，避免主站清空后误删全部播放列表
func CleanOrphanPlaylists() int64 {
	// 1. 取出所有主站影片
	var films []struct {
		Name string
		DbId int64
	}
	db.Mdb.Model(&model.SearchInfo{}).Select("name", "db_id").Scan(&films)
	if len(films) == 0 {
		log.Println("[CleanOrphan] search_infos 为空，跳过孤儿清理")
		return 0
	}

	// 2. 生成有效 movie_key 集合
	validKeys := make(map[string]struct{}, len(films)*4)
	for _, f := range films {
		for _, c := range utils.NormalizeTitleCandidates(f.Name) {
			validKeys[utils.GenerateHashKey(c)] = struct{}{}
		}
		// 基于豆瓣ID的哈希 (如果存在)
		if f.DbId != 0 {
			validKeys[utils.GenerateHashKey(f.DbId)] = struct{}{}
		}
	}

	// 3. 取出 movie_playlists 中所有 movie_key
	var allKeys []string
	db.Mdb.Model(&model.MoviePlaylist{}).Distinct().Pluck("movie_key", &allKeys)

	// 4. 找出孤儿 key（不在 validKeys 集合中）
	var orphanKeys []string
	for _, key := range allKeys {
		if _, ok := validKeys[key]; !ok {
			orphanKeys = append(orphanKeys, key)
		}
	}

	if len(orphanKeys) == 0 {
		log.Println("[CleanOrphan] movie_playlists 无孤儿记录")
		return 0
	}

	// 5. 分批删除孤儿记录，避免 IN 参数过长
	const batchSize = 1000
	var total int64
	for i := 0; i < len(orphanKeys); i += batchSize {
		end := i + batchSize
		if end > len(orphanKeys) {
			end = len(orphanKeys)
		}
		result := db.Mdb.Where("movie_key IN ?", orphanKeys[i:end]).Delete(&model.MoviePlaylist{})
		total += result.RowsAffected
	}
	log.Printf("[CleanOrphan] 已清理 %d 条孤儿 movie_playlists 记录\n", total)
	return total
}

// GetHotMovieByPid 获取当前级分类下的热门影片
func GetHotMovieByPid(pid int64, page *dto.Page) []model.SearchInfo {
	return GetHotMovieByPidLimit(pid, page.PageSize, (page.Current-1)*page.PageSize)
}

// GetHotMovieByPidLimit 轻量级获取热门影片
func GetHotMovieByPidLimit(pid int64, limit, offset int) []model.SearchInfo {
	pid = ResolveCategoryID(pid)
	var s []model.SearchInfo
	t := time.Now().AddDate(0, -1, 0).Unix()
	if err := db.Mdb.Limit(limit).Offset(offset).Where("pid = ? AND update_stamp > ?", pid, t).Order(" year DESC, hits DESC").Find(&s).Error; err != nil {
		log.Printf("GetHotMovieByPidLimit Error: %v", err)
		return nil
	}
	return s
}

// GetHotMovieByCid 获取当前分类下的热门影片
func GetHotMovieByCid(cid int64, page *dto.Page) []model.SearchInfo {
	return GetHotMovieByCidLimit(cid, page.PageSize, (page.Current-1)*page.PageSize)
}

// GetHotMovieByCidLimit 轻量级获取热门影片
func GetHotMovieByCidLimit(cid int64, limit, offset int) []model.SearchInfo {
	cid = ResolveCategoryID(cid)
	var s []model.SearchInfo
	t := time.Now().AddDate(0, -1, 0).Unix()
	if err := db.Mdb.Limit(limit).Offset(offset).Where("cid = ? AND update_stamp > ?", cid, t).Order(" year DESC, hits DESC").Find(&s).Error; err != nil {
		log.Printf("GetHotMovieByCidLimit Error: %v", err)
		return nil
	}
	return s
}

// GetMultiplePlay 通过影片名 hash 值匹配播放源
func GetMultiplePlay(siteId, key string) []model.MovieUrlInfo {
	return GetMultiplePlayByKeys(siteId, []string{key})
}

// GetMultiplePlayByKeys 按优先级批量匹配播放源，返回首个命中的播放列表
func GetMultiplePlayByKeys(siteId string, keys []string) []model.MovieUrlInfo {
	orderedKeys := make([]string, 0, len(keys))
	seen := make(map[string]struct{}, len(keys))
	for _, k := range keys {
		k = strings.TrimSpace(k)
		if k == "" {
			continue
		}
		if _, ok := seen[k]; ok {
			continue
		}
		seen[k] = struct{}{}
		orderedKeys = append(orderedKeys, k)
	}
	if siteId == "" || len(orderedKeys) == 0 {
		return nil
	}

	var playlists []model.MoviePlaylist
	if err := db.Mdb.
		Where("source_id = ? AND movie_key IN ?", siteId, orderedKeys).
		Find(&playlists).Error; err != nil {
		return nil
	}
	if len(playlists) == 0 {
		return nil
	}

	contentByKey := make(map[string]string, len(playlists))
	for _, p := range playlists {
		contentByKey[p.MovieKey] = p.Content
	}

	var playlist model.MoviePlaylist
	var playList []model.MovieUrlInfo
	for _, k := range orderedKeys {
		content, ok := contentByKey[k]
		if !ok || content == "" {
			continue
		}
		playlist.Content = content
		var allPlayList [][]model.MovieUrlInfo
		if err := json.Unmarshal([]byte(playlist.Content), &allPlayList); err == nil && len(allPlayList) > 0 && len(allPlayList[0]) > 0 {
			playList = allPlayList[0]
			break
		}
	}
	return playList
}

// GetBasicInfoBySearchInfos 通过 searchInfo 获取影片的基本信息
func GetBasicInfoBySearchInfos(infos ...model.SearchInfo) []model.MovieBasicInfo {
	var list []model.MovieBasicInfo
	for _, s := range infos {
		list = append(list, model.MovieBasicInfo{
			Id:       s.Mid,
			Cid:      s.Cid,
			Pid:      s.Pid,
			Name:     s.Name,
			SubTitle: s.SubTitle,
			CName:    s.CName,
			State:    s.State,
			Picture:  s.Picture,
			Actor:    s.Actor,
			Director: s.Director,
			Blurb:    s.Blurb,
			Remarks:  s.Remarks,
			Area:     s.Area,
			Year:     fmt.Sprint(s.Year),
		})
	}
	return list
}

// GetMovieListBySort 通过排序类型返回对应的影片基本信息
func GetMovieListBySort(t int, pid int64, page *dto.Page) []model.MovieBasicInfo {
	pid = ResolveCategoryID(pid)
	var sl []model.SearchInfo
	qw := db.Mdb.Model(&model.SearchInfo{}).Where("pid = ?", pid).Limit(page.PageSize).Offset((page.Current - 1) * page.PageSize)
	switch t {
	case 0:
		qw.Order("release_stamp DESC")
	case 1:
		qw.Order("hits DESC")
	case 2:
		qw.Order(latestUpdateOrderSQL)
	}
	if err := qw.Find(&sl).Error; err != nil {
		log.Printf("GetMovieListBySort Error: %v", err)
		return nil
	}
	return GetBasicInfoBySearchInfos(sl...)
}
