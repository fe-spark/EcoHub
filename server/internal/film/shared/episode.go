package shared

import (
	"encoding/json"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// EpisodeCount 单条线路有效分集数（非空 Episode 计数）。
func EpisodeCount(links []model.MovieUrlInfo) int {
	n := 0
	for _, link := range links {
		if strings.TrimSpace(link.Episode) != "" {
			n++
		}
	}
	return n
}

// MaxEpisodeCount 取多条线路中的最大分集数。
func MaxEpisodeCount(counts []int) int {
	max := 0
	for _, c := range counts {
		if c > max {
			max = c
		}
	}
	return max
}

// IsEpisodeCountHigher 新数据最大集数是否严格大于已有全库最大集数。
// 历史无线路（新片）且新数据有分集 -> true；数量未增加 -> false。
func IsEpisodeCountHigher(newCounts []int, existingCounts []int) bool {
	newMax := MaxEpisodeCount(newCounts)
	if newMax <= 0 {
		return false
	}
	existMax := MaxEpisodeCount(existingCounts)
	return newMax > existMax
}

// ExtractEpisodeCountsFromDetail 主站详情各线路分集数。
func ExtractEpisodeCountsFromDetail(d model.MovieDetail) []int {
	counts := make([]int, 0, len(d.PlayList))
	for _, group := range d.PlayList {
		if n := EpisodeCount(group); n > 0 {
			counts = append(counts, n)
		}
	}
	return counts
}

// ExtractEpisodeCountsFromContents 附属源 playlist 各线路分集数。
func ExtractEpisodeCountsFromContents(contents []string) []int {
	counts := make([]int, 0, len(contents))
	for _, content := range contents {
		var links []model.MovieUrlInfo
		if err := json.Unmarshal([]byte(content), &links); err == nil {
			if n := EpisodeCount(links); n > 0 {
				counts = append(counts, n)
			}
		}
	}
	return counts
}

// LoadExistingEpisodeCountsByMIDs 查库获取 mids 在写库前各源播放线路分集数。
// excludeSourceID 非空时跳过该源自己的 playlist，避免把本源旧数据当全局基准。
func LoadExistingEpisodeCountsByMIDs(tx *gorm.DB, mids []int64, excludeSourceID string) (map[int64][]int, error) {
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

	q := tx.Model(&model.FilmSourcePlaylist{}).
		Select("mid, episode_count").
		Where("mid IN ? AND line_kind = ?", mids, "play")
	if excludeSourceID != "" {
		q = q.Where("source_id <> ?", excludeSourceID)
	}

	type playlistRow struct {
		Mid          int64
		EpisodeCount int
	}
	var rows []playlistRow
	if err := q.Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, r := range rows {
		out[r.Mid] = append(out[r.Mid], r.EpisodeCount)
	}
	return out, nil
}
