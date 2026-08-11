package repository

import (
	"fmt"
	"os"
	"log"
	"path/filepath"
	"regexp"
	"sync/atomic"
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"
	"strings"

	"gorm.io/gorm/clause"
)

var pictureSyncRunning atomic.Bool

// AcquirePictureSync 获取图片同步执行权, 已在同步中时返回 false
func AcquirePictureSync() bool {
	return pictureSyncRunning.CompareAndSwap(false, true)
}

// ReleasePictureSync 释放图片同步执行权
func ReleasePictureSync() {
	pictureSyncRunning.Store(false)
}

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
// scope 支持 all(全部)/manual(手动上传)/related(关联影片); name 支持素材名称模糊搜索
func GetFileInfoPage(tl []string, scope string, name string, page *dto.Page) []model.FileInfo {
	var fl []model.FileInfo
	query := db.Mdb.Model(&model.FileInfo{}).Where("file_type IN ?", tl)
	switch scope {
	case "manual":
		query = query.Where("relevance_id = 0")
	case "related":
		query = query.Where("relevance_id > 0")
	}
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
			// 下载失败先出队，避免死循环；日志便于排查源站不可达等问题
			log.Printf("[PictureSync] download failed mid=%d link=%s err=%v", item.Mid, item.Link, err)
			db.Mdb.Unscoped().Delete(&item)
			continue
		}
		// 完成同步后将图片信息保存到 Gallery 中
		fid := regexp.MustCompile(`\.[^.]+$`).ReplaceAllString(fileName, "")
		name := fid
		var filmName string
		if err := db.Mdb.Model(&model.FilmIndex{}).Where("mid = ?", item.Mid).Pluck("name", &filmName).Error; err == nil && strings.TrimSpace(filmName) != "" {
			name = strings.TrimSpace(filmName)
		}
		SaveGallery(model.FileInfo{
			Link:        fmt.Sprint(config.FilmPictureAccess, fileName),
			Uid:         config.UserIdInitialVal,
			RelevanceId: item.Mid,
			Type:        0,
			Fid:         fid,
			FileType:    strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), ".")),
			Name:        name,
		})
		// 同步成功后从队列删除
		db.Mdb.Unscoped().Delete(&item)
	}
	// 递归执行直到图片暂存信息为空
	SyncFilmPicture()
}

// RepairMissingPictures 扫描图库表中本地文件缺失的记录，按 film_index.picture 重新下载。
// 用于数据重置后 files 记录残留、或磁盘文件被清理后的修复。
func RepairMissingPictures() (fixed int, skipped int, failed int) {
	var list []model.FileInfo
	if err := db.Mdb.Model(&model.FileInfo{}).Order("id DESC").Find(&list).Error; err != nil {
		log.Printf("[PictureRepair] list files failed: %v", err)
		return
	}
	for _, f := range list {
		storage := StoragePath(&f)
		if storage != "" {
			if _, err := os.Stat(storage); err == nil {
				skipped++
				continue
			}
		}
		// 优先用影片原图 URL 重新下载
		var remote string
		if f.RelevanceId > 0 {
			_ = db.Mdb.Model(&model.FilmIndex{}).Where("mid = ?", f.RelevanceId).Pluck("picture", &remote).Error
		}
		remote = strings.TrimSpace(remote)
		if remote == "" || strings.HasPrefix(remote, config.FilmPictureAccess) || strings.HasPrefix(remote, "/api/upload/") {
			// 无可用远程源：删除无效图库记录，避免前端持续 404 黑块
			db.Mdb.Unscoped().Delete(&model.FileInfo{}, f.ID)
			failed++
			continue
		}
		fileName, err := utils.SaveOnlineFile(remote, config.FilmPictureUploadDir)
		if err != nil {
			log.Printf("[PictureRepair] redownload failed id=%d mid=%d err=%v", f.ID, f.RelevanceId, err)
			failed++
			continue
		}
		newLink := fmt.Sprint(config.FilmPictureAccess, fileName)
		fid := regexp.MustCompile(`\.[^.]+$`).ReplaceAllString(fileName, "")
		fileType := strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), "."))
		if err := db.Mdb.Model(&model.FileInfo{}).Where("id = ?", f.ID).Updates(map[string]any{
			"link":      newLink,
			"fid":       fid,
			"file_type": fileType,
		}).Error; err != nil {
			log.Printf("[PictureRepair] update meta failed id=%d err=%v", f.ID, err)
			failed++
			continue
		}
		fixed++
	}
	log.Printf("[PictureRepair] done fixed=%d skipped=%d failed=%d", fixed, skipped, failed)
	return
}
