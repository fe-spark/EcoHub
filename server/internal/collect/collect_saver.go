package collect

import (
	"context"

	"server/internal/collect/scheduler"
	"server/internal/film/writer"
	"server/internal/model"
)

func saveCollectedFilmForCollect(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error) {
	return writer.SaveCollectedPeerDetails(ctx, s, page, list)
}
