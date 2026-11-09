package migration

import (
	"sort"
	"strings"

	"server/internal/model"
	"server/internal/utils"

	"gorm.io/gorm"
)

// legacyBannerTable 旧版全局轮播表。迁到 banner_slot 后删除。
const legacyBannerTable = "banners_record"

// migrateBannerSlot 把旧的全局轮播按站点迁到 banner_slot，然后删除旧表（不可逆）。
func migrateBannerSlot(gdb *gorm.DB) error {
	if gdb == nil {
		return nil
	}
	if err := gdb.AutoMigrate(&model.BannerSlot{}); err != nil {
		return err
	}
	if !gdb.Migrator().HasTable(legacyBannerTable) {
		return nil
	}
	var legacy []model.LegacyBanner
	if err := gdb.Table(legacyBannerTable).Find(&legacy).Error; err != nil {
		return err
	}
	slots, err := ConvertLegacyBanners(gdb, legacy)
	if err != nil {
		return err
	}
	return gdb.Transaction(func(tx *gorm.DB) error {
		if len(slots) > 0 {
			if err := tx.Create(&slots).Error; err != nil {
				return err
			}
		}
		return tx.Migrator().DropTable(legacyBannerTable)
	})
}

type slotKey struct {
	sourceID string
	mid      int64
}

type legacyFilm struct {
	Mid           int64
	FirstSourceId string
	Year          int64
	Score         float64
	Blurb         string
}

// ConvertLegacyBanners 把旧版全局轮播行转成按站点的轮播。
//   - 站点：影片在当前首选站有播放线路就归首选站，否则归影片的首个站点；影片已不存在的行丢弃。
//   - 同一站点同一影片只留排序靠前的一行，排序按站点重新从 1 编号。
//   - isCustomPic 或 tmdbId>0 的行设为 custom，封面和横图写成覆盖图；其余跟随片库。
//   - 简介、评分、年份只在和片库不同时写成覆盖项。
func ConvertLegacyBanners(gdb *gorm.DB, legacy []model.LegacyBanner) ([]model.BannerSlot, error) {
	if gdb == nil || len(legacy) == 0 {
		return []model.BannerSlot{}, nil
	}
	rows := append([]model.LegacyBanner(nil), legacy...)
	sort.SliceStable(rows, func(i, j int) bool { return rows[i].Sort < rows[j].Sort })

	mids := make([]int64, 0, len(rows))
	for _, r := range rows {
		if r.Mid > 0 {
			mids = append(mids, r.Mid)
		}
	}
	films := map[int64]legacyFilm{}
	if len(mids) > 0 {
		var list []legacyFilm
		if err := gdb.Model(&model.FilmIndex{}).
			Select("mid, first_source_id, year, score, blurb").
			Where("mid IN ?", mids).Scan(&list).Error; err != nil {
			return nil, err
		}
		for _, f := range list {
			films[f.Mid] = f
		}
	}

	primaryID := ""
	var primary model.FilmSource
	if err := gdb.Where("state = ?", true).Order("sort ASC, created_at ASC, id ASC").First(&primary).Error; err == nil {
		primaryID = strings.TrimSpace(primary.Id)
	}
	onPrimary := map[int64]bool{}
	if primaryID != "" && len(mids) > 0 {
		var hit []int64
		if err := gdb.Model(&model.FilmSourcePlaylist{}).
			Where("source_id = ? AND line_kind = ? AND mid IN ?", primaryID, model.LineKindPlay, mids).
			Distinct().Pluck("mid", &hit).Error; err != nil {
			return nil, err
		}
		for _, mid := range hit {
			onPrimary[mid] = true
		}
	}

	out := make([]model.BannerSlot, 0, len(rows))
	seen := map[slotKey]bool{}
	nextSort := map[string]int64{}
	usedIDs := map[string]bool{}
	for _, r := range rows {
		film, ok := films[r.Mid]
		if !ok {
			continue
		}
		sourceID := strings.TrimSpace(film.FirstSourceId)
		if onPrimary[r.Mid] {
			sourceID = primaryID
		}
		if sourceID == "" {
			continue
		}
		key := slotKey{sourceID, r.Mid}
		if seen[key] {
			continue
		}
		seen[key] = true

		id := strings.TrimSpace(r.Id)
		if id == "" || usedIDs[id] {
			gen, err := utils.GenerateSalt()
			if err != nil {
				return nil, err
			}
			id = gen
		}
		usedIDs[id] = true
		nextSort[sourceID]++

		slot := model.BannerSlot{
			Id:        id,
			SourceId:  sourceID,
			Mid:       r.Mid,
			Sort:      nextSort[sourceID],
			ImageMode: model.BannerImageFollow,
			TmdbID:    r.TmdbID,
		}
		if r.IsCustomPic || r.TmdbID > 0 {
			slot.ImageMode = model.BannerImageCustom
			poster := firstNonEmpty(r.CustomPicture, r.Picture, r.Poster)
			slot.PosterOverride = poster
			if slide := strings.TrimSpace(r.PictureSlide); slide != "" && slide != poster {
				slot.BackdropOverride = slide
			}
		}
		if blurb := strings.TrimSpace(r.Blurb); blurb != "" && blurb != strings.TrimSpace(film.Blurb) {
			slot.BlurbOverride = blurb
		}
		if r.Score > 0 && r.Score != film.Score {
			slot.ScoreOverride = r.Score
		}
		if r.Year > 0 && r.Year != film.Year {
			slot.YearOverride = r.Year
		}
		out = append(out, slot)
	}
	return out, nil
}

func firstNonEmpty(values ...string) string {
	for _, v := range values {
		if v = strings.TrimSpace(v); v != "" {
			return v
		}
	}
	return ""
}
