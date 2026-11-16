package film

import (
	"server/internal/infra/db"
	"server/internal/model"
)

// countFilms 当前主站档案数。软删除不计入。
func countFilms(primary string) (int64, error) {
	var n int64
	err := db.Mdb.Model(&model.FilmIndex{}).Where("first_source_id = ?", primary).Count(&n).Error
	return n, err
}

// countPrimaryMulti 当前主站里有多个播放源的影片数。
// 同一编号挂了两个及以上播放源，或片名在另一个采集站也有，计一次。只在库里计数，不把片名拉进内存清洗。
func countPrimaryMulti(primary string) (int64, error) {
	film := model.TableFilmIndex
	play := model.TableFilmSourcePlaylist
	var n int64
	err := db.Mdb.Raw(`
		SELECT COUNT(*) FROM (
			SELECT f.mid
			FROM `+film+` f
			JOIN `+play+` p ON p.mid = f.mid AND p.line_kind = ?
			WHERE f.deleted_at IS NULL AND f.first_source_id = ?
			GROUP BY f.mid
			HAVING COUNT(DISTINCT p.source_id) >= 2
			UNION
			SELECT f.mid
			FROM `+film+` f
			JOIN (
				SELECT name
				FROM `+film+`
				WHERE deleted_at IS NULL AND name <> '' AND first_source_id <> ''
				GROUP BY name
				HAVING COUNT(DISTINCT first_source_id) > 1
			) shared ON shared.name = f.name
			WHERE f.deleted_at IS NULL AND f.first_source_id = ?
		) multi
	`, model.LineKindPlay, primary, primary).Scan(&n).Error
	return n, err
}
