package repository

import (
	"fmt"
	"log"
	"os"
	"sync"
	"time"

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
	invalidateMissingGalleryCache()
}

// GetFileInfoById 通过ID获取对应的图片信息
func GetFileInfoById(id uint) model.FileInfo {
	var f = model.FileInfo{}
	db.Mdb.First(&f, id)
	return f
}

// GetFileInfoPage 获取素材分页数据（仅用户手动上传，relevance_id = 0）
// name 支持素材名称模糊搜索；beginTime/endTime 为零值时不按时间过滤
func GetFileInfoPage(tl []string, name string, beginTime, endTime time.Time, page *dto.Page) []model.FileInfo {
	var fl []model.FileInfo
	query := db.Mdb.Model(&model.FileInfo{}).
		Where("file_type IN ?", tl).
		Where("relevance_id = 0")
	if name != "" {
		query = query.Where("(name LIKE ? OR fid LIKE ?)", "%"+name+"%", "%"+name+"%")
	}
	if !beginTime.IsZero() {
		query = query.Where("created_at >= ?", beginTime)
	}
	if !endTime.IsZero() {
		query = query.Where("created_at <= ?", endTime)
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
	invalidateMissingGalleryCache()
}

var (
	missingGalleryMu    sync.Mutex
	missingGalleryCount int
	missingGalleryAt    time.Time
)

const missingGalleryTTL = 30 * time.Second

func invalidateMissingGalleryCache() {
	missingGalleryMu.Lock()
	missingGalleryAt = time.Time{}
	missingGalleryMu.Unlock()
}

// CountMissingUserGallery 用户上传素材中磁盘文件已不存在的条数（短 TTL，避免列表热路径每次全量 Stat）。
func CountMissingUserGallery() int {
	missingGalleryMu.Lock()
	defer missingGalleryMu.Unlock()
	if !missingGalleryAt.IsZero() && time.Since(missingGalleryAt) < missingGalleryTTL {
		return missingGalleryCount
	}
	var list []model.FileInfo
	if err := db.Mdb.Where("relevance_id = 0").Find(&list).Error; err != nil {
		return missingGalleryCount
	}
	n := 0
	for i := range list {
		path := StoragePath(&list[i])
		if path == "" {
			continue
		}
		if _, err := os.Stat(path); os.IsNotExist(err) {
			n++
		}
	}
	missingGalleryCount = n
	missingGalleryAt = time.Now()
	return n
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
