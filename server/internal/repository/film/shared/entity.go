package shared

import (
	"fmt"

	"server/internal/model"
)

func ApplyFilmListSnapshot(detail *model.MovieDetail, info model.FilmListSnapshot) {
	if detail == nil {
		return
	}
	detail.Id = info.Mid
	detail.Pid = info.Pid
	detail.Cid = info.Cid
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
	if info.Year > 0 {
		detail.Year = fmt.Sprint(info.Year)
	}
}

func BuildMovieBasicInfosFromSnapshots(infos ...model.FilmListSnapshot) []model.MovieBasicInfo {
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
			SourceId:     s.SourceId,
		})
	}
	return list
}
