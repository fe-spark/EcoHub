package repository

import (
	"errors"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
)

// FilmNamesByIDs 按全局影片编号读取片名。已删除的影片仍返回原名，供采集记录展示。
func FilmNamesByIDs(ids []int64) (map[int64]string, error) {
	out := make(map[int64]string)
	if len(ids) == 0 {
		return out, nil
	}
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	var rows []struct {
		Mid  int64
		Name string
	}
	if err := db.Mdb.Unscoped().Model(&model.FilmIndex{}).
		Select("mid", "name").
		Where("mid IN ?", ids).
		Find(&rows).Error; err != nil {
		return nil, err
	}
	for _, row := range rows {
		name := strings.TrimSpace(row.Name)
		if row.Mid > 0 && name != "" {
			out[row.Mid] = name
		}
	}
	return out, nil
}

// escapeLikePattern 转义 LIKE 通配符。查询须带 ESCAPE '\\'，SQLite 和 MySQL 才都按字面量匹配。
func escapeLikePattern(s string) string {
	s = strings.ReplaceAll(s, "\\", "\\\\")
	s = strings.ReplaceAll(s, "%", "\\%")
	s = strings.ReplaceAll(s, "_", "\\_")
	return s
}
