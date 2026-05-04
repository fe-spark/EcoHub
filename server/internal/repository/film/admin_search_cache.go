package film

import (
	"encoding/json"
	"fmt"
	"log"
	"sort"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
)

const adminFilmSearchCacheTTL = 30 * time.Minute

func adminFilmSearchFactCacheKey() string {
	return fmt.Sprintf("%s:Manage:FilmSearch:Facts", config.RedisKeyPrefix)
}

func loadAdminFilmSearchFacts() []model.FilmIndex {
	startedAt := time.Now()
	cacheKey := adminFilmSearchFactCacheKey()
	if data, err := db.Rdb.Get(db.Cxt, cacheKey).Bytes(); err == nil && len(data) > 0 {
		var indexes []model.FilmIndex
		if json.Unmarshal(data, &indexes) == nil {
			log.Printf("[ManageFilmSearch] 内存筛选事实缓存命中 count=%d cost=%s", len(indexes), time.Since(startedAt))
			return indexes
		}
	}

	var indexes []model.FilmIndex
	query := applyVisibleCategoryFilter(db.Mdb.Model(&model.FilmIndex{}))
	if err := query.Find(&indexes).Error; err != nil {
		log.Printf("loadAdminFilmSearchFacts Error: %v", err)
		return nil
	}
	if data, err := json.Marshal(indexes); err == nil {
		db.Rdb.Set(db.Cxt, cacheKey, data, adminFilmSearchCacheTTL)
	}
	log.Printf("[ManageFilmSearch] 内存筛选事实缓存重建 count=%d cost=%s", len(indexes), time.Since(startedAt))
	return indexes
}

func GetSearchPageFast(s model.SearchVo) []model.FilmIndex {
	startedAt := time.Now()
	page := ensurePage(s.Paging)
	indexes := loadAdminFilmSearchFacts()
	if indexes == nil {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmIndex{}
	}

	filtered := make([]model.FilmIndex, 0, len(indexes))
	for _, index := range indexes {
		if matchesAdminSearch(index, s) {
			filtered = append(filtered, index)
		}
	}

	sort.SliceStable(filtered, func(i, j int) bool {
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
		return []model.FilmIndex{}
	}
	end := offset + page.PageSize
	if end > len(filtered) {
		end = len(filtered)
	}
	log.Printf(
		"[ManageFilmSearch] 内存筛选完成 name=%q pid=%d cid=%d plot=%q area=%q language=%q year=%d total=%d page=%d size=%d cost=%s",
		s.Name,
		s.Pid,
		s.Cid,
		s.Plot,
		s.Area,
		s.Language,
		s.Year,
		page.Total,
		page.Current,
		page.PageSize,
		time.Since(startedAt),
	)
	return filtered[offset:end]
}

func matchesAdminSearch(index model.FilmIndex, s model.SearchVo) bool {
	name := strings.TrimSpace(s.Name)
	if name != "" && !strings.Contains(index.Name, name) {
		return false
	}
	if !matchesAdminSearchCategory(index, s) {
		return false
	}
	if s.Plot != "" && !strings.Contains(index.ClassTag, s.Plot) {
		return false
	}
	if s.Area != "" && strings.TrimSpace(index.Area) != s.Area {
		return false
	}
	if s.Language != "" && strings.TrimSpace(index.Language) != s.Language {
		return false
	}
	if s.Year > 0 && index.Year != s.Year {
		return false
	}
	if s.BeginTime > 0 && index.UpdateStamp < s.BeginTime {
		return false
	}
	if s.EndTime > 0 && index.UpdateStamp > s.EndTime {
		return false
	}
	return true
}

func matchesAdminSearchCategory(index model.FilmIndex, s model.SearchVo) bool {
	pid := support.ResolveCategoryID(s.Pid)
	cid := support.ResolveCategoryID(s.Cid)
	if cid > 0 {
		if support.IsRootCategory(cid) {
			return support.ResolveCategoryID(index.Pid) == cid
		}
		return support.ResolveCategoryID(index.Cid) == cid
	}
	if pid > 0 {
		return support.ResolveCategoryID(index.Pid) == pid
	}
	return true
}

func ClearAdminFilmSearchCache() {
	db.Rdb.Del(db.Cxt, adminFilmSearchFactCacheKey())
}
