package collect

import (
	"context"

	"server/internal/model"
	"server/internal/film/writer"
	"server/internal/collect/scheduler"
)

func saveCollectedFilmForCollect(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error) {
	return writer.SaveCollectedPeerDetails(ctx, s, page, list)
}
