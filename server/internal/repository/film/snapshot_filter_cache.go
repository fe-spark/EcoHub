package film

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strconv"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/support"
)

const snapshotFilterCacheTTL = 12 * time.Hour

func snapshotFilterFactCacheKey(version string, pid int64) string {
	return fmt.Sprintf("%s:Facts:v%s:P%d", config.FilmClassifySearchKey, strings.TrimSpace(version), support.ResolveCategoryID(pid))
}

func loadSnapshotFilterFacts(version string, pid int64) []model.FilmListSnapshot {
	startedAt := time.Now()
	version = strings.TrimSpace(version)
	pid = support.ResolveCategoryID(pid)
	if version == "" {
		return nil
	}

	cacheKey := snapshotFilterFactCacheKey(version, pid)
	if data, err := db.Rdb.Get(db.Cxt, cacheKey).Bytes(); err == nil && len(data) > 0 {
		var snapshots []model.FilmListSnapshot
		if json.Unmarshal(data, &snapshots) == nil {
			log.Printf("[FilmClassifySearch] 内存筛选事实缓存命中 pid=%d count=%d cost=%s", pid, len(snapshots), time.Since(startedAt))
			return snapshots
		}
	}

	var snapshots []model.FilmListSnapshot
	query := db.Mdb.Model(&model.FilmListSnapshot{}).Where("snapshot_version = ?", version)
	if pid > 0 {
		query = applyCategoryFieldFilter(query, "pid", pid)
	} else {
		query = applyVisibleCategoryFilter(query)
	}
	if err := query.Find(&snapshots).Error; err != nil {
		log.Printf("loadSnapshotFilterFacts Error: %v", err)
		return nil
	}

	if data, err := json.Marshal(snapshots); err == nil {
		db.Rdb.Set(db.Cxt, cacheKey, data, snapshotFilterCacheTTL)
	}
	log.Printf("[FilmClassifySearch] 内存筛选事实缓存重建 pid=%d count=%d cost=%s", pid, len(snapshots), time.Since(startedAt))
	return snapshots
}

func WarmSnapshotFilterCaches(version string) {
	version = strings.TrimSpace(version)
	if version == "" {
		return
	}

	startedAt := time.Now()
	var roots []model.Category
	if err := db.Mdb.Where("pid = ? AND `show` = ?", 0, true).Order("sort ASC, id ASC").Find(&roots).Error; err != nil {
		log.Printf("WarmSnapshotFilterCaches Categories Error: %v", err)
		return
	}

	for _, root := range roots {
		if root.Id <= 0 {
			continue
		}
		loadSnapshotFilterFacts(version, root.Id)
	}
	log.Printf("[FilmClassifySearch] 内存筛选事实缓存预热完成 version=%s roots=%d cost=%s", version, len(roots), time.Since(startedAt))
}

func SearchSnapshotsByKeywordFast(version string, keyword string, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = ensurePage(page)
	keyword = strings.TrimSpace(keyword)
	snapshots := loadSnapshotFilterFacts(version, 0)
	if snapshots == nil {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}
	}

	filtered := make([]model.FilmListSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if keyword == "" || strings.Contains(snapshot.Name, keyword) || strings.Contains(snapshot.SubTitle, keyword) {
			filtered = append(filtered, snapshot)
		}
	}
	sort.SliceStable(filtered, func(i, j int) bool {
		if filtered[i].Year != filtered[j].Year {
			return filtered[i].Year > filtered[j].Year
		}
		if filtered[i].UpdateStamp != filtered[j].UpdateStamp {
			return filtered[i].UpdateStamp > filtered[j].UpdateStamp
		}
		return filtered[i].Mid > filtered[j].Mid
	})
	page.Total = len(filtered)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	offset := getPageOffset(page)
	if offset >= len(filtered) {
		return []model.FilmListSnapshot{}
	}
	end := offset + page.PageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	log.Printf("[SearchFilm] 内存筛选完成 keyword=%q total=%d page=%d size=%d cost=%s", keyword, page.Total, page.Current, page.PageSize, time.Since(startedAt))
	return filtered[offset:end]
}

func ListProvideSnapshotsFast(version string, st model.SearchTagsVO, keyword string, recentHours int, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = ensurePage(page)
	st = normalizeSearchTagsVO(st)
	snapshots := loadSnapshotFilterFacts(version, st.Pid)
	if snapshots == nil {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}
	}

	keyword = strings.TrimSpace(keyword)
	var timeLimit int64
	if recentHours > 0 {
		timeLimit = time.Now().Add(-time.Duration(recentHours) * time.Hour).Unix()
	}

	filtered := make([]model.FilmListSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if !matchesSnapshotSearchTags(snapshot, st) {
			continue
		}
		if keyword != "" && !strings.Contains(snapshot.Name, keyword) && !strings.Contains(snapshot.SubTitle, keyword) {
			continue
		}
		if timeLimit > 0 && snapshot.UpdateStamp < timeLimit {
			continue
		}
		filtered = append(filtered, snapshot)
	}

	sortSnapshotsBySearchTag(filtered, st.Sort)
	page.Total = len(filtered)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	offset := getPageOffset(page)
	if offset >= len(filtered) {
		return []model.FilmListSnapshot{}
	}
	end := offset + page.PageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	log.Printf("[ProvideVod] 内存筛选完成 pid=%d cid=%d keyword=%q total=%d page=%d size=%d cost=%s", st.Pid, st.Cid, keyword, page.Total, page.Current, page.PageSize, time.Since(startedAt))
	return filtered[offset:end]
}

func ListFilmSnapshotsByTagsFast(version string, st model.SearchTagsVO, page *dto.Page) []model.FilmListSnapshot {
	startedAt := time.Now()
	page = ensurePage(page)
	st = normalizeSearchTagsVO(st)
	snapshots := loadSnapshotFilterFacts(version, st.Pid)
	if snapshots == nil {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}
	}

	filtered := make([]model.FilmListSnapshot, 0, len(snapshots))
	for _, snapshot := range snapshots {
		if matchesSnapshotSearchTags(snapshot, st) {
			filtered = append(filtered, snapshot)
		}
	}

	sortSnapshotsBySearchTag(filtered, st.Sort)
	page.Total = len(filtered)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}

	offset := getPageOffset(page)
	if offset >= len(filtered) {
		return []model.FilmListSnapshot{}
	}
	end := offset + page.PageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	log.Printf(
		"[FilmClassifySearch] 内存筛选完成 pid=%d cid=%d plot=%q area=%q language=%q year=%q sort=%q total=%d page=%d size=%d cost=%s",
		st.Pid,
		st.Cid,
		st.Plot,
		st.Area,
		st.Language,
		st.Year,
		st.Sort,
		page.Total,
		page.Current,
		page.PageSize,
		time.Since(startedAt),
	)
	return filtered[offset:end]
}

func matchesSnapshotSearchTags(snapshot model.FilmListSnapshot, st model.SearchTagsVO) bool {
	return matchesSnapshotCategory(snapshot, st) &&
		matchesYearTag(snapshot, st.Year) &&
		matchesTextTag(snapshot.Area, st.Area) &&
		matchesTextTag(snapshot.Language, st.Language) &&
		matchesPlotTag(snapshot.ClassTag, st.Plot)
}

func matchesSnapshotCategory(snapshot model.FilmListSnapshot, st model.SearchTagsVO) bool {
	if st.Cid <= 0 {
		return true
	}
	if st.Cid == model.TagUncategorizedValue {
		return snapshot.Cid == 0
	}
	if support.IsRootCategory(st.Cid) {
		return support.ResolveCategoryID(snapshot.Pid) == st.Cid
	}
	return support.ResolveCategoryID(snapshot.Cid) == st.Cid
}

func matchesYearTag(snapshot model.FilmListSnapshot, value string) bool {
	value = strings.TrimSpace(value)
	if value == "" {
		return true
	}
	switch value {
	case model.TagUnknownValue:
		return snapshot.Year <= 0
	case model.TagOthersValue:
		return true
	default:
		year, err := strconv.ParseInt(value, 10, 64)
		return err == nil && snapshot.Year == year
	}
}

func matchesTextTag(actual string, value string) bool {
	value = strings.TrimSpace(value)
	actual = strings.TrimSpace(actual)
	if value == "" {
		return true
	}
	switch value {
	case model.TagUnknownValue:
		return actual == ""
	case model.TagOthersValue:
		return actual != ""
	default:
		return actual == value
	}
}

func matchesPlotTag(classTag string, value string) bool {
	value = strings.TrimSpace(value)
	classTag = strings.TrimSpace(classTag)
	if value == "" {
		return true
	}
	switch value {
	case model.TagUnknownValue:
		return classTag == ""
	case model.TagOthersValue:
		return classTag != ""
	default:
		return strings.Contains(classTag, value)
	}
}

func sortSnapshotsBySearchTag(snapshots []model.FilmListSnapshot, sortValue string) {
	if strings.TrimSpace(sortValue) == "" {
		sortValue = "update_stamp"
	}
	sort.SliceStable(snapshots, func(i, j int) bool {
		left := snapshots[i]
		right := snapshots[j]
		switch sortValue {
		case "hits":
			if left.Hits != right.Hits {
				return left.Hits > right.Hits
			}
		case "score":
			if left.Score != right.Score {
				return left.Score > right.Score
			}
		case "year":
			if left.Year != right.Year {
				return left.Year > right.Year
			}
		}
		if left.UpdateStamp != right.UpdateStamp {
			return left.UpdateStamp > right.UpdateStamp
		}
		return left.Mid > right.Mid
	})
}
