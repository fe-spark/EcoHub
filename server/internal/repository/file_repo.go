package repository

import (
	"fmt"
	"log"
	"os"
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"strings"
)

// StoragePath 获取文件的保存路径
func StoragePath(f *model.FileInfo) string {
	var storage string
	switch f.FileType {
	case "jpeg", "jpg", "png", "webp", "ico":
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

// GetFileInfoById 通过ID获取对应的图片信息
func GetFileInfoById(id uint) model.FileInfo {
	var f = model.FileInfo{}
	db.Mdb.First(&f, id)
	return f
}

// GetFileInfoPage 获取素材分页数据（仅用户手动上传，relevance_id = 0）
// name 支持素材名称模糊搜索
func GetFileInfoPage(tl []string, name string, page *dto.Page) []model.FileInfo {
	var fl []model.FileInfo
	query := db.Mdb.Model(&model.FileInfo{}).
		Where("file_type IN ?", tl).
		Where("relevance_id = 0")
	if name != "" {
		query = query.Where("(name LIKE ? OR fid LIKE ?)", "%"+name+"%", "%"+name+"%")
	}
	query = query.Order("id DESC")
	dto.GetPage(query, page)
	if err := query.Limit(page.PageSize).Offset((page.Current - 1) * page.PageSize).Find(&fl).Error; err != nil {
		log.Println(err)
		return nil
	}
	return fl
}

// RenameFileInfo 更新素材名称
func RenameFileInfo(id uint, name string) error {
	return db.Mdb.Model(&model.FileInfo{}).Where("id = ?", id).Update("name", name).Error
}

func DelFileInfo(id uint) {
	db.Mdb.Unscoped().Delete(&model.FileInfo{}, id)
}

// PurgeSyncedGallery 清理历史采集同步产生的图库记录及本地文件（素材中心仅保留用户上传）
func PurgeSyncedGallery() {
	var list []model.FileInfo
	if err := db.Mdb.Where("relevance_id > 0").Find(&list).Error; err != nil {
		log.Printf("[Gallery] list synced files failed: %v", err)
		return
	}
	if len(list) == 0 {
		return
	}
	for _, f := range list {
		if path := StoragePath(&f); path != "" {
			_ = os.Remove(path)
		}
	}
	if err := db.Mdb.Unscoped().Where("relevance_id > 0").Delete(&model.FileInfo{}).Error; err != nil {
		log.Printf("[Gallery] purge synced files failed: %v", err)
		return
	}
	log.Printf("[Gallery] purged %d synced picture records", len(list))
}
