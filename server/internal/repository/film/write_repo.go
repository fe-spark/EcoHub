package film

import (
	"encoding/json"
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
	"server/internal/utils"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

func searchInfoContentKeyUpsert() clause.OnConflict {
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "content_key"}},
		DoUpdates: clause.AssignmentColumns(searchInfoUpsertUpdateColumns),
	}
}

func movieSourceMappingUpsert() clause.OnConflict {
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "source_id"}, {Name: "source_mid"}},
		DoUpdates: clause.AssignmentColumns([]string{"global_mid", "updated_at", "deleted_at"}),
	}
}

func filterValidSearchInfos(list []model.SearchInfo) []model.SearchInfo {
	validList := make([]model.SearchInfo, 0, len(list))
	for _, item := range list {
		if strings.TrimSpace(item.Name) == "" {
			continue
		}
		validList = append(validList, item)
	}
	return validList
}

func upsertSearchInfos(list []model.SearchInfo) error {
	return upsertSearchInfosTx(db.Mdb, list)
}

func upsertSearchInfosTx(tx *gorm.DB, list []model.SearchInfo) error {
	if len(list) == 0 {
		return nil
	}
	return tx.Clauses(searchInfoContentKeyUpsert()).CreateInBatches(&list, 200).Error
}

func loadSearchInfoMidMapByContentKeys(contentKeys []string) map[string]int64 {
	return loadSearchInfoMidMapByContentKeysTx(db.Mdb, contentKeys)
}

func loadSearchInfoMidMapByContentKeysTx(tx *gorm.DB, contentKeys []string) map[string]int64 {
	if len(contentKeys) == 0 {
		return nil
	}

	var latestInfos []model.SearchInfo
	if err := tx.Where("content_key IN ?", contentKeys).Find(&latestInfos).Error; err != nil {
		return nil
	}

	keyToMid := make(map[string]int64, len(latestInfos))
	for _, info := range latestInfos {
		keyToMid[info.ContentKey] = info.Mid
	}
	return keyToMid
}

func buildContentKeys(list []model.SearchInfo) []string {
	contentKeys := make([]string, 0, len(list))
	for _, item := range list {
		contentKeys = append(contentKeys, item.ContentKey)
	}
	return contentKeys
}

func buildMovieSourceMappings(list []model.SearchInfo, keyToMid map[string]int64) []model.MovieSourceMapping {
	mappings := make([]model.MovieSourceMapping, 0, len(list))
	for _, item := range list {
		globalMid, ok := keyToMid[item.ContentKey]
		if !ok {
			continue
		}
		mappings = append(mappings, model.MovieSourceMapping{
			SourceId:  item.SourceId,
			SourceMid: item.Mid,
			GlobalMid: globalMid,
		})
	}
	return mappings
}

// saveMovieSourceMappings 仅维护“站点原始影片 ID -> 全局影片 ID”的最小映射，
// 供后台单片更新时把统一 mid 翻译回各站自己的 source_mid。
func saveMovieSourceMappings(mappings []model.MovieSourceMapping) {
	saveMovieSourceMappingsTx(db.Mdb, mappings)
}

func saveMovieSourceMappingsTx(tx *gorm.DB, mappings []model.MovieSourceMapping) {
	if len(mappings) == 0 {
		return
	}
	if err := tx.Clauses(movieSourceMappingUpsert()).CreateInBatches(&mappings, 200).Error; err != nil {
		log.Printf("saveMovieSourceMappings 失败: %v\n", err)
	}
}

func saveSearchInfosAndMappings(list []model.SearchInfo) (map[string]int64, error) {
	return saveSearchInfosAndMappingsTx(db.Mdb, list)
}

func saveSearchInfosAndMappingsTx(tx *gorm.DB, list []model.SearchInfo) (map[string]int64, error) {
	if len(list) == 0 {
		return nil, nil
	}

	if err := upsertSearchInfosTx(tx, list); err != nil {
		return nil, err
	}

	keyToMid := loadSearchInfoMidMapByContentKeysTx(tx, buildContentKeys(list))
	if keyToMid == nil {
		return nil, fmt.Errorf("load search info mids failed")
	}
	if err := saveMovieSourceMappingsTxE(tx, buildMovieSourceMappings(list, keyToMid)); err != nil {
		return nil, err
	}
	return keyToMid, nil
}

func saveMovieSourceMappingsTxE(tx *gorm.DB, mappings []model.MovieSourceMapping) error {
	if len(mappings) == 0 {
		return nil
	}
	return tx.Clauses(movieSourceMappingUpsert()).CreateInBatches(&mappings, 200).Error
}

func buildSearchInfosFromDetails(sourceID string, details []model.MovieDetail) ([]model.SearchInfo, map[string]model.SearchInfo) {
	infoList := make([]model.SearchInfo, 0, len(details))
	infoByKey := make(map[string]model.SearchInfo, len(details))
	for _, detail := range details {
		info := ConvertSearchInfo(sourceID, detail)
		infoList = append(infoList, info)
		infoByKey[info.ContentKey] = info
	}
	return infoList, infoByKey
}

func movieDetailInfoUpsert() clause.OnConflict {
	return clause.OnConflict{
		Columns:   []clause.Column{{Name: "mid"}},
		DoUpdates: clause.AssignmentColumns([]string{"source_id", "content", "updated_at", "deleted_at"}),
	}
}

func buildMovieDetailInfos(sourceID string, details []model.MovieDetail, infoByKey map[string]model.SearchInfo, keyToMid map[string]int64) []model.MovieDetailInfo {
	detailInfos := make([]model.MovieDetailInfo, 0, len(details))
	for _, detail := range details {
		info, ok := infoByKey[BuildContentKey(detail)]
		if !ok {
			continue
		}

		globalMid, ok := keyToMid[info.ContentKey]
		if !ok {
			globalMid = detail.Id
		}

		ApplyResolvedCategory(&detail, info)
		detail.Id = globalMid
		data, _ := json.Marshal(detail)
		detailInfos = append(detailInfos, model.MovieDetailInfo{Mid: globalMid, SourceId: sourceID, Content: string(data)})
	}
	return detailInfos
}

func buildMovieMatchKeyMappings(details []model.MovieDetail, infoByKey map[string]model.SearchInfo, keyToMid map[string]int64) map[int64][]string {
	midToKeys := make(map[int64][]string, len(details))
	for _, detail := range details {
		info, ok := infoByKey[BuildContentKey(detail)]
		if !ok {
			continue
		}
		globalMid, ok := keyToMid[info.ContentKey]
		if !ok || globalMid <= 0 {
			continue
		}
		midToKeys[globalMid] = BuildMovieMatchKeys(detail.DbId, detail.Name)
	}
	return midToKeys
}

func saveMovieDetailInfos(detailInfos []model.MovieDetailInfo) error {
	return saveMovieDetailInfosTx(db.Mdb, detailInfos)
}

func saveMovieDetailInfosTx(tx *gorm.DB, detailInfos []model.MovieDetailInfo) error {
	if len(detailInfos) == 0 {
		return nil
	}
	return tx.Clauses(movieDetailInfoUpsert()).Create(&detailInfos).Error
}

func clearDetailCaches(pid int64) {
	ClearSearchTagsCache(pid)
	db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
}

func clearSearchInfoCachesByPids(list []model.SearchInfo) {
	pidSet := make(map[int64]struct{})
	for _, item := range list {
		pidSet[item.Pid] = struct{}{}
	}
	clearSearchInfoCachesByPidSet(pidSet)
}

func clearSearchInfoCachesByPidSet(pidSet map[int64]struct{}) {
	for pid := range pidSet {
		if pid <= 0 {
			continue
		}
		ClearSearchTagsCache(pid)
	}
	db.Rdb.Del(db.Cxt, config.ActiveCategoryTreeKey)
	support.ClearIndexPageCache()
	ClearProvideListCache()
}

func BatchSaveOrUpdate(list []model.SearchInfo) map[string]int64 {
	list = filterValidSearchInfos(list)
	if len(list) == 0 {
		return nil
	}

	keyToMid, err := saveSearchInfosAndMappings(list)
	if err != nil {
		log.Printf("BatchSaveOrUpdate upsert 失败: %v\n", err)
		return nil
	}

	clearSearchInfoCachesByPids(list)
	BatchHandleSearchTag(list...)
	return keyToMid
}

func SaveSearchInfo(s model.SearchInfo) error {
	if _, err := saveSearchInfosAndMappings([]model.SearchInfo{s}); err != nil {
		return err
	}
	clearSearchInfoCachesByPids([]model.SearchInfo{s})
	BatchHandleSearchTag(s)
	return nil
}

func SaveDetails(id string, list []model.MovieDetail) error {
	infoList, infoByKey := buildSearchInfosFromDetails(id, list)
	infoList = filterValidSearchInfos(infoList)
	if len(infoList) == 0 {
		return nil
	}

	var refreshInfos []model.SearchInfo
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		keyToMid, err := saveSearchInfosAndMappingsTx(tx, infoList)
		if err != nil {
			return err
		}
		if err := saveMovieDetailInfosTx(tx, buildMovieDetailInfos(id, list, infoByKey, keyToMid)); err != nil {
			return err
		}
		if err := saveMovieMatchKeysByMidTx(tx, buildMovieMatchKeyMappings(list, infoByKey, keyToMid)); err != nil {
			return err
		}
		refreshInfos = reloadSearchInfosByContentKeysTx(tx, buildContentKeys(infoList))
		if len(refreshInfos) == 0 {
			return nil
		}
		return RefreshPlayFromSummaryBySearchInfosTx(tx, refreshInfos)
	}); err != nil {
		return err
	}

	clearSearchInfoCachesByPids(infoList)
	BatchHandleSearchTag(infoList...)
	ClearProvideListCache()
	return nil
}

func SaveDetail(id string, detail model.MovieDetail) error {
	searchInfo := ConvertSearchInfo(id, detail)
	if strings.TrimSpace(searchInfo.Name) == "" {
		return nil
	}

	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		keyToMid, err := saveSearchInfosAndMappingsTx(tx, []model.SearchInfo{searchInfo})
		if err != nil {
			return err
		}
		if err := saveMovieDetailInfosTx(tx, buildMovieDetailInfos(id, []model.MovieDetail{detail}, map[string]model.SearchInfo{searchInfo.ContentKey: searchInfo}, keyToMid)); err != nil {
			return err
		}
		if err := saveMovieMatchKeysByMidTx(tx, buildMovieMatchKeyMappings([]model.MovieDetail{detail}, map[string]model.SearchInfo{searchInfo.ContentKey: searchInfo}, keyToMid)); err != nil {
			return err
		}
		refreshInfos := reloadSearchInfosByContentKeysTx(tx, []string{searchInfo.ContentKey})
		if len(refreshInfos) == 0 {
			return nil
		}
		return RefreshPlayFromSummaryBySearchInfosTx(tx, refreshInfos)
	}); err != nil {
		return err
	}

	BatchHandleSearchTag(searchInfo)
	clearDetailCaches(searchInfo.Pid)
	ClearProvideListCache()
	return nil
}

func reloadSearchInfosByContentKeys(contentKeys []string) []model.SearchInfo {
	return reloadSearchInfosByContentKeysTx(db.Mdb, contentKeys)
}

func reloadSearchInfosByContentKeysTx(tx *gorm.DB, contentKeys []string) []model.SearchInfo {
	if len(contentKeys) == 0 {
		return nil
	}
	var infos []model.SearchInfo
	if err := tx.Where("content_key IN ?", contentKeys).Find(&infos).Error; err != nil {
		return nil
	}
	return infos
}

func BatchHandleSearchTag(infos ...model.SearchInfo) {
	if len(infos) == 0 {
		return
	}

	pids := make([]int64, 0, len(infos))
	for pid := range collectSearchTagPids(infos) {
		pids = append(pids, pid)
	}
	if err := RebuildSearchTagsByPids(pids...); err != nil {
		log.Printf("RebuildSearchTagsByPids Error: %v", err)
		return
	}

	ClearAllSearchTagsCache()
}

func RebuildSearchTagsByPids(pids ...int64) error {
	pidSet := make(map[int64]struct{}, len(pids))
	orderedPids := make([]int64, 0, len(pids))
	for _, pid := range pids {
		if pid <= 0 {
			continue
		}
		if _, ok := pidSet[pid]; ok {
			continue
		}
		pidSet[pid] = struct{}{}
		orderedPids = append(orderedPids, pid)
	}
	if len(orderedPids) == 0 {
		return nil
	}

	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		rebuiltInfos, err := rebuildSearchInfosFromMovieDetailsTx(tx, orderedPids)
		if err != nil {
			return err
		}
		if len(rebuiltInfos) == 0 {
			log.Printf("[RebuildSearchTagsByPids] 未找到可重建的事实详情数据: pids=%v", orderedPids)
			return nil
		}
		if err := upsertSearchInfosTx(tx, rebuiltInfos); err != nil {
			return err
		}
		if err := RefreshPlayFromSummaryBySearchInfosTx(tx, rebuiltInfos); err != nil {
			return err
		}

		if err := tx.Unscoped().Where("pid IN ?", orderedPids).Delete(&model.SearchTagItem{}).Error; err != nil {
			return err
		}

		for _, pid := range orderedPids {
			initializedPids.Delete(pid)
			if err := ensureStaticTagsForPidTx(tx, pid); err != nil {
				return err
			}
		}

		for _, info := range rebuiltInfos {
			if err := handleDynamicSearchTagsTx(tx, info); err != nil {
				return err
			}
		}
		return nil
	}); err != nil {
		return err
	}

	for _, pid := range orderedPids {
		ClearSearchTagsCache(pid)
	}
	return nil
}

func ForceRebuildDerivedData() error {
	refreshCategoryCaches()

	var rootPids []int64
	if err := db.Mdb.Model(&model.Category{}).Where("pid = ?", 0).Order("sort ASC, id ASC").Pluck("id", &rootPids).Error; err != nil {
		return err
	}

	return db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.SearchTagItem{}).Error; err != nil {
			return err
		}

		rebuiltInfos, err := rebuildAllSearchInfosFromMovieDetailsTx(tx)
		if err != nil {
			return err
		}
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.SearchInfo{}).Error; err != nil {
			return err
		}
		if len(rebuiltInfos) > 0 {
			if err := upsertSearchInfosTx(tx, rebuiltInfos); err != nil {
				return err
			}
			if err := RefreshPlayFromSummaryBySearchInfosTx(tx, rebuiltInfos); err != nil {
				return err
			}
			for _, info := range rebuiltInfos {
				if err := handleDynamicSearchTagsTx(tx, info); err != nil {
					return err
				}
			}
		}

		initializedPids = sync.Map{}
		for _, pid := range rootPids {
			if pid <= 0 {
				continue
			}
			if err := ensureStaticTagsForPidTx(tx, pid); err != nil {
				return err
			}
		}
		return nil
	})
}

type rebuildSearchInfoRow struct {
	Mid             int64
	ContentKey      string
	SourceId        string
	PlayFromSummary string
	Pid             int64
	Cid             int64
	CName           string
	RootCategoryKey string
	CategoryKey     string
	DetailContent   string
}

func rebuildSearchInfosFromMovieDetailsTx(tx *gorm.DB, pids []int64) ([]model.SearchInfo, error) {
	if len(pids) == 0 {
		return nil, nil
	}
	pidSet := make(map[int64]struct{}, len(pids))
	for _, pid := range pids {
		if pid > 0 {
			pidSet[pid] = struct{}{}
		}
	}
	return rebuildSearchInfosFromMovieDetailsByPidSetTx(tx, pidSet)
}

func rebuildAllSearchInfosFromMovieDetailsTx(tx *gorm.DB) ([]model.SearchInfo, error) {
	return rebuildSearchInfosFromMovieDetailsByPidSetTx(tx, nil)
}

func rebuildSearchInfosFromMovieDetailsByPidSetTx(tx *gorm.DB, pidSet map[int64]struct{}) ([]model.SearchInfo, error) {
	var rows []rebuildSearchInfoRow
	if err := tx.Model(&model.SearchInfo{}).
		Select("search_info.mid, search_info.content_key, search_info.source_id, search_info.play_from_summary, search_info.pid, search_info.cid, search_info.c_name, search_info.root_category_key, search_info.category_key, movie_detail_info.content AS detail_content").
		Joins("JOIN movie_detail_info ON movie_detail_info.mid = search_info.mid").
		Scan(&rows).Error; err != nil {
		return nil, err
	}
	if len(rows) == 0 {
		log.Printf("[rebuildSearchInfosFromMovieDetailsTx] 未命中 search_info 与 movie_detail_info 关联数据")
		return nil, nil
	}

	rebuiltInfos := make([]model.SearchInfo, 0, len(rows))
	for _, row := range rows {
		if strings.TrimSpace(row.DetailContent) == "" {
			log.Printf("[rebuildSearchInfosFromMovieDetailsTx] 详情内容为空: mid=%d source=%s", row.Mid, row.SourceId)
			continue
		}

		var detail model.MovieDetail
		if err := json.Unmarshal([]byte(row.DetailContent), &detail); err != nil {
			log.Printf("[rebuildSearchInfosFromMovieDetailsTx] 详情反序列化失败: mid=%d source=%s err=%v", row.Mid, row.SourceId, err)
			continue
		}

		category := resolveSearchCategory(row.SourceId, detail)
		if category.Pid <= 0 && row.Pid > 0 {
			category = resolveLocalCategory(row.Pid, row.Cid, row.CName)
		}
		if len(pidSet) > 0 {
			if _, ok := pidSet[category.Pid]; !ok {
				continue
			}
		}
		meta := normalizeSearchMetadata(detail, category)
		rebuilt := buildSearchInfo(row.SourceId, detail, category, meta)
		rebuilt.Mid = row.Mid
		rebuilt.ContentKey = row.ContentKey
		rebuilt.SourceId = row.SourceId
		rebuilt.PlayFromSummary = row.PlayFromSummary
		rebuilt.CName = category.CName
		if rebuilt.RootCategoryKey == "" {
			rebuilt.RootCategoryKey = row.RootCategoryKey
		}
		if rebuilt.RootCategoryKey == "" {
			rebuilt.RootCategoryKey = category.PKey
		}
		if rebuilt.CategoryKey == "" {
			rebuilt.CategoryKey = row.CategoryKey
		}
		if rebuilt.CategoryKey == "" {
			rebuilt.CategoryKey = category.CKey
		}

		if rebuilt.Pid <= 0 {
			continue
		}
		rebuiltInfos = append(rebuiltInfos, rebuilt)
	}
	if len(rebuiltInfos) == 0 {
		log.Printf("[rebuildSearchInfosFromMovieDetailsTx] 事实详情存在，但未生成任何有效 SearchInfo")
	}

	return rebuiltInfos, nil
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
	_ = handleDynamicSearchTagsTx(db.Mdb, info)
}

func handleDynamicSearchTagsTx(tx *gorm.DB, info model.SearchInfo) error {
	if info.Pid <= 0 {
		return nil
	}

	if err := handleCategorySearchTagTx(tx, info); err != nil {
		return err
	}
	if err := handlePlotSearchTagTx(tx, info); err != nil {
		return err
	}
	if err := HandleSearchTagsTx(tx, info.Area, "Area", info.Pid); err != nil {
		return err
	}
	if err := HandleSearchTagsTx(tx, info.Language, "Language", info.Pid); err != nil {
		return err
	}
	if info.Year > 0 {
		if err := HandleSearchTagsTx(tx, fmt.Sprint(info.Year), "Year", info.Pid); err != nil {
			return err
		}
	}
	return nil
}

func handleCategorySearchTag(info model.SearchInfo) {
	_ = handleCategorySearchTagTx(db.Mdb, info)
}

func handleCategorySearchTagTx(tx *gorm.DB, info model.SearchInfo) error {
	if info.Cid <= 0 {
		return nil
	}

	catName := support.GetCategoryNameById(info.Cid)
	if catName == "" {
		catName = info.CName
	}
	return HandleSearchTagsTx(tx, catName, "Category", info.Pid, fmt.Sprint(info.Cid))
}

func handlePlotSearchTag(info model.SearchInfo) {
	_ = handlePlotSearchTagTx(db.Mdb, info)
}

func handlePlotSearchTagTx(tx *gorm.DB, info model.SearchInfo) error {
	mainCategoryName := support.GetMainCategoryName(info.Pid)
	cleanPlot := support.CleanPlotTags(info.ClassTag, info.Area, mainCategoryName, info.CName)
	return HandleSearchTagsTx(tx, cleanPlot, "Plot", info.Pid)
}

func ensureStaticTagsForPid(pid int64) {
	_ = ensureStaticTagsForPidTx(db.Mdb, pid)
}

func ensureStaticTagsForPidTx(tx *gorm.DB, pid int64) error {
	if _, ok := initializedPids.Load(pid); ok {
		return nil
	}

	var initialItems []model.SearchTagItem
	for i := 65; i <= 90; i++ {
		v := string(rune(i))
		initialItems = append(initialItems, model.SearchTagItem{Pid: pid, TagType: "Initial", Name: v, Value: v, Score: int64(90 - i)})
	}
	if err := tx.Clauses(clause.OnConflict{DoNothing: true}).Create(&initialItems).Error; err != nil {
		return err
	}
	initializedPids.Store(pid, true)
	return nil
}

var (
	reTagCleanup = regexp.MustCompile(`[\s\n\r]+`)
	reTagSplit   = regexp.MustCompile(`[/,，、\s\.\+\|]`)
)

func normalizeSearchTagValue(tagType string, value string) string {
	value = strings.TrimSpace(value)
	value = strings.TrimRight(value, ":：")

	switch tagType {
	case "Area":
		switch value {
		case "地区", "制片国家", "制片国家地区":
			return ""
		}
	case "Language":
		switch value {
		case "语言", "对白语言":
			return ""
		}
	}

	return value
}

func HandleSearchTags(allTags string, tagType string, pid int64, customValues ...string) {
	_ = HandleSearchTagsTx(db.Mdb, allTags, tagType, pid, customValues...)
}

func HandleSearchTagsTx(tx *gorm.DB, allTags string, tagType string, pid int64, customValues ...string) error {
	allTags = reTagCleanup.ReplaceAllString(allTags, "")
	parts := reTagSplit.Split(allTags, -1)
	var saveErr error

	upsert := func(v string, customVal ...string) {
		v = normalizeSearchTagValue(tagType, v)
		if v == "" || v == model.TagOthersValue || v == "其他" || v == "其它" || v == "全部" || v == "完结" || v == "HD" || v == "解说" || v == "剧情" || v == "暂无" {
			return
		}

		val := v
		if len(customVal) > 0 {
			val = normalizeSearchTagValue(tagType, customVal[0])
			if val == "" {
				return
			}
		}

		if tagType == "Category" && val == fmt.Sprint(pid) {
			return
		}

		if tagType == "Year" {
			if y, _ := strconv.Atoi(v); y <= 0 {
				return
			}
		}

		if err := tx.Clauses(clause.OnConflict{
			Columns: []clause.Column{{Name: "pid"}, {Name: "tag_type"}, {Name: "value"}},
			DoUpdates: clause.Assignments(map[string]any{
				"score":      gorm.Expr("score + 1"),
				"name":       v,
				"deleted_at": nil,
			}),
		}).Create(&model.SearchTagItem{Pid: pid, TagType: tagType, Name: v, Value: val, Score: 1}).Error; err != nil {
			saveErr = err
		}
	}

	for _, t := range parts {
		if saveErr != nil {
			return saveErr
		}
		if tagType == "Category" && len(customValues) > 0 {
			upsert(t, customValues[0])
		} else {
			upsert(t)
		}
	}
	return saveErr
}

func resolveLocalCategory(pid int64, cid int64, cName string) resolvedSearchCategory {
	result := resolvedSearchCategory{CName: strings.TrimSpace(cName)}
	if cid > 0 {
		result.Cid = cid
	}
	if result.Cid > 0 {
		result.Pid = support.GetRootId(result.Cid)
	}
	if result.Pid == 0 && pid > 0 {
		result.Pid = pid
	}
	if result.Pid > 0 && result.Cid > 0 && result.CName == "" {
		result.CName = support.GetCategoryNameById(result.Cid)
	}
	if result.Pid > 0 && result.PKey == "" {
		result.PKey = support.GetCategoryStableKeyByID(result.Pid)
	}
	if result.Cid > 0 {
		result.CKey = support.GetCategoryStableKeyByID(result.Cid)
	}
	return result
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
	if strings.TrimSpace(sourceId) == "manual" {
		return resolveLocalCategory(detail.Pid, detail.Cid, detail.CName)
	}

	sourceCid := detail.Cid
	sourcePid := detail.Pid
	if detail.RawCid > 0 {
		sourceCid = detail.RawCid
	}
	if detail.RawPid > 0 {
		sourcePid = detail.RawPid
	}

	result := resolvedSearchCategory{CName: strings.TrimSpace(detail.CName)}
	result.Cid = support.GetLocalCategoryId(sourceId, sourceCid)
	if result.Cid > 0 {
		result.Pid = support.GetRootId(result.Cid)
	}
	if result.Pid == 0 {
		result.Pid = support.GetRootId(support.GetLocalCategoryId(sourceId, sourcePid))
	}
	if result.Pid > 0 && result.Cid == 0 && result.CName != "" {
		var category model.Category
		if err := db.Mdb.Where("pid = ? AND name = ?", result.Pid, result.CName).First(&category).Error; err == nil {
			result.Cid = category.Id
		}
	}
	if result.Pid > 0 && result.CName == "" {
		result.CName = support.GetCategoryNameById(result.Pid)
	}
	if result.Pid > 0 {
		result.PKey = support.GetCategoryStableKeyByID(result.Pid)
	}
	if result.Cid > 0 {
		result.CKey = support.GetCategoryStableKeyByID(result.Cid)
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

	finalArea := support.NormalizeArea(detail.Area)
	finalLang := support.NormalizeLanguage(detail.Language)
	mainCategoryName := support.GetMainCategoryName(category.Pid)

	return normalizedSearchMeta{
		Score:       score,
		UpdateStamp: stamp.Unix(),
		Year:        year,
		Area:        finalArea,
		Language:    finalLang,
		ClassTag:    support.CleanPlotTags(detail.ClassTag, finalArea, mainCategoryName, category.CName),
	}
}

func buildSearchInfo(sourceId string, detail model.MovieDetail, category resolvedSearchCategory, meta normalizedSearchMeta) model.SearchInfo {
	return model.SearchInfo{
		Mid:               detail.Id,
		ContentKey:        BuildContentKey(detail),
		SourceId:          sourceId,
		Cid:               category.Cid,
		Pid:               category.Pid,
		RootCategoryKey:   category.PKey,
		CategoryKey:       category.CKey,
		SeriesKey:         utils.BuildSeriesKey(detail.Name, detail.SubTitle),
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
		CollectStamp:      detail.AddTime,
		Picture:           detail.Picture,
		PictureSlide:      detail.PictureSlide,
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
