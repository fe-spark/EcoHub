package spider

import (
	"context"
	"errors"
	"fmt"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/film/writer"
	"server/internal/spider/scheduler"
)

func saveCollectedFilmForCollect(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (scheduler.Mids, error) {
	return writer.SaveCollectedPeerDetails(ctx, s, page, list)
}

func saveFilmPageFailure(s *model.FilmSource, h, pg int, phase string, err error) {
	if err == nil {
		err = errors.New("unknown error")
	}
	recordErr := repository.SaveFailureRecord(model.FailureRecord{
		OriginId:   s.Id,
		OriginName: s.Name,
		Uri:        s.Uri,
		PageNumber: pg,
		Hour:       h,
		Cause:      fmt.Sprintf("%s: %v", phase, err),
		Status:     model.FailureRecordStatusPending,
	})
	if recordErr != nil {
		syslog.Errorf("[Spider][Failure] 失败页记录保存失败 source_id=%s source=%s page=%d hour=%d phase=%s err=%v record_err=%v", s.Id, s.Name, pg, h, phase, err, recordErr)
	}
}
