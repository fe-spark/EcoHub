package film

import (
	"strings"

	"server/internal/model"
	"server/internal/repository/support"
)

func GetSearchPageFast(s model.SearchVo) []model.FilmIndex {
	return GetSearchPageReadModel(s)
}

func matchesAdminSearch(index model.FilmIndex, s model.SearchVo) bool {
	name := strings.TrimSpace(s.Name)
	if name != "" && !strings.Contains(index.Name, name) {
		return false
	}
	if !matchesAdminSearchCategory(index, s) {
		return false
	}
	if s.Plot != "" && !strings.Contains(index.ClassTag, s.Plot) {
		return false
	}
	if s.Area != "" && strings.TrimSpace(index.Area) != s.Area {
		return false
	}
	if s.Language != "" && strings.TrimSpace(index.Language) != s.Language {
		return false
	}
	if s.Year > 0 && index.Year != s.Year {
		return false
	}
	if s.BeginTime > 0 && index.UpdateStamp < s.BeginTime {
		return false
	}
	if s.EndTime > 0 && index.UpdateStamp > s.EndTime {
		return false
	}
	return true
}

func matchesAdminSearchCategory(index model.FilmIndex, s model.SearchVo) bool {
	pid := support.ResolveCategoryID(s.Pid)
	cid := support.ResolveCategoryID(s.Cid)
	if cid > 0 {
		if support.IsRootCategory(cid) {
			return support.ResolveCategoryID(index.Pid) == cid
		}
		return support.ResolveCategoryID(index.Cid) == cid
	}
	if pid > 0 {
		return support.ResolveCategoryID(index.Pid) == pid
	}
	return true
}

func ClearAdminFilmSearchCache() {}
