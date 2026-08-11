package film

import (
	"encoding/json"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// episodeCount 单条线路有效分集数（非空 Episode 计数）。
func episodeCount(links []model.MovieUrlInfo) int {
	n := 0
	for _, link := range links {
		if strings.TrimSpace(link.Episode) != "" {
			n++
		}
	}
	return n
}

// maxEpisodeCount 取多条线路中的最大分集数。
func maxEpisodeCount(counts []int) int {
	max := 0
	for _, c := range counts {
		if c > max {
			max = c
		}
	}
	return max
}

// isEpisodeCountHigher 新数据最大集数是否严格大于已有全库最大集数。
// 历史无线路（新片）且新数据有分集 -> true；数量未增加 -> false。
func isEpisodeCountHigher(newCounts []int, existingCounts []int) bool {
	newMax := maxEpisodeCount(newCounts)
	if newMax <= 0 {
		return false
	}
	existMax := maxEpisodeCount(existingCounts)
	return newMax > existMax
}

// extractEpisodeCountsFromDetail 主站详情各线路分集数。
func extractEpisodeCountsFromDetail(d model.MovieDetail) []int {
	counts := make([]int, 0, len(d.PlayList))
	for _, group := range d.PlayList {
		if n := episodeCount(group); n > 0 {
			counts = append(counts, n)
		}
	}
	return counts
}

// extractEpisodeCountsFromPlaylistSignatures 附属源 playlist 各线路分集数。
func extractEpisodeCountsFromPlaylistSignatures(sigs []playlistSignature) []int {
	counts := make([]int, 0, len(sigs))
	for _, s := range sigs {
		var links []model.MovieUrlInfo
		if err := json.Unmarshal([]byte(s.Content), &links); err == nil {
			if n := episodeCount(links); n > 0 {
				counts = append(counts, n)
			}
		}
	}
	return counts
}

// loadExistingEpisodeCountsByMIDs 查库获取 mids 在写库前主站与其它源的各线路分集数。
// excludeSourceID 非空时跳过该源自己的 playlist，避免把本源旧数据当全局基准。
func loadExistingEpisodeCountsByMIDs(tx *gorm.DB, mids []int64, excludeSourceID string) (map[int64][]int, error) {
	out := make(map[int64][]int, len(mids))
	if len(mids) == 0 {
		return out, nil
	}
	if tx == nil {
		tx = db.Mdb
	}
	if tx == nil {
		return out, nil
	}
	excludeSourceID = strings.TrimSpace(excludeSourceID)

	var detailRows []model.MovieDetailInfo
	if err := tx.Where("mid IN ?", mids).Find(&detailRows).Error; err == nil {
		for _, row := range detailRows {
			if strings.TrimSpace(row.Content) == "" {
				continue
			}
			var detail model.MovieDetail
			if err := json.Unmarshal([]byte(row.Content), &detail); err == nil {
				counts := extractEpisodeCountsFromDetail(detail)
				out[row.Mid] = append(out[row.Mid], counts...)
			}
		}
	}

	keysByMid := loadMovieMatchKeysByMidsTx(tx, mids)
	allKeys := make([]string, 0)
	keyToMid := make(map[string]int64)
	for mid, keys := range keysByMid {
		for _, k := range keys {
			if k != "" {
				allKeys = append(allKeys, k)
				keyToMid[k] = mid
			}
		}
	}
	if len(allKeys) > 0 {
		q := tx.Where("movie_key IN ?", allKeys)
		if excludeSourceID != "" {
			q = q.Where("source_id <> ?", excludeSourceID)
		}
		var playlistRows []model.MoviePlaylist
		if err := q.Find(&playlistRows).Error; err == nil {
			for _, row := range playlistRows {
				if strings.TrimSpace(row.Content) == "" {
					continue
				}
				mid := keyToMid[row.MovieKey]
				if mid <= 0 {
					continue
				}
				var links []model.MovieUrlInfo
				if err := json.Unmarshal([]byte(row.Content), &links); err == nil {
					if n := episodeCount(links); n > 0 {
						out[mid] = append(out[mid], n)
					}
				}
			}
		}
	}

	return out, nil
}
