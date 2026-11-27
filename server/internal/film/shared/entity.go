package shared

import (
	"fmt"
	"strconv"
	"strings"

	"server/internal/model"
)

func ApplyFilmListSnapshot(detail *model.MovieDetail, info model.FilmListSnapshot) {
	if detail == nil {
		return
	}
	if replaced := ApplyPosterSourcePictures([]model.FilmListSnapshot{info}); len(replaced) == 1 {
		info = replaced[0]
	}
	detail.Id = info.Mid
	detail.SourceId = info.SourceId
	detail.Pid = info.Pid
	detail.Cid = info.Cid
	if detail.RawCid == 0 && info.CategoryKey != "" {
		parts := strings.Split(info.CategoryKey, ":")
		if len(parts) >= 3 {
			if id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64); err == nil && id > 0 {
				detail.RawCid = id
			}
		}
	}
	if detail.RawPid == 0 && info.RootCategoryKey != "" {
		parts := strings.Split(info.RootCategoryKey, ":")
		if len(parts) >= 3 {
			if id, err := strconv.ParseInt(parts[len(parts)-1], 10, 64); err == nil && id > 0 {
				detail.RawPid = id
			}
		}
	}
	detail.Name = info.Name
	detail.SubTitle = info.SubTitle
	detail.CName = info.CName
	detail.ClassTag = info.ClassTag
	detail.Area = info.Area
	detail.Language = info.Language
	detail.State = info.State
	detail.Remarks = info.Remarks
	detail.Picture = info.DisplayPicture()
	detail.PictureSlide = info.DisplayPictureSlide()
	detail.CustomPicture = info.CustomPicture
	detail.CustomPictureSlide = info.CustomPictureSlide
	detail.IsCustomPicture = info.IsCustomPicture
	detail.Actor = info.Actor
	detail.Director = info.Director
	detail.Writer = info.Writer
	detail.Blurb = info.Blurb
	detail.Content = info.Content
	detail.ReleaseDate = info.ReleaseDate
	detail.IsScraped = info.IsScraped
	detail.TmdbID = info.TmdbID
	if info.Year > 0 {
		detail.Year = fmt.Sprint(info.Year)
	}
}

func BuildMovieBasicInfosFromSnapshots(infos ...model.FilmListSnapshot) []model.MovieBasicInfo {
	infos = ApplyPosterSourcePictures(infos)
	list := make([]model.MovieBasicInfo, 0, len(infos))
	for _, s := range infos {
		list = append(list, model.MovieBasicInfo{
			Id:           s.Mid,
			Cid:          s.Cid,
			Pid:          s.Pid,
			Name:         s.Name,
			SubTitle:     s.SubTitle,
			CName:        s.CName,
			State:        s.State,
			Picture:      s.DisplayPicture(),
			PictureSlide: s.DisplayPictureSlide(),
			Actor:        s.Actor,
			Director:     s.Director,
			Blurb:        s.Blurb,
			Remarks:      s.Remarks,
			Area:         s.Area,
			Year:         fmt.Sprint(s.Year),
			ClassTag:     s.ClassTag,
			Score:        s.Score,
			Hits:         s.Hits,
			SourceId:     s.SourceId,
			IsScraped:    s.IsScraped,
			TmdbID:       s.TmdbID,
		})
	}
	return list
}
