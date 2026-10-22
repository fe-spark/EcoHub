package converter

import (
	"errors"
	"server/internal/model"
	"time"
)

/*
	系统内部对象想换转换
*/

// CovertFilmDetailVo 将 FilmDetailVo 转化为 MovieDetail
func CovertFilmDetailVo(fd model.FilmDetailVo) (model.MovieDetail, error) {
	if fd.Name == "" {
		return model.MovieDetail{}, errors.New("影片名称不能为空")
	}
	t, err := time.ParseInLocation(time.DateTime, fd.AddTime, time.Local)
	md := model.MovieDetail{
		Id:                 fd.Id,
		Cid:                fd.Cid,
		Pid:                fd.Pid,
		Name:               fd.Name,
		Picture:            fd.Picture,
		PictureSlide:       fd.PictureSlide,
		CustomPicture:      fd.CustomPicture,
		CustomPictureSlide: fd.CustomPictureSlide,
		IsCustomPicture:    fd.IsCustomPicture,
		DownFrom:           fd.DownFrom,
		MovieDescriptor: model.MovieDescriptor{
			SubTitle:    fd.SubTitle,
			CName:       fd.CName,
			EnName:      fd.EnName,
			Initial:     fd.Initial,
			ClassTag:    fd.ClassTag,
			Actor:       fd.Actor,
			Director:    fd.Director,
			Writer:      fd.Writer,
			Blurb:       fd.Content,
			Remarks:     fd.Remarks,
			ReleaseDate: fd.ReleaseDate,
			Area:        fd.Area,
			Language:    fd.Language,
			Year:        fd.Year,
			State:       fd.State,
			UpdateTime:  fd.UpdateTime,
			AddTime:     t.Unix(),
			DbId:        fd.DbId,
			DbScore:     fd.DbScore,
			Hits:        fd.Hits,
			Content:     fd.Content,
		},
	}
	md.ManualLinesUpdate = fd.ManualLinesUpdate
	if len(fd.PlayFrom) > 0 {
		md.PlayFrom = fd.PlayFrom
	}
	// v2 只保留m3u8播放源
	md.PlayList = GenFilmPlayList(fd.PlayLink, "$$$")
	return md, err
}
