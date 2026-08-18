package service

import (
	"fmt"
	"os"
	"path/filepath"
	"server/internal/config"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"strings"
	"time"
)

type FileService struct{}

func NewFileService() *FileService {
	return &FileService{}
}

var FileSvc = new(FileService)

func (s *FileService) SingleFileUpload(fileName string, name string, uid int) string {
	var f = model.FileInfo{
		Link: fmt.Sprint(config.FilmPictureAccess, filepath.Base(fileName)),
		Uid:  uid,
		Type: 0,
		Name: name,
	}
	f.Fid = strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	f.FileType = strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), "."))
	repository.SaveGallery(f)
	return f.Link
}

func (s *FileService) GetPhotoPage(name string, beginTime, endTime time.Time, page *dto.Page) []model.FileInfo {
	var tl = []string{"jpeg", "jpg", "png", "webp", "ico"}
	return repository.GetFileInfoPage(tl, name, beginTime, endTime, page)
}

// StorageStatus 素材存储探测结果；文案由前端组装。
type StorageStatus struct {
	VolumeMounted bool `json:"volumeMounted"`
	MissingCount  int  `json:"missingCount"`
}

func (s *FileService) StorageStatus() StorageStatus {
	return StorageStatus{
		VolumeMounted: config.ContainerUploadVolumeOK(),
		MissingCount:  repository.CountMissingUserGallery(),
	}
}

func (s *FileService) RenameFile(id uint, name string) error {
	f := repository.GetFileInfoById(id)
	if f.ID == 0 {
		return fmt.Errorf("图片不存在")
	}
	return repository.RenameFileInfo(id, name)
}

func (s *FileService) RemoveFileById(id uint) error {
	f := repository.GetFileInfoById(id)
	if f.ID == 0 {
		return fmt.Errorf("图片不存在")
	}
	storagePath := repository.StoragePath(&f)
	err := os.Remove(storagePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	repository.DelFileInfo(id)
	return nil
}
