package film

import (
	"encoding/json"
	"errors"
	"log"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"

	"gorm.io/gorm"
)

func EnsurePage(page *dto.Page) *dto.Page {
	if page == nil {
		return &dto.Page{Current: 1, PageSize: 20}
	}
	if page.Current <= 0 {
		page.Current = 1
	}
	if page.PageSize <= 0 {
		page.PageSize = 20
	}
	return page
}

func ensurePage(page *dto.Page) *dto.Page {
	return EnsurePage(page)
}

func getPageOffset(page *dto.Page) int {
	page = ensurePage(page)
	if page.Current <= 1 {
		return 0
	}
	return (page.Current - 1) * page.PageSize
}

// GetBasicInfoByKey 获取影片的基本信息
func GetBasicInfoByKey(cid int64, mid int64) model.MovieBasicInfo {
	index := GetFilmIndexById(mid)
	if index != nil {
		return BuildMovieBasicInfos(*index)[0]
	}
	return model.MovieBasicInfo{}
}

// GetMovieDetail 获取影片详情信息
func GetMovieDetail(cid int64, mid int64) *model.MovieDetail {
	index := GetFilmIndexById(mid)
	if index == nil {
		return nil
	}

	var movieDetailInfo model.MovieDetailInfo
	if err := db.Mdb.Where("mid = ?", mid).First(&movieDetailInfo).Error; err != nil {
		if !errors.Is(err, gorm.ErrRecordNotFound) {
			log.Printf("GetMovieDetail Error: %v", err)
		}
		return nil
	}
	var detail model.MovieDetail
	if err := json.Unmarshal([]byte(movieDetailInfo.Content), &detail); err != nil {
		log.Printf("Unmarshal MovieDetail Error: %v", err)
		return nil
	}
	ApplyFilmIndex(&detail, *index)

	if detail.PlayFrom == nil {
		detail.PlayFrom = []string{}
	}
	if detail.PlayList == nil {
		detail.PlayList = [][]model.MovieUrlInfo{}
	} else {
		for i, inner := range detail.PlayList {
			if inner == nil {
				detail.PlayList[i] = []model.MovieUrlInfo{}
			}
		}
	}
	if detail.DownloadList == nil {
		detail.DownloadList = [][]model.MovieUrlInfo{}
	} else {
		for i, inner := range detail.DownloadList {
			if inner == nil {
				detail.DownloadList[i] = []model.MovieUrlInfo{}
			}
		}
	}
	return &detail
}

func GetFilmIndexById(id int64) *model.FilmIndex {
	s := model.FilmIndex{}
	if err := db.Mdb.Where("mid = ?", id).First(&s).Error; err != nil {
		return nil
	}
	return &s
}
