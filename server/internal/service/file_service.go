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
)

type FileService struct{}

func NewFileService() *FileService {
	return &FileService{}
}

var FileSvc = new(FileService)

func (s *FileService) SingleFileUpload(fileName string, uid int) string {
	var f = model.FileInfo{
		Link: fmt.Sprint(config.FilmPictureAccess, filepath.Base(fileName)),
		Uid:  uid,
		Type: 0,
	}
	f.Fid = strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	f.FileType = strings.TrimPrefix(filepath.Ext(fileName), ".")
	repository.SaveGallery(f)
	return f.Link
}

func (s *FileService) GetPhotoPage(page *dto.Page) []model.FileInfo {
	var tl = []string{"jpeg", "jpg", "png", "webp"}
	return repository.GetFileInfoPage(tl, page)
}

func (s *FileService) RemoveFileById(id uint) error {
	f := repository.GetFileInfoById(id)
	storagePath := repository.StoragePath(&f)
	err := os.Remove(storagePath)
	if err != nil {
		return err
	}
	repository.DelFileInfo(id)
	return nil
}
