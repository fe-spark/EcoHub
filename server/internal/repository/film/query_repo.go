package film

import (
	"errors"

	"server/internal/infra/db"
	"server/internal/model"
)

// ErrDatabaseUnavailable 数据库连接未就绪。
var ErrDatabaseUnavailable = errors.New("数据库未连接")

func GetFilmIndexById(id int64) *model.FilmIndex {
	row, err := FindFilmIndex(id)
	if err != nil {
		return nil
	}
	return &row
}

// FindFilmIndex 按 mid 读取影片档案。
func FindFilmIndex(mid int64) (model.FilmIndex, error) {
	if db.Mdb == nil {
		return model.FilmIndex{}, ErrDatabaseUnavailable
	}
	var row model.FilmIndex
	err := db.Mdb.Where("mid = ?", mid).First(&row).Error
	return row, err
}

// ListFilmIndexBrief 读取挂接播放线路需要的档案字段。
func ListFilmIndexBrief(mids []int64) ([]model.FilmIndex, error) {
	if db.Mdb == nil {
		return nil, ErrDatabaseUnavailable
	}
	var films []model.FilmIndex
	err := db.Mdb.Select("mid", "first_source_id", "db_id", "name", "pid").Where("mid IN ?", mids).Find(&films).Error
	return films, err
}

// ListPlaylistsByMid 读取一部影片的全部线路。
func ListPlaylistsByMid(mid int64) ([]model.FilmSourcePlaylist, error) {
	if db.Mdb == nil {
		return nil, ErrDatabaseUnavailable
	}
	var rows []model.FilmSourcePlaylist
	err := db.Mdb.Where("mid = ?", mid).Order("line_kind ASC, group_index ASC").Find(&rows).Error
	return rows, err
}

// ListPlayPlaylistsByMids 批量读取播放线路。
func ListPlayPlaylistsByMids(mids []int64) ([]model.FilmSourcePlaylist, error) {
	if db.Mdb == nil {
		return nil, ErrDatabaseUnavailable
	}
	var rows []model.FilmSourcePlaylist
	err := db.Mdb.Where("mid IN ? AND line_kind = ?", mids, model.LineKindPlay).
		Order("mid ASC, group_index ASC").
		Find(&rows).Error
	return rows, err
}
