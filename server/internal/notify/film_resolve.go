package notify

import (
	"server/internal/infra/db"
	"server/internal/model"
)

type filmMeta struct {
	Name        string
	UpdateStamp int64
}

// ResolveFilmMeta 批量查询 mid → 片名与 update_stamp。
func ResolveFilmMeta(mids []int64) map[int64]filmMeta {
	out := make(map[int64]filmMeta, len(mids))
	if len(mids) == 0 || db.Mdb == nil {
		return out
	}
	uniq := make([]int64, 0, len(mids))
	seen := make(map[int64]struct{}, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		uniq = append(uniq, mid)
	}
	const chunk = 200
	for start := 0; start < len(uniq); start += chunk {
		end := start + chunk
		if end > len(uniq) {
			end = len(uniq)
		}
		var rows []model.FilmIndex
		if err := db.Mdb.Select("mid", "name", "update_stamp").Where("mid IN ?", uniq[start:end]).Find(&rows).Error; err != nil {
			continue
		}
		for _, row := range rows {
			if row.Mid > 0 {
				out[row.Mid] = filmMeta{Name: row.Name, UpdateStamp: row.UpdateStamp}
			}
		}
	}
	return out
}
