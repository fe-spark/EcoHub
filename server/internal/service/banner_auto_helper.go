package service

import (
	"log"
	"math/rand"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/utils"
)

func normalizeBannerTargetCount(count int) int {
	if count <= 0 {
		return repository.DefaultBannerCount
	}
	if count > repository.MaxBannerCount {
		return repository.MaxBannerCount
	}
	return count
}

// extractMainTitle 提取主片名，去除尾部或内部的副标题/外文原名括号，例如 "杀手妈咪（유부녀 킬러）" -> "杀手妈咪"
func extractMainTitle(s string) string {
	s = strings.TrimSpace(s)
	if idx := strings.IndexAny(s, "(（[【"); idx > 0 {
		prefix := strings.TrimSpace(s[:idx])
		if prefix != "" {
			return prefix
		}
	}
	return s
}

// replaceMissingSlides 优先用全新候选中已有横图顶替；全新池耗尽才回退当前在展旧片。
func replaceMissingSlides(
	picked []model.FilmListSnapshot,
	freshCandidates []model.FilmListSnapshot,
	existingCandidates []model.FilmListSnapshot,
	pinnedMap map[int64]struct{},
	r *rand.Rand,
) ([]model.FilmListSnapshot, int) {
	if r == nil {
		r = rand.New(rand.NewSource(time.Now().UnixNano()))
	}

	existingPickedMap := make(map[int64]struct{}, len(picked)+len(pinnedMap))
	for k := range pinnedMap {
		existingPickedMap[k] = struct{}{}
	}
	for _, s := range picked {
		if s.Mid > 0 {
			existingPickedMap[s.Mid] = struct{}{}
		}
	}

	existingMidSet := make(map[int64]struct{}, len(existingCandidates))
	for _, e := range existingCandidates {
		if e.Mid > 0 {
			existingMidSet[e.Mid] = struct{}{}
		}
	}

	pickWithSlide := func(pool []model.FilmListSnapshot) []model.FilmListSnapshot {
		out := make([]model.FilmListSnapshot, 0)
		for _, c := range pool {
			if c.Mid <= 0 {
				continue
			}
			if _, exists := existingPickedMap[c.Mid]; exists {
				continue
			}
			if strings.TrimSpace(c.DisplayPictureSlide()) != "" {
				out = append(out, c)
			}
		}
		r.Shuffle(len(out), func(i, j int) { out[i], out[j] = out[j], out[i] })
		return out
	}

	freshRepl := pickWithSlide(freshCandidates)
	oldRepl := pickWithSlide(existingCandidates)

	extraReused := 0
	fi, oi := 0, 0
	for i, snap := range picked {
		if strings.TrimSpace(snap.DisplayPictureSlide()) != "" {
			continue
		}
		var sub model.FilmListSnapshot
		switch {
		case fi < len(freshRepl):
			sub = freshRepl[fi]
			fi++
		case oi < len(oldRepl):
			sub = oldRepl[oi]
			oi++
			if _, wasExisting := existingMidSet[snap.Mid]; !wasExisting {
				extraReused++
			}
		default:
			continue
		}
		existingPickedMap[sub.Mid] = struct{}{}
		log.Printf("[BannerAuto] 影片 [%s](mid=%d) 未能刮削到横屏大图，已自动挑选已有横图影片 [%s](mid=%d) 顶替",
			snap.Name, snap.Mid, sub.Name, sub.Mid)
		picked[i] = sub
	}
	return picked, extraReused
}

func bannerFromSnapshot(snap model.FilmListSnapshot, sortOrder int, fallbackPosterSlide bool) model.Banner {
	pic := strings.TrimSpace(snap.DisplayPicture())
	slide := strings.TrimSpace(snap.DisplayPictureSlide())
	if fallbackPosterSlide && slide == "" && pic != "" {
		slide = pic
	}

	return model.Banner{
		Id:            utils.GenerateSalt(),
		Mid:           snap.Mid,
		Name:          snap.Name,
		Year:          snap.Year,
		CName:         snap.CName,
		Poster:        pic,
		Picture:       pic,
		PictureSlide:  slide,
		CustomPicture: pic,
		Remark:        snap.Remarks,
		Sort:          int64(sortOrder),
		IsCustomPic:   true,
	}
}
