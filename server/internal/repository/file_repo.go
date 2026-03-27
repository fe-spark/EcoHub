package repository

import (
	"fmt"
	"log"
	"path/filepath"
	"regexp"
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"
	"strings"

	"gorm.io/gorm/clause"
)

// StoragePath 获取文件的保存路径
func StoragePath(f *model.FileInfo) string {
	var storage string
	switch f.FileType {
	case "jpeg", "jpg", "png", "webp":
		storage = strings.Replace(f.Link, config.FilmPictureAccess, fmt.Sprint(config.FilmPictureUploadDir, "/"), 1)
	default:
	}
	return storage
}

// ExistFileTable 是否存在Picture表
func ExistFileTable() bool {
	return db.Mdb.Migrator().HasTable(&model.FileInfo{})
}

// SaveGallery 保存图片关联信息
func SaveGallery(f model.FileInfo) {
	db.Mdb.Create(&f)
}

// ExistFileInfoByRid 查找图片信息是否存在
func ExistFileInfoByRid(rid int64) bool {
	var count int64
	db.Mdb.Model(&model.FileInfo{}).Where("relevance_id = ?", rid).Count(&count)
	return count > 0
}

// GetFileInfoByRid 通过关联的资源id获取对应的图片信息
func GetFileInfoByRid(rid int64) model.FileInfo {
	var f model.FileInfo
	db.Mdb.Where("relevance_id = ?", rid).First(&f)
	return f
}

// GetFileInfoById 通过ID获取对应的图片信息
func GetFileInfoById(id uint) model.FileInfo {
	var f = model.FileInfo{}
	db.Mdb.First(&f, id)
	return f
}

// GetFileInfoPage 获取文件关联信息分页数据
func GetFileInfoPage(tl []string, page *dto.Page) []model.FileInfo {
	var fl []model.FileInfo
	query := db.Mdb.Model(&model.FileInfo{}).Where("file_type IN ?", tl).Order("id DESC")
	dto.GetPage(query, page)
	if err := query.Limit(page.PageSize).Offset((page.Current - 1) * page.PageSize).Find(&fl).Error; err != nil {
		log.Println(err)
		return nil
	}
	return fl
}

func DelFileInfo(id uint) {
	db.Mdb.Unscoped().Delete(&model.FileInfo{}, id)
}

// SaveVirtualPic 保存待同步的图片信息 (MySQL 持久化)
func SaveVirtualPic(pl []model.VirtualPicture) error {
	var queue []model.VirtualPictureQueue
	for _, p := range pl {
		queue = append(queue, model.VirtualPictureQueue{
			Mid:  p.Id,
			Link: p.Link,
		})
	}
	if len(queue) > 0 {
		return db.Mdb.Clauses(clause.OnConflict{
			Columns:   []clause.Column{{Name: "mid"}},
			DoUpdates: clause.AssignmentColumns([]string{"link", "updated_at"}),
		}).Create(&queue).Error
	}
	return nil
}

// SyncFilmPicture 同步新采集入栈还未同步的图片 (从 MySQL 获取)
func SyncFilmPicture() {
	var queue []model.VirtualPictureQueue
	// 每次扫描 MaxScanCount 条
	if err := db.Mdb.Limit(config.MaxScanCount).Find(&queue).Error; err != nil || len(queue) == 0 {
		return
	}

	for _, item := range queue {
		// 判断当前影片是否已经同步过图片
		if ExistFileInfoByRid(item.Mid) {
			db.Mdb.Unscoped().Delete(&item)
			continue
		}
		// 将图片同步到服务器中
		fileName, err := utils.SaveOnlineFile(item.Link, config.FilmPictureUploadDir)
		if err != nil {
			// 如果下载失败，逻辑上可以保留重试或者删除看业务需求，这里先删除防止死循环
			db.Mdb.Unscoped().Delete(&item)
			continue
		}
		// 完成同步后将图片信息保存到 Gallery 中
		SaveGallery(model.FileInfo{
			Link:        fmt.Sprint(config.FilmPictureAccess, fileName),
			Uid:         config.UserIdInitialVal,
			RelevanceId: item.Mid,
			Type:        0,
			Fid:         regexp.MustCompile(`\.[^.]+$`).ReplaceAllString(fileName, ""),
			FileType:    strings.TrimPrefix(filepath.Ext(fileName), "."),
		})
		// 同步成功后从队列删除
		db.Mdb.Unscoped().Delete(&item)
	}
	// 递归执行直到图片暂存信息为空
	SyncFilmPicture()
}
