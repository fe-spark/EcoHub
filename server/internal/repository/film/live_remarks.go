package film

import (
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

// maxLiveRemarksBatchSize 单次分批查询详情与播放列表的大小，防止单次 SQL 携带过多参数和产生大内存占用
const maxLiveRemarksBatchSize = 100

// LiveBannerSnapshot 轮播实时关联的影片动态信息
type LiveBannerSnapshot struct {
	Mid                int64
	Remarks            string
	Area               string
	ClassTag           string
	Actor              string
	Director           string
	Blurb              string
	Score              float64
	Hits               int64
	Picture            string
	PictureSlide       string
	CustomPicture      string
	CustomPictureSlide string
	IsCustomPicture    bool
}

func (s LiveBannerSnapshot) DisplayPicture() string {
	if s.IsCustomPicture && len(s.CustomPicture) > 0 {
		return s.CustomPicture
	}
	return s.Picture
}

func (s LiveBannerSnapshot) DisplayPictureSlide() string {
	if s.IsCustomPicture && len(s.CustomPictureSlide) > 0 {
		return s.CustomPictureSlide
	}
	return s.PictureSlide
}

// LiveBannerSnapshotsByMIDs 从活跃快照读取展示用状态与最新海报/幻灯图（毫秒级索引直查）。
func LiveBannerSnapshotsByMIDs(mids []int64) map[int64]LiveBannerSnapshot {
	out := make(map[int64]LiveBannerSnapshot, len(mids))
	if len(mids) == 0 || db.Mdb == nil {
		return out
	}

	version := GetActiveSnapshotVersion()
	if version == "" {
		return out
	}

	type row struct {
		Mid                int64
		Remarks            string
		Area               string
		ClassTag           string
		Actor              string
		Director           string
		Blurb              string
		Score              float64
		Hits               int64
		Picture            string
		PictureSlide       string
		CustomPicture      string
		CustomPictureSlide string
		IsCustomPicture    bool
	}
	var rows []row
	if err := db.Mdb.Model(&model.FilmListSnapshot{}).
		Select("mid, remarks, area, class_tag, actor, director, blurb, score, hits, picture, picture_slide, custom_picture, custom_picture_slide, is_custom_picture").
		Where("snapshot_version = ? AND mid IN ?", version, mids).
		Scan(&rows).Error; err != nil {
		log.Printf("[Film] LiveBannerSnapshotsByMIDs 读快照状态失败: %v", err)
		return out
	}

	for _, r := range rows {
		out[r.Mid] = LiveBannerSnapshot{
			Mid:                r.Mid,
			Remarks:            strings.TrimSpace(r.Remarks),
			Area:               strings.TrimSpace(r.Area),
			ClassTag:           strings.TrimSpace(r.ClassTag),
			Actor:              strings.TrimSpace(r.Actor),
			Director:           strings.TrimSpace(r.Director),
			Blurb:              strings.TrimSpace(r.Blurb),
			Score:              r.Score,
			Hits:               r.Hits,
			Picture:            strings.TrimSpace(r.Picture),
			PictureSlide:       strings.TrimSpace(r.PictureSlide),
			CustomPicture:      strings.TrimSpace(r.CustomPicture),
			CustomPictureSlide: strings.TrimSpace(r.CustomPictureSlide),
			IsCustomPicture:    r.IsCustomPicture,
		}
	}
	return out
}

// LiveUpdateRemarksByMIDs 从活跃快照读取展示用更新状态（毫秒级索引直查）。
func LiveUpdateRemarksByMIDs(mids []int64) map[int64]string {
	snaps := LiveBannerSnapshotsByMIDs(mids)
	out := make(map[int64]string, len(snaps))
	for mid, s := range snaps {
		if s.Remarks != "" {
			out[mid] = s.Remarks
		}
	}
	return out
}
