package writer

import (
	"log"

	"server/internal/model"
	"server/internal/spider/scheduler"
)

func savePeerFilmsIsolated(source *model.FilmSource, page int, details []model.MovieDetail) (scheduler.Mids, error) {
	affected := make([]int64, 0, len(details))
	notify := make([]int64, 0, len(details))
	unmapped := 0
	for _, detail := range details {
		mid, isNew, playChanged, stored, err := savePeerFilm(source, detail)
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
			return scheduler.Mids{}, err
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
	return scheduler.Mids{
		Affected: uniqueMIDs(affected),
		Notify:   uniqueMIDs(notify),
		Unmapped: unmapped,
	}, nil
}
