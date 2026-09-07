package notify

import (
	"server/internal/infra/db"
	"server/internal/model"
)

// resolveFilmNames 批量查询 mid 对应片名（走 film_index 主键索引）。
func resolveFilmNames(mids []int64) map[int64]string {
	out := make(map[int64]string, len(mids))
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
		var rows []struct {
			Mid  int64  `gorm:"column:mid"`
			Name string `gorm:"column:name"`
		}
		if err := db.Mdb.Table(model.TableFilmIndex).Select("mid, name").Where("mid IN ?", uniq[start:end]).Scan(&rows).Error; err != nil {
			continue
		}
		for _, r := range rows {
			if r.Mid > 0 {
				out[r.Mid] = r.Name
			}
		}
	}
	return out
}
