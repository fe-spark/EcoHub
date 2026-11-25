package film

import (
	"context"
	"strings"

	"server/internal/film/catalog"
	"server/internal/model"
)

// AssembleBanners 用片库数据组装轮播行。影片不在片库、或在该站没有播放线路时视为不可用：
// 首页跳过；后台保留并标记，便于删除和排序。读路径不删数据。
func AssembleBanners(ctx context.Context, slots []model.BannerSlot, keepUnavailable bool) ([]model.BannerAdminItem, error) {
	out := make([]model.BannerAdminItem, 0, len(slots))
	if len(slots) == 0 {
		return out, nil
	}
	midsBySource := make(map[string][]int64)
	for _, slot := range slots {
		midsBySource[slot.SourceId] = append(midsBySource[slot.SourceId], slot.Mid)
	}
	live := make(map[string]map[int64]LiveBannerSnapshot, len(midsBySource))
	for sourceID, mids := range midsBySource {
		snaps, err := LiveBannerSnapshotsByMIDs(ctx, sourceID, mids)
		if err != nil {
			return nil, err
		}
		live[sourceID] = snaps
	}
	for _, slot := range slots {
		snap, ok := live[slot.SourceId][slot.Mid]
		if !ok {
			if !keepUnavailable {
				continue
			}
			item := assembleBanner(slot, LiveBannerSnapshot{Mid: slot.Mid})
			item.Unavailable = true
			out = append(out, item)
			continue
		}
		out = append(out, assembleBanner(slot, snap))
	}
	for i := range out {
		out[i].Sort = int64(i + 1)
	}
	return out, nil
}

func assembleBanner(slot model.BannerSlot, snap LiveBannerSnapshot) model.BannerAdminItem {
	libPoster := strings.TrimSpace(snap.DisplayPicture())
	libSlide := strings.TrimSpace(snap.DisplayPictureSlide())
	if libSlide == libPoster {
		libSlide = ""
	}
	library := model.BannerValues{
		Poster: libPoster, Backdrop: libSlide, Blurb: snap.Blurb, Score: snap.Score, Year: snap.Year,
	}
	overrides := model.BannerValues{
		Poster: slot.PosterOverride, Backdrop: slot.BackdropOverride, Blurb: slot.BlurbOverride,
		Score: slot.ScoreOverride, Year: slot.YearOverride,
	}

	custom := slot.ImageMode == model.BannerImageCustom
	poster, slide, customPic := libPoster, libSlide, ""
	if custom {
		if slot.PosterOverride != "" {
			poster = slot.PosterOverride
			customPic = slot.PosterOverride
		}
		if slot.BackdropOverride != "" {
			slide = slot.BackdropOverride
		}
	}
	if slide == poster {
		slide = ""
	}
	blurb := snap.Blurb
	if slot.BlurbOverride != "" {
		blurb = slot.BlurbOverride
	}
	score := snap.Score
	if slot.ScoreOverride > 0 {
		score = slot.ScoreOverride
	}
	year := snap.Year
	if slot.YearOverride > 0 {
		year = slot.YearOverride
	}
	imageMode := model.BannerImageFollow
	if custom {
		imageMode = model.BannerImageCustom
	}
	return model.BannerAdminItem{
		Banner: model.Banner{
			Id:            slot.Id,
			Mid:           slot.Mid,
			Name:          snap.Name,
			Year:          year,
			CName:         snap.CName,
			Poster:        poster,
			Picture:       poster,
			PictureSlide:  slide,
			CustomPicture: customPic,
			Remark:        snap.Remarks,
			Sort:          slot.Sort,
			IsCustomPic:   custom,
			Area:          snap.Area,
			ClassTag:      snap.ClassTag,
			Actor:         snap.Actor,
			Director:      snap.Director,
			Blurb:         blurb,
			Score:         score,
			TmdbID:        slot.TmdbID,
			IsScraped:     slot.TmdbID > 0,
		},
		SourceId:  slot.SourceId,
		ImageMode: imageMode,
		Overrides: overrides,
		Library:   library,
	}
}

func checkFilmOnSource(mid int64, sourceID string) error {
	if mid <= 0 {
		return bannerInputErr("请选择影片")
	}
	ok, err := catalog.FilmsOnSource(context.Background(), sourceID, []int64{mid})
	if err != nil {
		return err
	}
	if !ok[mid] {
		return bannerInputErr("影片不存在或不属于该采集站")
	}
	return nil
}
