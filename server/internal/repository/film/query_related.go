package film

import (
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"

	"gorm.io/gorm"
)

func appendUniqueRelatedCandidates(dst []model.FilmIndex, src []model.FilmIndex, seen map[int64]struct{}, limit int) []model.FilmIndex {
	for _, item := range src {
		if _, ok := seen[item.Mid]; ok {
			continue
		}
		seen[item.Mid] = struct{}{}
		dst = append(dst, item)
		if len(dst) >= limit {
			break
		}
	}
	return dst
}

func queryRelatedCandidates(search model.FilmIndex, limit int, apply func(query *gorm.DB) *gorm.DB) []model.FilmIndex {
	if limit <= 0 {
		return nil
	}
	query := db.Mdb.Model(&model.FilmIndex{}).
		Where("mid != ?", search.Mid).
		Where("deleted_at IS NULL")
	query = applySameCurrentRootCategoryFilter(query, search)
	if apply != nil {
		query = apply(query)
	}
	var list []model.FilmIndex
	if err := query.Order(latestUpdateOrderSQL).Limit(limit).Find(&list).Error; err != nil {
		log.Printf("queryRelatedCandidates Error: %v", err)
		return nil
	}
	return list
}

func loadRelatedCandidates(search model.FilmIndex, limit int) []model.FilmIndex {
	coreToken := extractCoreSearchToken(search.Name)
	tags := splitClassTags(search.ClassTag)
	list := make([]model.FilmIndex, 0, limit)
	seen := make(map[int64]struct{}, limit)
	strongLimit := max(limit, 20)
	cidLimit := max(limit/2, 10)
	tagLimit := max(limit/3, 6)

	if search.SeriesKey != "" {
		list = appendUniqueRelatedCandidates(list, queryRelatedCandidates(search, strongLimit, func(query *gorm.DB) *gorm.DB {
			return query.Where("series_key = ?", search.SeriesKey)
		}), seen, limit)
	}
	if coreToken != "" {
		like := fmt.Sprintf("%%%s%%", coreToken)
		list = appendUniqueRelatedCandidates(list, queryRelatedCandidates(search, strongLimit, func(query *gorm.DB) *gorm.DB {
			return query.Where("name LIKE ? OR sub_title LIKE ?", like, like)
		}), seen, limit)
	}
	if search.Cid > 0 {
		list = appendUniqueRelatedCandidates(list, queryRelatedCandidates(search, cidLimit, func(query *gorm.DB) *gorm.DB {
			return applySameCurrentCategoryFilter(query, search)
		}), seen, limit)
	}
	for _, tag := range tags {
		list = appendUniqueRelatedCandidates(list, queryRelatedCandidates(search, tagLimit, func(query *gorm.DB) *gorm.DB {
			return query.Where("class_tag LIKE ?", fmt.Sprintf("%%%s%%", tag))
		}), seen, limit)
	}
	return list
}

func loadFallbackCandidates(search model.FilmIndex, limit int, exclude map[int64]struct{}) []model.FilmIndex {
	if limit <= 0 {
		return nil
	}
	appendUnique := func(dst []model.FilmIndex, source []model.FilmIndex, max int) []model.FilmIndex {
		for _, item := range source {
			if _, ok := exclude[item.Mid]; ok {
				continue
			}
			exclude[item.Mid] = struct{}{}
			dst = append(dst, item)
			if len(dst) >= max {
				break
			}
		}
		return dst
	}
	var result []model.FilmIndex
	if search.Cid > 0 {
		result = appendUnique(result, getFallbackRelatedSearchInfos(search, &dto.Page{Current: 1, PageSize: limit}), limit)
	}
	if len(result) >= limit || search.Pid <= 0 {
		return result
	}
	var pidHotList []model.FilmIndex
	hotSince := time.Now().AddDate(0, -1, 0).Unix()
	pidHotQuery := db.Mdb.Model(&model.FilmIndex{}).
		Where("mid != ?", search.Mid).
		Where("deleted_at IS NULL").
		Where("update_stamp > ?", hotSince)
	pidHotQuery = applySameCurrentRootCategoryFilter(pidHotQuery, search)
	if err := pidHotQuery.
		Order("year DESC, hits DESC, mid DESC").
		Limit(limit * 2).
		Find(&pidHotList).Error; err != nil {
		log.Printf("loadFallbackCandidates Pid Hot Fallback Error: %v", err)
	} else {
		result = appendUnique(result, pidHotList, limit)
	}
	if len(result) >= limit {
		return result
	}
	var pidList []model.FilmIndex
	pidQuery := db.Mdb.Model(&model.FilmIndex{}).
		Where("mid != ?", search.Mid).
		Where("deleted_at IS NULL")
	pidQuery = applySameCurrentRootCategoryFilter(pidQuery, search)
	if err := pidQuery.
		Order(latestUpdateOrderSQL).
		Limit(limit * 2).
		Find(&pidList).Error; err != nil {
		log.Printf("loadFallbackCandidates Pid Fallback Error: %v", err)
		return result
	}
	return appendUnique(result, pidList, limit)
}

func buildRelatedMovieQuery(search model.FilmIndex, coreToken string, tags []string) *gorm.DB {
	nameLike := fmt.Sprintf("%%%s%%", coreToken)
	prefixLike := fmt.Sprintf("%s%%", coreToken)
	escapedCoreToken := strings.ReplaceAll(coreToken, "'", "''")
	escapedPrefixLike := strings.ReplaceAll(prefixLike, "'", "''")
	escapedNameLike := strings.ReplaceAll(nameLike, "'", "''")

	query := db.Mdb.Model(&model.FilmIndex{}).
		Where("mid != ?", search.Mid).
		Where("deleted_at IS NULL")
	query = applySameCurrentRootCategoryFilter(query, search)

	nameCondition := db.Mdb.Where("name LIKE ? OR sub_title LIKE ?", nameLike, nameLike)
	for _, tag := range tags {
		nameCondition = nameCondition.Or("class_tag LIKE ?", fmt.Sprintf("%%%s%%", tag))
	}

	query = query.Where(nameCondition)
	query = query.Order(fmt.Sprintf("(name = '%s') DESC", escapedCoreToken))
	query = query.Order(fmt.Sprintf("(name LIKE '%s') DESC", escapedPrefixLike))
	query = query.Order(fmt.Sprintf("(name LIKE '%s' OR sub_title LIKE '%s') DESC", escapedNameLike, escapedNameLike))
	if search.Cid > 0 {
		query = query.Order(fmt.Sprintf("(cid = %d) DESC", search.Cid))
	}
	query = query.Order(latestUpdateOrderSQL)

	return query
}

func getFallbackRelatedSearchInfos(search model.FilmIndex, page *dto.Page) []model.FilmIndex {
	if search.Cid <= 0 {
		return nil
	}

	var list []model.FilmIndex
	query := db.Mdb.Model(&model.FilmIndex{}).
		Where("mid != ?", search.Mid).
		Where("deleted_at IS NULL")
	query = applySameCurrentCategoryFilter(query, search)
	if err := query.
		Order(latestUpdateOrderSQL).
		Offset(getPageOffset(page)).
		Limit(page.PageSize).
		Find(&list).Error; err != nil {
		log.Printf("GetRelateMovieBasicInfo Fallback Error: %v", err)
		return nil
	}
	return list
}

func GetRelateMovieBasicInfo(search model.FilmIndex, page *dto.Page) []model.MovieBasicInfo {
	page = ensurePage(page)
	targetSize := page.Current * page.PageSize
	candidates := loadRelatedCandidates(search, max(targetSize*5, 80))
	ranked := rankRelatedCandidates(search, candidates, targetSize)
	if len(ranked) < targetSize {
		exclude := make(map[int64]struct{}, len(ranked)+1)
		exclude[search.Mid] = struct{}{}
		for _, item := range ranked {
			exclude[item.Mid] = struct{}{}
		}
		fallback := loadFallbackCandidates(search, targetSize-len(ranked), exclude)
		ranked = append(ranked, fallback...)
	}
	offset := getPageOffset(page)
	if offset >= len(ranked) {
		return []model.MovieBasicInfo{}
	}
	end := min(offset+page.PageSize, len(ranked))
	return BuildMovieBasicInfos(ranked[offset:end]...)
}
