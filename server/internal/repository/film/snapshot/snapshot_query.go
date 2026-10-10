package snapshot

import (
	"fmt"
	"gorm.io/gorm"
	"log"
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	filmquery "server/internal/repository/film/query"
	"server/internal/repository/film/shared"
)

func GetSnapshotByMid(version string, mid int64) *model.FilmListSnapshot {
	if mid <= 0 || db.Mdb == nil {
		return nil
	}
	var index model.FilmIndex
	if err := liveFilmQuery().Where("mid = ?", mid).First(&index).Error; err != nil {
		return nil
	}
	snap := buildFilmListSnapshot(resolveListVersion(version), index)
	return &snap
}

// GetSnapshotsByMidsOrdered 按 mid 列表顺序取当前版本快照；无快照的 mid 跳过。
func GetSnapshotsByMidsOrdered(version string, mids []int64) []model.FilmListSnapshot {
	if len(mids) == 0 || db.Mdb == nil {
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
		var rows []model.FilmIndex
		if err := liveFilmQuery().Where("mid IN ?", uniq[start:end]).Find(&rows).Error; err != nil {
			continue
		}
		for _, row := range rows {
			if row.Mid > 0 {
				byMid[row.Mid] = buildFilmListSnapshot(resolveListVersion(version), row)
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
	var detail model.MovieDetail
	shared.ApplyFilmListSnapshot(&detail, snapshot)
	normalizeMovieDetailLists(&detail)
	return &detail, snapshot.UpdateStamp
}

func HasMovieDetail(mid int64) bool {
	if mid <= 0 {
		return false
	}
	var count int64
	if err := db.Mdb.Model(&model.FilmIndex{}).Where("mid = ?", mid).Limit(1).Count(&count).Error; err != nil {
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

func GetSnapshotMovieListByCategoryWithSource(version string, sourceId string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	return GetSnapshotMovieListByCategoryWithSourceReadModel(version, sourceId, field, id, limit, offset)
}

func GetSnapshotMovieListByCategoryPage(version string, field string, id int64, page *dto.Page) []model.MovieBasicInfo {
	return GetSnapshotMovieListByCategoryPageReadModel(version, field, id, page)
}

func GetSnapshotMovieListByCategoryPageWithSource(version string, sourceId string, field string, id int64, page *dto.Page) []model.MovieBasicInfo {
	return GetSnapshotMovieListByCategoryPageWithSourceReadModel(version, sourceId, field, id, page)
}

func GetSnapshotHotMovieListByCategory(version string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	return GetSnapshotHotMovieListByCategoryReadModel(version, field, id, limit, offset)
}

func GetSnapshotHotMovieListByCategoryWithSource(version string, sourceId string, field string, id int64, limit int, offset int) []model.MovieBasicInfo {
	return GetSnapshotHotMovieListByCategoryWithSourceReadModel(version, sourceId, field, id, limit, offset)
}

func GetSnapshotDynamicHotMovieListByCategory(version string, field string, id int64, limit int, poolSize int) []model.MovieBasicInfo {
	return GetSnapshotDynamicHotMovieListByCategoryReadModel(version, field, id, limit, poolSize)
}

func GetSnapshotDynamicHotMovieListByCategoryWithSource(version string, sourceId string, field string, id int64, limit int, poolSize int) []model.MovieBasicInfo {
	return GetSnapshotDynamicHotMovieListByCategoryWithSourceReadModel(version, sourceId, field, id, limit, poolSize)
}

func SnapshotClassifyCacheKey(version string, pid int64, page *dto.Page) string {
	page = shared.EnsurePage(page)
	return fmt.Sprintf("%s:v%s:P%d:C%d:S%d", config.FilmClassifyCacheKey, version, pid, page.Current, page.PageSize)
}

// GetSnapshotBannerCandidates 按排片策略、分类和采集站获取轮播候选。sourceID 为空时不限站。
func GetSnapshotBannerCandidates(version string, strategy string, sourceID string, categoryPids []int64, limit int) []model.FilmListSnapshot {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if limit <= 0 || db.Mdb == nil {
		return []model.FilmListSnapshot{}
	}

	query := applyBannerCategories(applySourceMembership(liveFilmQuery(), sourceID), sourceID, categoryPids)

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

	results, err := scanListSnapshots(applyStrategy(query, true).Limit(limit))
	if err != nil {
		log.Println("[Snapshot] 获取轮播候选集异常:", err)
		return []model.FilmListSnapshot{}
	}
	if len(results) == 0 && strategy == "score_random" {
		log.Printf("[Snapshot] 高分候选池为空，已回退为不加评分过滤的候选池")
		fallback := applyBannerCategories(applySourceMembership(liveFilmQuery(), sourceID), sourceID, categoryPids)
		results, err = scanListSnapshots(applyStrategy(fallback, false).Limit(limit))
		if err != nil {
			log.Println("[Snapshot] 获取轮播候选集回退异常:", err)
			return []model.FilmListSnapshot{}
		}
	}
	return results
}

// GetSnapshotHDBackdropCandidates 获取已有高清横图的候选。sourceID 为空时不限站。
func GetSnapshotHDBackdropCandidates(version string, sourceID string, categoryPids []int64, limit int) []model.FilmListSnapshot {
	version = strings.TrimSpace(version)
	if version == "" {
		version = GetActiveSnapshotVersion()
	}
	if limit <= 0 || db.Mdb == nil {
		return []model.FilmListSnapshot{}
	}
	query := applyBannerCategories(applySourceMembership(liveFilmQuery(), sourceID), sourceID, categoryPids).
		Where("picture_slide != '' OR custom_picture_slide != '' OR is_custom_picture = 1")
	results, _ := scanListSnapshots(query.Order("hits DESC, update_stamp DESC").Limit(limit))
	return results
}

// applyBannerCategories 有采集站时按该站 type_id 匹配分类键。没有采集站时仍按展示分类 pid。
func applyBannerCategories(query *gorm.DB, sourceID string, categoryIDs []int64) *gorm.DB {
	if query == nil || len(categoryIDs) == 0 {
		return query
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return query.Where("pid IN ?", categoryIDs)
	}
	typeIDs := make([]int64, 0, len(categoryIDs))
	seen := make(map[int64]struct{}, len(categoryIDs))
	for _, id := range categoryIDs {
		for _, typeID := range repository.PublicSourceTypeIDs(sourceID, "pid", id) {
			if typeID <= 0 {
				continue
			}
			if _, ok := seen[typeID]; ok {
				continue
			}
			seen[typeID] = struct{}{}
			typeIDs = append(typeIDs, typeID)
		}
	}
	if len(typeIDs) == 0 {
		return query.Where("1 = 0")
	}
	return filmquery.ApplySourceTypeMatchIDs(query, sourceID, "pid", typeIDs)
}

// FilterSnapshotsByPlaySource 只保留指定采集站有播放线路的影片。sourceID 为空时原样返回。
func FilterSnapshotsByPlaySource(sourceID string, snaps []model.FilmListSnapshot) []model.FilmListSnapshot {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || len(snaps) == 0 || db.Mdb == nil {
		return snaps
	}
	mids := make([]int64, 0, len(snaps))
	for _, snap := range snaps {
		if snap.Mid > 0 {
			mids = append(mids, snap.Mid)
		}
	}
	if len(mids) == 0 {
		return []model.FilmListSnapshot{}
	}
	var owned []int64
	if err := liveFilmQuery().Where("mid IN ?", mids).Where(model.FilmHasPlaySourceSQL(), sourceID, "play").Pluck("mid", &owned).Error; err != nil {
		log.Println("[Snapshot] 按采集站过滤轮播影片失败:", err)
		return []model.FilmListSnapshot{}
	}
	keep := make(map[int64]struct{}, len(owned))
	for _, mid := range owned {
		keep[mid] = struct{}{}
	}
	out := make([]model.FilmListSnapshot, 0, len(owned))
	for _, snap := range snaps {
		if _, ok := keep[snap.Mid]; ok {
			out = append(out, snap)
		}
	}
	return out
}
