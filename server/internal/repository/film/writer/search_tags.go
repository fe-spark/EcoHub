package writer

import (
	"fmt"
	"log"
	"strconv"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/film/cache"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var initializedPids sync.Map

const (
	searchTagsRebuildFilmBatchSize = 200
)

func BatchHandleSearchTag(infos ...model.FilmIndex) {
	if len(infos) == 0 {
		return
	}

	pids := collectSearchTagPidList(infos)
	if err := RefreshSearchTagsByPids(pids...); err != nil {
		log.Printf("RefreshSearchTagsByPids Error: %v", err)
		return
	}

	cache.ClearAllSearchTagsCache()
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
		cache.ClearSearchTagsCache(pid)
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

	totalFilms := 0
	var lastID uint
	for {
		var batch []model.FilmIndex
		if err := db.Mdb.Model(&model.FilmIndex{}).
			Select("id, pid, cid, c_name, class_tag, area, language, year").
			Where("pid = ? AND id > ?", pid, lastID).
			Order("id ASC").
			Limit(searchTagsRebuildFilmBatchSize).
			Find(&batch).Error; err != nil {
			return totalFilms, err
		}
		if len(batch) == 0 {
			break
		}
		totalFilms += len(batch)
		lastID = batch[len(batch)-1].ID

		items := aggregateSearchTagItems(collectDynamicSearchTagItemsBatch(batch))
		if len(items) == 0 {
			continue
		}
		if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
			return bulkUpsertSearchTagItemsTx(tx, items)
		}); err != nil {
			return totalFilms, err
		}
	}
	return totalFilms, nil
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
	parts := shared.SplitRawSearchTags(allTags)
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
	v := shared.NormalizeSearchTagValue(tagType, rawValue)
	if v == "" || v == model.TagOthersValue || v == "其他" || v == "其它" || v == "全部" || v == "完结" || v == "HD" || v == "解说" || v == "剧情" || v == "暂无" {
		return model.SearchTagItem{}, false
	}
	val := v
	if len(customVal) > 0 {
		val = shared.NormalizeSearchTagValue(tagType, customVal[0])
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
	}).CreateInBatches(items, shared.UpsertBatchSize).Error
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
