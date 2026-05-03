package film

import (
	"fmt"

	"server/internal/model"
)

func BuildContentKey(detail model.MovieDetail) string {
	keys := BuildMovieMatchKeys(detail.DbId, detail.Name)
	if len(keys) == 0 {
		return ""
	}
	return fmt.Sprintf("name_%s", keys[0])
}

func ApplyFilmIndex(detail *model.MovieDetail, info model.FilmIndex) {
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
	detail.Picture = info.Picture
	detail.PictureSlide = info.PictureSlide
	detail.Actor = info.Actor
	detail.Director = info.Director
	detail.Blurb = info.Blurb
	if info.Year > 0 {
		detail.Year = fmt.Sprint(info.Year)
	}
}

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
	detail.Picture = info.Picture
	detail.PictureSlide = info.PictureSlide
	detail.Actor = info.Actor
	detail.Director = info.Director
	detail.Blurb = info.Blurb
	if info.Year > 0 {
		detail.Year = fmt.Sprint(info.Year)
	}
}

func BuildMovieBasicInfos(infos ...model.FilmIndex) []model.MovieBasicInfo {
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
			Picture:      s.Picture,
			PictureSlide: s.PictureSlide,
			Actor:        s.Actor,
			Director:     s.Director,
			Blurb:        s.Blurb,
			Remarks:      s.Remarks,
			Area:         s.Area,
			Year:         fmt.Sprint(s.Year),
		})
	}
	return list
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
			Picture:      s.Picture,
			PictureSlide: s.PictureSlide,
			Actor:        s.Actor,
			Director:     s.Director,
			Blurb:        s.Blurb,
			Remarks:      s.Remarks,
			Area:         s.Area,
			Year:         fmt.Sprint(s.Year),
		})
	}
	return list
}
