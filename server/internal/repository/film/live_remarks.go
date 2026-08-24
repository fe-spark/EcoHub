package film

import (
	"encoding/json"
	"fmt"
	"log"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
)

type liveProgress struct {
	count         int
	last          string
	masterCount   int
	masterRemarks string
}

func (p *liveProgress) note(count int, last string) {
	if count > p.count {
		p.count = count
		p.last = last
	}
}

// pickLiveRemark 轮播更新状态与最近更新同源：全库最大集数所在进度。
// 主站集数未落后时用主站 Remarks；附属站先追到更多集时按该线路最后一集生成文案。
func pickLiveRemark(masterCount int, masterRemarks, leadingLast string, globalMax int) string {
	masterRemarks = strings.TrimSpace(masterRemarks)
	if globalMax <= 0 {
		return masterRemarks
	}
	if masterCount >= globalMax && masterRemarks != "" {
		return masterRemarks
	}
	return formatLiveRemark(leadingLast, globalMax)
}

func formatLiveRemark(last string, n int) string {
	last = strings.TrimSpace(last)
	if last == "" {
		if n > 0 {
			return fmt.Sprintf("更新至%d集", n)
		}
		return ""
	}
	if strings.Contains(last, "更新") || strings.Contains(last, "完结") || last == "正片" || last == "HD" {
		return last
	}
	return "更新至" + last
}

// LiveUpdateRemarksByMIDs 按全库主站详情 + 附属站 playlist 最大集数生成展示用更新状态。
func LiveUpdateRemarksByMIDs(mids []int64) map[int64]string {
	out := make(map[int64]string, len(mids))
	if len(mids) == 0 || db.Mdb == nil {
		return out
	}
	prog := make(map[int64]*liveProgress, len(mids))
	ensure := func(mid int64) *liveProgress {
		p := prog[mid]
		if p == nil {
			p = &liveProgress{}
			prog[mid] = p
		}
		return p
	}

	var detailRows []model.MovieDetailInfo
	if err := db.Mdb.Where("mid IN ?", mids).Find(&detailRows).Error; err != nil {
		log.Printf("[Film] LiveUpdateRemarks 读详情失败: %v", err)
	} else {
		for _, row := range detailRows {
			if row.Mid <= 0 || strings.TrimSpace(row.Content) == "" {
				continue
			}
			var detail model.MovieDetail
			if err := json.Unmarshal([]byte(row.Content), &detail); err != nil {
				continue
			}
			p := ensure(row.Mid)
			p.masterRemarks = strings.TrimSpace(detail.Remarks)
			p.masterCount = maxEpisodeCount(extractEpisodeCountsFromDetail(detail))
			for _, group := range detail.PlayList {
				n := episodeCount(group)
				if n > 0 {
					p.note(n, lastEpisodeLabel(group))
				}
			}
		}
	}

	keysByMid := loadMovieMatchKeysByMidsTx(db.Mdb, mids)
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
		var playlistRows []model.MoviePlaylist
		if err := db.Mdb.Where("movie_key IN ?", allKeys).Find(&playlistRows).Error; err != nil {
			log.Printf("[Film] LiveUpdateRemarks 读 playlist 失败: %v", err)
		} else {
			for _, row := range playlistRows {
				mid := keyToMid[row.MovieKey]
				if mid <= 0 || strings.TrimSpace(row.Content) == "" {
					continue
				}
				var links []model.MovieUrlInfo
				if err := json.Unmarshal([]byte(row.Content), &links); err != nil {
					continue
				}
				n := episodeCount(links)
				if n > 0 {
					ensure(mid).note(n, lastEpisodeLabel(links))
				}
			}
		}
	}

	for mid, p := range prog {
		if p == nil {
			continue
		}
		out[mid] = pickLiveRemark(p.masterCount, p.masterRemarks, p.last, p.count)
	}
	return out
}
