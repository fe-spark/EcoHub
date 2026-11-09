package film

import (
	"context"
	"fmt"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	filmshared "server/internal/repository/film/shared"
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

// LiveBannerSnapshot 轮播实时关联的影片动态信息
type LiveBannerSnapshot struct {
	Mid                int64
	Name               string
	Year               int64
	CName              string
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
	IsScraped          bool
	TmdbID             int64
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
// sourceID 非空时只返回在该站有播放线路的影片。查询出错时返回 error，调用方不要把空结果当成“没有影片”。
func LiveBannerSnapshotsByMIDs(ctx context.Context, sourceID string, mids []int64) (map[int64]LiveBannerSnapshot, error) {
	out := make(map[int64]LiveBannerSnapshot, len(mids))
	if len(mids) == 0 || db.Mdb == nil {
		return out, nil
	}
	if ctx == nil {
		ctx = context.Background()
	}

	type row struct {
		Mid                int64
		FirstSourceId      string
		DbId               int64
		Name               string
		Pid                int64
		Year               int64
		CName              string
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
	query := db.Mdb.WithContext(ctx).Model(&model.FilmIndex{}).
		Select("mid, first_source_id, db_id, name, pid, year, c_name, remarks, area, class_tag, actor, director, blurb, score, hits, picture, picture_slide, custom_picture, custom_picture_slide, is_custom_picture").
		Where("mid IN ?", mids)
	if sourceID = strings.TrimSpace(sourceID); sourceID != "" {
		query = query.Where(model.FilmHasPlaySourceSQL(), sourceID, model.LineKindPlay)
	}
	if err := query.Scan(&rows).Error; err != nil {
		return nil, fmt.Errorf("读取轮播影片失败: %w", err)
	}

	for _, r := range rows {
		snap := LiveBannerSnapshot{
			Mid:                r.Mid,
			Name:               strings.TrimSpace(r.Name),
			Year:               r.Year,
			CName:              strings.TrimSpace(r.CName),
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
		out[r.Mid] = snap
	}
	posters := make([]model.FilmListSnapshot, 0, len(rows))
	for _, r := range rows {
		current := out[r.Mid]
		posters = append(posters, model.FilmListSnapshot{
			Mid:             r.Mid,
			SourceId:        r.FirstSourceId,
			DbId:            r.DbId,
			Name:            r.Name,
			Pid:             r.Pid,
			Picture:         current.Picture,
			PictureSlide:    current.PictureSlide,
			IsCustomPicture: current.IsCustomPicture,
		})
	}
	for _, snap := range filmshared.ApplyPosterSourcePictures(posters) {
		current := out[snap.Mid]
		if current.Picture == snap.Picture && current.PictureSlide == snap.PictureSlide && current.IsCustomPicture == snap.IsCustomPicture {
			continue
		}
		current.Picture = snap.Picture
		current.PictureSlide = snap.PictureSlide
		current.CustomPicture = snap.CustomPicture
		current.CustomPictureSlide = snap.CustomPictureSlide
		current.IsCustomPicture = snap.IsCustomPicture
		out[snap.Mid] = current
	}
	return out, nil
}

// LiveUpdateRemarksByMIDs 从活跃快照读取展示用更新状态（毫秒级索引直查）。
func LiveUpdateRemarksByMIDs(mids []int64) map[int64]string {
	snaps, err := LiveBannerSnapshotsByMIDs(context.Background(), "", mids)
	if err != nil {
		return map[int64]string{}
	}
	out := make(map[int64]string, len(snaps))
	for mid, s := range snaps {
		if s.Remarks != "" {
			out[mid] = s.Remarks
		}
	}
	return out
}
