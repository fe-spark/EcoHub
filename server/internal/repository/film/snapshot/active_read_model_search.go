package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmquery "server/internal/repository/film/query"
	"server/internal/repository/film/shared"
	"server/internal/utils"
	"strconv"

	"golang.org/x/sync/singleflight"
)

const (
	tagSearchCacheTTL    = 2 * time.Hour
	snapshotSelectFields = "mid, first_source_id, pid, cid, c_name, name, score, hits, update_stamp, remarks, state, picture, picture_slide, custom_picture, custom_picture_slide, is_custom_picture, year, class_tag, area, language"
)

type tagSearchCacheItem struct {
	Total     int                      `json:"total"`
	PageCount int                      `json:"page_count"`
	Snapshots []model.FilmListSnapshot `json:"snapshots"`
}

var tagSearchSfGroup singleflight.Group

func ListFilmSnapshotsByTagsReadModel(version string, st model.SearchTagsVO, page *dto.Page) ([]model.FilmListSnapshot, error) {
	startedAt := time.Now()
	page = shared.EnsurePage(page)
	st = shared.NormalizeSearchTagsVO(st)
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" {
		return nil, ErrSearchIndexMissing
	}

	var cacheKey string
	if st.SourceId != "" {
		cacheKey = fmt.Sprintf("%s:v%s:src_%s:%d:%d:%s:%s:%s:%s:%s:p%d:s%d:ck4",
			config.FilmSearchTagsKey, version, st.SourceId, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year, st.Sort, page.Current, page.PageSize)
	} else {
		cacheKey = fmt.Sprintf("%s:v%s:%d:%d:%s:%s:%s:%s:%s:p%d:s%d",
			config.FilmSearchTagsKey, version, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year, st.Sort, page.Current, page.PageSize)
	}
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var item tagSearchCacheItem
			if json.Unmarshal([]byte(data), &item) == nil {
				page.Total = item.Total
				page.PageCount = item.PageCount
				log.Printf(
					"[FilmClassifySearch] 命中缓存 source=%q pid=%d cid=%d plot=%q area=%q language=%q year=%q sort=%q total=%d page=%d size=%d cost=%s",
					st.SourceId, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year, st.Sort, page.Total, page.Current, len(item.Snapshots), time.Since(startedAt),
				)
				return item.Snapshots, nil
			}
		}
	}

	val, err, _ := tagSearchSfGroup.Do(cacheKey, func() (any, error) {
		if db.Rdb != nil {
			if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
				var item tagSearchCacheItem
				if json.Unmarshal([]byte(data), &item) == nil {
					return item, nil
				}
			}
		}

		listGen := GetSearchCacheVersion()
		query := applySourceMembership(liveFilmQuery(), st.SourceId)
		if st.SourceId != "" {
			if st.Cid > 0 {
				query = filmquery.ApplySourceTypeMatchIDs(query, st.SourceId, "cid", repository.PublicSourceTypeIDs(st.SourceId, "cid", st.Cid))
			} else if st.Pid > 0 {
				query = filmquery.ApplySourceTypeMatchIDs(query, st.SourceId, "pid", repository.PublicSourceTypeIDs(st.SourceId, "pid", st.Pid))
			}
		} else if st.Cid > 0 {
			query = filmquery.ApplyLiveCategoryMatch(query, "cid", st.Cid)
		} else if st.Pid > 0 {
			query = filmquery.ApplyLiveCategoryMatch(query, "pid", st.Pid)
		}
		query, err := applyTagSearchFilter(query, version, st)
		if err != nil {
			return nil, err
		}

		var total int64 = -1
		var countKey string
		if st.SourceId != "" {
			countKey = fmt.Sprintf("%s:count:v%s:src_%s:%d:%d:%s:%s:%s:%s:ck3",
				config.FilmSearchTagsKey, version, st.SourceId, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year)
		} else {
			countKey = fmt.Sprintf("%s:count:v%s:%d:%d:%s:%s:%s:%s",
				config.FilmSearchTagsKey, version, st.Pid, st.Cid, st.Plot, st.Area, st.Language, st.Year)
		}
		if db.Rdb != nil {
			if countStr, err := db.Rdb.Get(db.Cxt, countKey).Result(); err == nil && countStr != "" {
				if parsedTotal, err := strconv.ParseInt(countStr, 10, 64); err == nil && parsedTotal >= 0 {
					total = parsedTotal
				}
			}
		}

		if total < 0 {
			if err := query.Count(&total).Error; err != nil {
				return tagSearchCacheItem{}, err
			}
			writeListCache(countKey, []byte(strconv.FormatInt(total, 10)), 10*time.Minute, listGen)
		}

		calcTotal := int(total)
		calcPageCount := (calcTotal + page.PageSize - 1) / page.PageSize
		if calcPageCount <= 0 {
			calcPageCount = 1
		}

		orderClause := "update_stamp DESC, mid DESC"
		switch st.Sort {
		case "hits":
			orderClause = "hits DESC, mid DESC"
		case "score":
			orderClause = "score DESC, mid DESC"
		case "year":
			orderClause = "year DESC, update_stamp DESC, mid DESC"
		}

		offset := shared.PageOffset(page)
		snapshots, err := findListPage(query, orderClause, offset, page.PageSize)
		if err != nil {
			return tagSearchCacheItem{}, err
		}

		item := tagSearchCacheItem{
			Total:     calcTotal,
			PageCount: calcPageCount,
			Snapshots: snapshots,
		}

		if raw, err := json.Marshal(item); err == nil {
			writeListCache(cacheKey, raw, listCacheTTL(len(snapshots), tagSearchCacheTTL), listGen)
		}
		return item, nil
	})

	if err != nil {
		return nil, err
	}
	if val == nil {
		return nil, ErrSearchIndexMissing
	}
	item, ok := val.(tagSearchCacheItem)
	if !ok {
		return nil, errors.New("检索索引损坏")
	}
	page.Total = item.Total
	page.PageCount = item.PageCount

	res := make([]model.FilmListSnapshot, len(item.Snapshots))
	copy(res, item.Snapshots)

	log.Printf(
		"[FilmClassifySearch] 筛选完成 pid=%d cid=%d plot=%q area=%q language=%q year=%q sort=%q total=%d page=%d size=%d cost=%s",
		st.Pid,
		st.Cid,
		st.Plot,
		st.Area,
		st.Language,
		st.Year,
		st.Sort,
		page.Total,
		page.Current,
		len(res),
		time.Since(startedAt),
	)
	return res, nil
}

type searchCacheItem struct {
	Total     int                      `json:"total"`
	PageCount int                      `json:"page_count"`
	Snapshots []model.FilmListSnapshot `json:"snapshots"`
}

func cloneFilmListSnapshots(src []model.FilmListSnapshot) []model.FilmListSnapshot {
	if src == nil {
		return nil
	}
	out := make([]model.FilmListSnapshot, len(src))
	copy(out, src)
	return out
}

func memoryKeywordSnapshots(version, sourceID, keyword, sortField string, pid, cid int64, page *dto.Page) ([]model.FilmListSnapshot, error) {
	page = shared.EnsurePage(page)
	keyword = strings.TrimSpace(keyword)
	sortField = utils.NormalizeSearchSortField(sortField)
	sourceID = strings.TrimSpace(sourceID)
	if keyword == "" || len([]rune(keyword)) > 64 || strings.HasPrefix(keyword, "http://") || strings.HasPrefix(keyword, "https://") {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmListSnapshot{}, nil
	}
	version = strings.TrimSpace(version)
	if version == "" {
		version = strings.TrimSpace(GetActiveSnapshotVersion())
	}
	if version == "" {
		return nil, ErrSearchIndexMissing
	}
	idx, err := loadFilmSearchMetaIndex(version)
	if err != nil {
		return nil, err
	}
	hits := searchFilmMetas(idx, keyword, sortField, pid, cid)
	if sourceID != "" {
		filtered := make([]scoredMetaHit, 0, len(hits))
		for _, h := range hits {
			pos, ok := idx.Pos[h.mid]
			if !ok || pos < 0 || pos >= len(idx.Items) {
				return nil, errors.New("检索索引损坏")
			}
			if idx.Items[pos].HasPlaySource(sourceID) {
				filtered = append(filtered, h)
			}
		}
		hits = filtered
	}
	pageMids := pageMidsFromMetaHits(hits, page)
	if len(pageMids) == 0 {
		return []model.FilmListSnapshot{}, nil
	}
	snaps, err := GetProjectedSnapshotsByMidsOrdered(version, pageMids)
	if err != nil {
		return nil, err
	}
	if snaps == nil {
		return []model.FilmListSnapshot{}, nil
	}
	return snaps, nil
}

func SearchSnapshotsByKeywordAndSortReadModel(version string, keyword string, sortField string, page *dto.Page) ([]model.FilmListSnapshot, error) {
	startedAt := time.Now()
	snaps, err := memoryKeywordSnapshots(version, "", keyword, sortField, 0, 0, page)
	if err != nil {
		return nil, err
	}
	log.Printf("[SearchFilm] 内存检索完成 keyword=%q sort=%q total=%d page=%d size=%d cost=%s",
		strings.TrimSpace(keyword), sortField, page.Total, page.Current, len(snaps), time.Since(startedAt))
	return snaps, nil
}

func SearchSnapshotsByKeywordSourceAndSortReadModel(version string, sourceID string, keyword string, sortField string, page *dto.Page) ([]model.FilmListSnapshot, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return SearchSnapshotsByKeywordAndSortReadModel(version, keyword, sortField, page)
	}
	startedAt := time.Now()
	snaps, err := memoryKeywordSnapshots(version, sourceID, keyword, sortField, 0, 0, page)
	if err != nil {
		return nil, err
	}
	log.Printf("[SearchFilm] 内存检索完成 keyword=%q source=%q sort=%q total=%d page=%d size=%d cost=%s",
		strings.TrimSpace(keyword), sourceID, sortField, page.Total, page.Current, len(snaps), time.Since(startedAt))
	return snaps, nil
}
