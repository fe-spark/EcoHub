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

func ApplyResolvedCategory(detail *model.MovieDetail, info model.SearchInfo) {
	if detail == nil {
		return
	}
	detail.Pid = info.Pid
	detail.Cid = info.Cid
}

func GetBasicInfoBySearchInfos(infos ...model.SearchInfo) []model.MovieBasicInfo {
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
