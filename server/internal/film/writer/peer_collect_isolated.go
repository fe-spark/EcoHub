package writer

import (
	"context"
	"log"
	"reflect"

	"server/internal/model"
)

func savePeerFilmsIsolated(ctx context.Context, source *model.FilmSource, page int, details []model.MovieDetail) (model.Mids, error) {
	affected := make([]int64, 0, len(details))
	notify := make([]int64, 0, len(details))
	unmapped := 0
	useCustomSave := reflect.ValueOf(savePeerFilm).Pointer() != defaultSavePeerFilmPtr
	for _, detail := range details {
		if ctx != nil && ctx.Err() != nil {
			return model.Mids{}, ctx.Err()
		}
		var (
			mid         int64
			isNew       bool
			playChanged bool
			stored      bool
			err         error
		)
		if useCustomSave {
			mid, isNew, playChanged, stored, err = savePeerFilm(source, detail)
		} else {
			mid, isNew, playChanged, stored, err = saveSinglePeerDetailTxCtx(ctx, source, detail)
		}
		if err != nil {
			if categorySkipKind(err) == skipKindUnmapped {
				unmapped++
				continue
			}
			if isDeterministicRowError(err) {
				log.Printf("[Spider][PeerCollect] 单片入库跳过 source=%s page=%d vod=%d name=%s err=%v",
					source.Id, page, detail.Id, detail.Name, err)
				continue
			}
			log.Printf("[Spider][PeerCollect] 单片入库失败 source=%s page=%d vod=%d err=%v",
				source.Id, page, detail.Id, err)
			return model.Mids{}, err
		}
		if mid <= 0 || !stored {
			continue
		}
		affected = append(affected, mid)
		if isNew || playChanged {
			notify = append(notify, mid)
		}
	}
	if unmapped > 0 {
		log.Printf("[Spider][PeerCollect] 分类跳过 source=%s page=%d unmapped=%d",
			source.Id, page, unmapped)
	}
	return model.Mids{
		Affected: uniqueMIDs(affected),
		Notify:   uniqueMIDs(notify),
		Unmapped: unmapped,
	}, nil
}
