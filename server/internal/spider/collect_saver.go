package spider

import (
	"context"

	"server/internal/model"
	"server/internal/repository/film/writer"
	"server/internal/spider/scheduler"
)

func saveCollectedFilmForCollect(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error) {
	return writer.SaveCollectedPeerDetails(ctx, s, page, list)
}
