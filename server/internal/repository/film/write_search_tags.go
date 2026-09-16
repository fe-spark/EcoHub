package film

import (
	"fmt"
	"log"
	"regexp"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

const searchTagsVisibleCacheInvalidateInterval = 2 * time.Second

const (
	searchTagsRebuildFilmBatchSize = 200
)

var searchTagsVisibleCacheState struct {
	mu     sync.Mutex
	lastAt time.Time
}

func BatchHandleSearchTag(infos ...model.FilmIndex) {
	if len(infos) == 0 {
		return
	}

	pids := collectSearchTagPidList(infos)
	if err := RefreshSearchTagsByPids(pids...); err != nil {
		log.Printf("RefreshSearchTagsByPids Error: %v", err)
		return
	}

	ClearAllSearchTagsCache()
	ClearAdminFilmSearchCache()
}

func UpdateSearchTagsForVisibleCollect(infos ...model.FilmIndex) {
	if len(infos) == 0 {
		return
	}
	for _, info := range infos {
		if err := handleDynamicSearchTagsTx(db.Mdb, info); err != nil {
			log.Printf("UpdateSearchTagsForVisibleCollect Error: %v", err)
		}
	}
	invalidateSearchTagsVisibleCacheThrottled()
}

func invalidateSearchTagsVisibleCacheThrottled() {
	now := time.Now()
	searchTagsVisibleCacheState.mu.Lock()
	if !searchTagsVisibleCacheState.lastAt.IsZero() && now.Sub(searchTagsVisibleCacheState.lastAt) < searchTagsVisibleCacheInvalidateInterval {
		searchTagsVisibleCacheState.mu.Unlock()
		return
	}
	searchTagsVisibleCacheState.lastAt = now
	searchTagsVisibleCacheState.mu.Unlock()

	ClearAllSearchTagsCache()
	ClearAdminFilmSearchCache()
}

func normalizeOrderedPids(pids []int64) []int64 {
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
	return orderedPids
}

func RefreshSearchTagsByPids(pids ...int64) error {
	orderedPids := normalizeOrderedPids(pids)
	if len(orderedPids) == 0 {
		return nil
	}

	start := time.Now()
	totalFilms := 0
	for idx, pid := range orderedPids {
		pidStart := time.Now()
		films, err := rebuildSearchTagsForPid(pid)
		if err != nil {
			return err
		}
		totalFilms += films
		log.Printf("[SearchTags] 标签重建进度 pid=%d (%d/%d) films=%d cost=%s total=%s",
			pid, idx+1, len(orderedPids), films, time.Since(pidStart), time.Since(start))
	}

	for _, pid := range orderedPids {
		ClearSearchTagsCache(pid)
	}
	log.Printf("[SearchTags] 标签重建完成 pids=%d films=%d cost=%s",
		len(orderedPids), totalFilms, time.Since(start))
	return nil
}

func rebuildSearchTagsForPid(pid int64) (int, error) {
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Unscoped().Where("pid = ?", pid).Delete(&model.SearchTagItem{}).Error; err != nil {
			return err
		}
		initializedPids.Delete(pid)
		return ensureStaticTagsForPidTx(tx, pid)
	}); err != nil {
		return 0, err
	}

	var infos []model.FilmIndex
	if err := db.Mdb.Where("pid = ?", pid).Find(&infos).Error; err != nil {
		return 0, err
	}
	if len(infos) == 0 {
		return 0, nil
	}

	for offset := 0; offset < len(infos); offset += searchTagsRebuildFilmBatchSize {
		end := offset + searchTagsRebuildFilmBatchSize
		if end > len(infos) {
			end = len(infos)
		}
		batch := infos[offset:end]
		items := aggregateSearchTagItems(collectDynamicSearchTagItemsBatch(batch))
		if len(items) == 0 {
			continue
		}
		if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
			return bulkUpsertSearchTagItemsTx(tx, items)
		}); err != nil {
			return 0, err
		}
	}
	return len(infos), nil
}

func collectDynamicSearchTagItemsBatch(infos []model.FilmIndex) []model.SearchTagItem {
	out := make([]model.SearchTagItem, 0, len(infos)*5)
	for _, info := range infos {
		out = append(out, collectDynamicSearchTagItems(info)...)
	}
	return out
}

func collectDynamicSearchTagItems(info model.FilmIndex) []model.SearchTagItem {
	if info.Pid <= 0 {
		return nil
	}
	items := make([]model.SearchTagItem, 0, 8)

	if info.Cid > 0 {
		catName := support.GetCategoryNameById(info.Cid)
		if catName == "" {
			catName = info.CName
		}
		items = append(items, collectSearchTagItems(catName, "Category", info.Pid, fmt.Sprint(info.Cid))...)
	}

	mainCategoryName := support.GetMainCategoryName(info.Pid)
	cleanPlot := support.CleanPlotTags(info.ClassTag, info.Area, mainCategoryName, info.CName)
	items = append(items, collectSearchTagItems(cleanPlot, "Plot", info.Pid)...)

	items = append(items, collectSearchTagItems(info.Area, "Area", info.Pid)...)
	items = append(items, collectSearchTagItems(info.Language, "Language", info.Pid)...)
	if info.Year > 0 {
		items = append(items, collectSearchTagItems(fmt.Sprint(info.Year), "Year", info.Pid)...)
	}
	return items
}

func collectSearchTagItems(allTags, tagType string, pid int64, customValues ...string) []model.SearchTagItem {
	allTags = reTagCleanup.ReplaceAllString(allTags, "")
	parts := reTagSplit.Split(allTags, -1)
	items := make([]model.SearchTagItem, 0, len(parts))
	for _, t := range parts {
		var customVal []string
		if tagType == "Category" && len(customValues) > 0 {
			customVal = customValues[:1]
		}
		if item, ok := buildSearchTagItem(t, tagType, pid, customVal...); ok {
			items = append(items, item)
		}
	}
	return items
}

func buildSearchTagItem(rawValue, tagType string, pid int64, customVal ...string) (model.SearchTagItem, bool) {
	v := normalizeSearchTagValue(tagType, rawValue)
	if v == "" || v == model.TagOthersValue || v == "其他" || v == "其它" || v == "全部" || v == "完结" || v == "HD" || v == "解说" || v == "剧情" || v == "暂无" {
		return model.SearchTagItem{}, false
	}
	val := v
	if len(customVal) > 0 {
		val = normalizeSearchTagValue(tagType, customVal[0])
		if val == "" {
			return model.SearchTagItem{}, false
		}
	}
	if tagType == "Category" && val == fmt.Sprint(pid) {
		return model.SearchTagItem{}, false
	}
	if tagType == "Year" {
		if y, _ := strconv.Atoi(v); y <= 0 {
			return model.SearchTagItem{}, false
		}
	}
	return model.SearchTagItem{Pid: pid, TagType: tagType, Name: v, Value: val, Score: 1}, true
}

func aggregateSearchTagItems(items []model.SearchTagItem) []model.SearchTagItem {
	if len(items) == 0 {
		return nil
	}
	type key struct {
		pid     int64
		tagType string
		value   string
	}
	agg := make(map[key]int, len(items))
	first := make(map[key]model.SearchTagItem, len(items))
	for _, item := range items {
		k := key{item.Pid, item.TagType, item.Value}
		if _, seen := first[k]; !seen {
			first[k] = item
		}
		agg[k]++
	}
	out := make([]model.SearchTagItem, 0, len(agg))
	for k, count := range agg {
		row := first[k]
		row.Score = int64(count)
		out = append(out, row)
	}
	return out
}

func bulkUpsertSearchTagItemsTx(tx *gorm.DB, items []model.SearchTagItem) error {
	if len(items) == 0 {
		return nil
	}
	return tx.Clauses(clause.OnConflict{
		Columns:   []clause.Column{{Name: "pid"}, {Name: "tag_type"}, {Name: "value"}},
		DoUpdates: clause.AssignmentColumns([]string{"name", "score", "deleted_at"}),
	}).CreateInBatches(items, upsertBatchSize).Error
}

func RefreshSearchTagsByMids(mids ...int64) error {
	if len(mids) == 0 {
		return nil
	}
	var infos []model.FilmIndex
	if err := db.Mdb.Select("pid").Where("mid IN ?", mids).Find(&infos).Error; err != nil {
		return err
	}
	return RefreshSearchTagsByPids(collectSearchTagPidList(infos)...)
}

func RebuildSearchTagsByPids(pids ...int64) error {
	return RefreshSearchTagsByPids(pids...)
}

func SaveSearchTag(filmIndex model.FilmIndex) {
	BatchHandleSearchTag(filmIndex)
}

func collectSearchTagPids(infos []model.FilmIndex) map[int64]bool {
	pids := make(map[int64]bool)
	for _, info := range infos {
		if info.Pid > 0 {
			pids[info.Pid] = true
		}
	}
	return pids
}

func collectSearchTagPidList(infos []model.FilmIndex) []int64 {
	pidSet := collectSearchTagPids(infos)
	pids := make([]int64, 0, len(pidSet))
	for pid := range pidSet {
		pids = append(pids, pid)
	}
	return pids
}

func handleDynamicSearchTags(info model.FilmIndex) {
	_ = handleDynamicSearchTagsTx(db.Mdb, info)
}

func handleDynamicSearchTagsTx(tx *gorm.DB, info model.FilmIndex) error {
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

func handleCategorySearchTag(info model.FilmIndex) {
	_ = handleCategorySearchTagTx(db.Mdb, info)
}

func handleCategorySearchTagTx(tx *gorm.DB, info model.FilmIndex) error {
	if info.Cid <= 0 {
		return nil
	}

	catName := support.GetCategoryNameById(info.Cid)
	if catName == "" {
		catName = info.CName
	}
	return HandleSearchTagsTx(tx, catName, "Category", info.Pid, fmt.Sprint(info.Cid))
}

func handlePlotSearchTag(info model.FilmIndex) {
	_ = handlePlotSearchTagTx(db.Mdb, info)
}

func handlePlotSearchTagTx(tx *gorm.DB, info model.FilmIndex) error {
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
