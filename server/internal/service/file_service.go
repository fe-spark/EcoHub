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

func (s *FileService) GetPhotoPage(name string, beginTime, endTime time.Time, page *dto.Page) ([]model.FileInfo, error) {
	var tl = []string{"jpeg", "jpg", "png", "webp", "ico"}
	return repository.GetFileInfoPage(tl, name, beginTime, endTime, page)
}

func (s *FileService) RenameFile(id uint, name string, operatorID uint, operatorIsAdmin bool) error {
	f := repository.GetFileInfoById(id)
	if f.ID == 0 {
		return fmt.Errorf("图片不存在")
	}
	if !operatorIsAdmin && f.Uid != int(operatorID) {
		return fmt.Errorf("权限不足，仅可操作本人上传的素材")
	}
	return repository.RenameFileInfo(id, name)
}
func (s *FileService) ValidateFilesRemovable(ids []uint, operatorID uint, operatorIsAdmin bool) error {
	for _, id := range ids {
		f := repository.GetFileInfoById(id)
		if f.ID == 0 {
			return fmt.Errorf("素材 ID %d 不存在", id)
		}
		if !operatorIsAdmin && f.Uid != int(operatorID) {
			return fmt.Errorf("权限不足，仅可操作本人上传的素材")
		}
	}
	return nil
}


func (s *FileService) RemoveFileById(id uint, operatorID uint, operatorIsAdmin bool) error {
	f := repository.GetFileInfoById(id)
	if f.ID == 0 {
		return fmt.Errorf("图片不存在")
	}
	if !operatorIsAdmin && f.Uid != int(operatorID) {
		return fmt.Errorf("权限不足，仅可操作本人上传的素材")
	}
	storagePath := repository.StoragePath(&f)
	err := os.Remove(storagePath)
	if err != nil && !os.IsNotExist(err) {
		return err
	}
	repository.DelFileInfo(id)
	return nil
}
