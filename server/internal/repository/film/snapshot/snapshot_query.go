package snapshot

import (
	"encoding/json"
	"errors"
	"fmt"
	"gorm.io/gorm"
	"log"
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/film/shared"
)

func GetSnapshotByMid(version string, mid int64) *model.FilmListSnapshot {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" || mid <= 0 || db.Mdb == nil {
		return nil
	}
	var snapshot model.FilmListSnapshot
	if err := db.Mdb.Unscoped().Where("snapshot_version = ? AND mid = ?", version, mid).First(&snapshot).Error; err != nil {
		return nil
	}
	return &snapshot
}

// GetSnapshotsByMidsOrdered 按 mid 列表顺序取当前版本快照；无快照的 mid 跳过。
func GetSnapshotsByMidsOrdered(version string, mids []int64) []model.FilmListSnapshot {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" || len(mids) == 0 || db.Mdb == nil {
		return nil
	}
	uniq := make([]int64, 0, len(mids))
	seen := make(map[int64]struct{}, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		uniq = append(uniq, mid)
	}
	if len(uniq) == 0 {
		return nil
	}
	byMid := make(map[int64]model.FilmListSnapshot, len(uniq))
	const chunk = 200
	for start := 0; start < len(uniq); start += chunk {
		end := start + chunk
		if end > len(uniq) {
			end = len(uniq)
		}
		var rows []model.FilmListSnapshot
		if err := db.Mdb.Unscoped().Where("snapshot_version = ? AND mid IN ?", version, uniq[start:end]).Find(&rows).Error; err != nil {
			continue
		}
		for _, row := range rows {
			if row.Mid > 0 {
				byMid[row.Mid] = row
			}
		}
	}
	out := make([]model.FilmListSnapshot, 0, len(mids))
	emitted := make(map[int64]struct{}, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := emitted[mid]; ok {
			continue
		}
		snap, ok := byMid[mid]
		if !ok {
			continue
		}
		emitted[mid] = struct{}{}
		out = append(out, snap)
	}
	return out
}

func GetMovieDetailBySnapshot(snapshot model.FilmListSnapshot) (*model.MovieDetail, int64) {
	if snapshot.Mid <= 0 {
		return nil, 0
	}
	var movieDetailInfo model.MovieDetailInfo
	if err := db.Mdb.Where("mid = ?", snapshot.Mid).First(&movieDetailInfo).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("GetMovieDetailBySnapshot Error: %v", err)
		}
		return nil, 0
	}
	var detail model.MovieDetail
	if err := json.Unmarshal([]byte(movieDetailInfo.Content), &detail); err != nil {
		log.Printf("Unmarshal Snapshot MovieDetail Error: %v", err)
		return nil, 0
	}
	shared.ApplyFilmListSnapshot(&detail, snapshot)
	normalizeMovieDetailLists(&detail)
	return &detail, snapshot.UpdateStamp
}

func HasMovieDetail(mid int64) bool {
	if mid <= 0 {
		return false
	}
	var count int64
	if err := db.Mdb.Model(&model.MovieDetailInfo{}).Where("mid = ?", mid).Limit(1).Count(&count).Error; err != nil {
		log.Printf("HasMovieDetail Error: %v", err)
		return false
	}
	return count > 0
}

func normalizeMovieDetailLists(detail *model.MovieDetail) {
	if detail == nil {
		return
	}
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
}

func GetSnapshotMovieListByCategory(version string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	return GetSnapshotMovieListByCategoryReadModel(version, field, id, limit, offset)
}

func GetSnapshotMovieListByCategoryPage(version string, field string, id int64, page *dto.Page) []model.MovieBasicInfo {
	return GetSnapshotMovieListByCategoryPageReadModel(version, field, id, page)
}

func GetSnapshotHotMovieListByCategory(version string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	return GetSnapshotHotMovieListByCategoryReadModel(version, field, id, limit, offset)
}

func GetSnapshotDynamicHotMovieListByCategory(version string, field string, id int64, limit int, poolSize int) []model.MovieBasicInfo {
	return GetSnapshotDynamicHotMovieListByCategoryReadModel(version, field, id, limit, poolSize)
}

func SnapshotClassifyCacheKey(version string, pid int64, page *dto.Page) string {
	page = shared.EnsurePage(page)
	return fmt.Sprintf("%s:v%s:P%d:C%d:S%d", config.FilmClassifyCacheKey, version, pid, page.Current, page.PageSize)
}

// GetSnapshotBannerCandidates 按排片策略与分类条件获取用于轮播的候选影片快照
func GetSnapshotBannerCandidates(version string, strategy string, categoryPids []int64, limit int) []model.FilmListSnapshot {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if version == "" || limit <= 0 || db.Mdb == nil {
		return []model.FilmListSnapshot{}
	}

	query := db.Mdb.Unscoped().Model(&model.FilmListSnapshot{}).
		Where("snapshot_version = ?", version)

	if len(categoryPids) > 0 {
		query = query.Where("pid IN ?", categoryPids)
	}

	applyStrategy := func(q *gorm.DB, withScoreFilter bool) *gorm.DB {
		switch strategy {
		case "hot_random":
			return q.Order("hits DESC")
		case "score_random":
			if withScoreFilter {
				q = q.Where("score >= ?", 6.0)
			}
			return q.Order("score DESC, hits DESC")
		case "latest_random":
			return q.Order("update_stamp DESC")
		default:
			return q.Order("hits DESC, update_stamp DESC")
		}
	}

	var results []model.FilmListSnapshot
	if err := applyStrategy(query, true).Limit(limit).Find(&results).Error; err != nil {
		log.Println("[Snapshot] 获取轮播候选集异常:", err)
		return []model.FilmListSnapshot{}
	}
	if len(results) == 0 && strategy == "score_random" {
		log.Printf("[Snapshot] 高分候选池为空，已回退为不加评分过滤的候选池")
		fallback := db.Mdb.Unscoped().Model(&model.FilmListSnapshot{}).
			Where("snapshot_version = ?", version)
		if len(categoryPids) > 0 {
			fallback = fallback.Where("pid IN ?", categoryPids)
		}
		if err := applyStrategy(fallback, false).Limit(limit).Find(&results).Error; err != nil {
			log.Println("[Snapshot] 获取轮播候选集回退异常:", err)
			return []model.FilmListSnapshot{}
		}
	}
	return results
}
