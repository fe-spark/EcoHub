package service

import (
	"strconv"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
)

// UseStoredFilm 编辑页使用影片自己保存的正文和封面，不用前台海报源替换结果。
func UseStoredFilm(detail *model.MovieDetailVo) {
	if detail == nil || detail.Id <= 0 || db.Mdb == nil {
		return
	}
	var row model.FilmIndex
	if err := db.Mdb.Where("mid = ?", detail.Id).First(&row).Error; err != nil {
		return
	}
	detail.Picture = row.Picture
	detail.PictureSlide = row.PictureSlide
	detail.CustomPicture = row.CustomPicture
	detail.CustomPictureSlide = row.CustomPictureSlide
	detail.IsCustomPicture = row.IsCustomPicture
	detail.SubTitle = row.SubTitle
	detail.ClassTag = row.ClassTag
	detail.Actor = row.Actor
	detail.Director = row.Director
	detail.Writer = row.Writer
	detail.Blurb = row.Blurb
	detail.Content = row.Content
	detail.ReleaseDate = row.ReleaseDate
	detail.Area = row.Area
	detail.Language = row.Language
	if row.Year > 0 {
		detail.Year = strconv.FormatInt(row.Year, 10)
	}
	if row.Score > 0 {
		detail.DbScore = strconv.FormatFloat(row.Score, 'f', -1, 64)
	}
	if strings.TrimSpace(row.Remarks) != "" {
		detail.Remarks = row.Remarks
	}
}
