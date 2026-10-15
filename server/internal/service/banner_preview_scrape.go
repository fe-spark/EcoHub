package service

import (
	"context"
	"fmt"
	"strings"
	"time"

	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
)

// PreviewScrapeEvent 草稿批量刮削进度。只回传预览，不写片库。
type PreviewScrapeEvent struct {
	Event        string  `json:"event"`
	Current      int     `json:"current,omitempty"`
	Total        int     `json:"total,omitempty"`
	Mid          int64   `json:"mid,omitempty"`
	Name         string  `json:"name,omitempty"`
	Success      bool    `json:"success,omitempty"`
	ScrapeState  string  `json:"scrapeState,omitempty"`
	Picture      string  `json:"picture,omitempty"`
	PictureSlide string  `json:"pictureSlide,omitempty"`
	Score        float64 `json:"score,omitempty"`
	Blurb        string  `json:"blurb,omitempty"`
	Year         int64   `json:"year,omitempty"`
	TmdbID       int64   `json:"tmdbId,omitempty"`
	MediaType    string  `json:"mediaType,omitempty"`
	SuccessCount int     `json:"successCount,omitempty"`
	Error        string  `json:"error,omitempty"`
}

type PreviewScrapeReq struct {
	Mids []int64 `json:"mids"`
}

// PreviewScrapeDeckStream 对草稿中的影片做 TMDB 预览匹配。确认替换后才落库。
func (s *ManageService) PreviewScrapeDeckStream(ctx context.Context, mids []int64, onEvent func(PreviewScrapeEvent)) error {
	if ctx == nil {
		ctx = context.Background()
	}
	if err := ctx.Err(); err != nil {
		return err
	}

	unique := uniquePositiveMids(mids)
	if len(unique) == 0 {
		if onEvent != nil {
			onEvent(PreviewScrapeEvent{Event: "done", Total: 0, SuccessCount: 0})
		}
		return nil
	}

	tmdbCfg := repository.GetTMDBConfig()
	if !tmdbCfg.Enabled || strings.TrimSpace(tmdbCfg.ApiKey) == "" {
		err := fmt.Errorf("TMDB 未启用")
		if onEvent != nil {
			onEvent(PreviewScrapeEvent{Event: "error", Error: err.Error()})
		}
		return err
	}

	version := filmsnapshot.GetActiveSnapshotVersion()
	if version == "" {
		err := fmt.Errorf("当前无影片快照")
		if onEvent != nil {
			onEvent(PreviewScrapeEvent{Event: "error", Error: err.Error()})
		}
		return err
	}

	snaps := filmsnapshot.GetSnapshotsByMidsOrdered(version, unique)
	byMid := make(map[int64]int, len(snaps))
	for i := range snaps {
		byMid[snaps[i].Mid] = i
	}

	if onEvent != nil {
		onEvent(PreviewScrapeEvent{Event: "start", Current: 0, Total: len(unique)})
	}

	successCount := 0
	for i, mid := range unique {
		if err := ctx.Err(); err != nil {
			return err
		}
		idx, ok := byMid[mid]
		if !ok {
			if onEvent != nil {
				onEvent(PreviewScrapeEvent{
					Event:       "progress",
					Current:     i + 1,
					Total:       len(unique),
					Mid:         mid,
					ScrapeState: "miss",
				})
			}
			continue
		}
		snap := snaps[idx]
		if onEvent != nil {
			onEvent(PreviewScrapeEvent{
				Event:       "item_start",
				Current:     i + 1,
				Total:       len(unique),
				Mid:         mid,
				Name:        snap.Name,
				ScrapeState: "scraping",
			})
		}
		singleCtx, cancel := context.WithTimeout(ctx, 8*time.Second)
		updated, mediaType, success, err := TMDBSvc.PreviewScrapeFilm(singleCtx, snap)
		cancel()
		if err := ctx.Err(); err != nil {
			return err
		}
		evt := PreviewScrapeEvent{
			Event:       "progress",
			Current:     i + 1,
			Total:       len(unique),
			Mid:         mid,
			Name:        snap.Name,
			ScrapeState: "miss",
		}
		if err == nil && success && updated != nil {
			slide := strings.TrimSpace(updated.DisplayPictureSlide())
			pic := strings.TrimSpace(updated.DisplayPicture())
			if slide == pic {
				slide = ""
			}
			evt.Success = true
			evt.ScrapeState = "pending"
			evt.Picture = pic
			evt.PictureSlide = slide
			evt.Score = updated.Score
			evt.Blurb = updated.Blurb
			evt.Year = updated.Year
			evt.TmdbID = updated.TmdbID
			evt.MediaType = mediaType
			successCount++
		}
		if onEvent != nil {
			onEvent(evt)
		}
	}

	if err := ctx.Err(); err != nil {
		return err
	}
	if onEvent != nil {
		onEvent(PreviewScrapeEvent{
			Event:        "done",
			Current:      len(unique),
			Total:        len(unique),
			SuccessCount: successCount,
		})
	}
	return nil
}

func uniquePositiveMids(mids []int64) []int64 {
	out := make([]int64, 0, len(mids))
	seen := make(map[int64]struct{}, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		out = append(out, mid)
	}
	return out
}
