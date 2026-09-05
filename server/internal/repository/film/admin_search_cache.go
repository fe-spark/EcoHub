package film

import (
	"server/internal/model"
)

func GetSearchPageFast(s model.SearchVo) []model.FilmIndex {
	return GetSearchPageReadModel(s)
}

func ClearAdminFilmSearchCache() {}
