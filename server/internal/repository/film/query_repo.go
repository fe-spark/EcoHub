package film

import (
	"server/internal/infra/db"
	"server/internal/model"
)

func GetFilmIndexById(id int64) *model.FilmIndex {
	s := model.FilmIndex{}
	if err := db.Mdb.Where("mid = ?", id).First(&s).Error; err != nil {
		return nil
	}
	return &s
}
