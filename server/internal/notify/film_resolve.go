package notify

import (
	"fmt"
	"server/internal/infra/db"
	"server/internal/model"
)

// ResolveFilmNames 批量查询 mid → 片名。
func ResolveFilmNames(mids []int64) map[int64]string {
	out := make(map[int64]string, len(mids))
	if len(mids) == 0 {
		return out
	}
	// 去重
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
		if err := db.Mdb.Select("mid", "name").Where("mid IN ?", uniq[start:end]).Find(&rows).Error; err != nil {
			continue
		}
		for _, row := range rows {
			if row.Mid > 0 {
				out[row.Mid] = row.Name
			}
		}
	}
	return out
}

func filmDisplayName(mid int64, names map[int64]string) string {
	if name, ok := names[mid]; ok && name != "" {
		return name
	}
	return fmt.Sprintf("#%d", mid)
}
