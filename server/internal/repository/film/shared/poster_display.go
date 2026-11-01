package shared

import (
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

type posterPicture struct {
	picture string
	slide   string
}

// ApplyPosterSourcePictures 前台封面改用海报源那部影片的最终展示图。
// 所属站就是海报源时，保持传入的图。对不上则不动。对方某一侧为空时，保留本片这一侧。
func ApplyPosterSourcePictures(snaps []model.FilmListSnapshot) []model.FilmListSnapshot {
	if len(snaps) == 0 || db.Mdb == nil {
		return snaps
	}
	posterID := activePosterSourceID(db.Mdb)
	if posterID == "" {
		return snaps
	}
	films := make([]model.FilmIndex, 0, len(snaps))
	for _, snap := range snaps {
		if snap.Mid <= 0 || strings.TrimSpace(snap.SourceId) == posterID {
			continue
		}
		films = append(films, model.FilmIndex{
			FilmIndexIdentity: model.FilmIndexIdentity{Mid: snap.Mid, FirstSourceId: snap.SourceId, DbId: snap.DbId},
			FilmIndexContent:  model.FilmIndexContent{Name: snap.Name},
			FilmIndexCategory: model.FilmIndexCategory{Pid: snap.Pid},
		})
	}
	hits := posterPicturesFor(db.Mdb, films, posterID)
	if len(hits) == 0 {
		return snaps
	}
	out := append([]model.FilmListSnapshot(nil), snaps...)
	for i := range out {
		hit, ok := hits[out[i].Mid]
		if !ok || hit.picture == "" {
			continue
		}
		out[i].Picture = hit.picture
		if hit.slide != "" {
			out[i].PictureSlide = hit.slide
		} else if kept := strings.TrimSpace(out[i].DisplayPictureSlide()); kept != "" {
			out[i].PictureSlide = kept
		}
		out[i].CustomPicture = ""
		out[i].CustomPictureSlide = ""
		out[i].IsCustomPicture = false
	}
	return out
}

func activePosterSourceID(tx *gorm.DB) string {
	if tx == nil {
		return ""
	}
	var marked model.FilmSource
	if err := tx.Where("is_poster_source = ? AND state = ?", true, true).
		Order("sort ASC, created_at ASC, id ASC").
		First(&marked).Error; err == nil {
		return strings.TrimSpace(marked.Id)
	}
	var fallback model.FilmSource
	if err := tx.Where("state = ?", true).
		Order("sort ASC, created_at ASC, id ASC").
		First(&fallback).Error; err == nil {
		return strings.TrimSpace(fallback.Id)
	}
	return ""
}

func posterPicturesFor(tx *gorm.DB, films []model.FilmIndex, posterID string) map[int64]posterPicture {
	out := map[int64]posterPicture{}
	if tx == nil || posterID == "" || len(films) == 0 {
		return out
	}
	ranks, keys := peerKeyRanks(films)
	if len(keys) == 0 {
		return out
	}
	var keyRows []model.MovieMatchKey
	if err := tx.Where("match_key IN ?", keys).Find(&keyRows).Error; err != nil || len(keyRows) == 0 {
		return out
	}
	ownerOf, err := filmOwnersTx(tx, keyRows)
	if err != nil {
		return out
	}
	chosen := choosePeerFilms(films, ranks, keyRows, ownerOf, map[string]struct{}{posterID: {}})
	type posterPair struct {
		origin int64
		peer   int64
	}
	pairs := make([]posterPair, 0, len(chosen))
	seen := map[int64]struct{}{}
	mids := make([]int64, 0, len(chosen))
	for origin, bySource := range chosen {
		pick, ok := bySource[posterID]
		if !ok || pick.mid <= 0 {
			continue
		}
		pairs = append(pairs, posterPair{origin: origin, peer: pick.mid})
		if _, ok := seen[pick.mid]; ok {
			continue
		}
		seen[pick.mid] = struct{}{}
		mids = append(mids, pick.mid)
	}
	if len(mids) == 0 {
		return out
	}
	var rows []model.FilmIndex
	if err := tx.Select("mid", "picture", "picture_slide", "custom_picture", "custom_picture_slide", "is_custom_picture").
		Where("mid IN ?", mids).Find(&rows).Error; err != nil {
		return out
	}
	byPeer := make(map[int64]posterPicture, len(rows))
	for _, row := range rows {
		byPeer[row.Mid] = posterFinal(row)
	}
	for _, pair := range pairs {
		hit, ok := byPeer[pair.peer]
		if !ok || hit.picture == "" {
			continue
		}
		out[pair.origin] = hit
	}
	return out
}

// posterFinal 与打开海报源那部影片时的封面一致。刮削结果已经写在自定义封面里。
func posterFinal(row model.FilmIndex) posterPicture {
	return posterPicture{
		picture: strings.TrimSpace(row.DisplayPicture()),
		slide:   strings.TrimSpace(row.DisplayPictureSlide()),
	}
}
