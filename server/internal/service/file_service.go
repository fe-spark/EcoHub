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

func (s *FileService) SingleFileUpload(fileName string, name string, uid int) (string, error) {
	var f = model.FileInfo{
		Link: fmt.Sprint(config.FilmPictureAccess, filepath.Base(fileName)),
		Uid:  uid,
		Type: 0,
		Name: name,
	}
	f.Fid = strings.TrimSuffix(filepath.Base(fileName), filepath.Ext(fileName))
	f.FileType = strings.ToLower(strings.TrimPrefix(filepath.Ext(fileName), "."))
	if err := repository.SaveGallery(f); err != nil {
		// DB 落库失败时清理磁盘文件，避免产生孤儿文件
		_ = os.Remove(fileName)
		return "", err
	}
	return f.Link, nil
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
// RemoveFiles 校验权限后先删磁盘文件，再删数据库记录。磁盘删除失败时保留记录以便重试。
func (s *FileService) RemoveFiles(ids []uint, operatorID uint, operatorIsAdmin bool) error {
	seen := make(map[uint]struct{}, len(ids))
	uniq := make([]uint, 0, len(ids))
	for _, id := range ids {
		if id == 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		uniq = append(uniq, id)
	}
	if len(uniq) == 0 {
		return nil
	}

	files, err := repository.GetFileInfosByIds(uniq)
	if err != nil {
		return err
	}
	byID := make(map[uint]model.FileInfo, len(files))
	for _, f := range files {
		byID[f.ID] = f
	}
	for _, id := range uniq {
		f, ok := byID[id]
		if !ok {
			return fmt.Errorf("素材 ID %d 不存在", id)
		}
		if !operatorIsAdmin && f.Uid != int(operatorID) {
			return fmt.Errorf("权限不足，仅可操作本人上传的素材")
		}
		if repository.StoragePath(&f) == "" {
			return fmt.Errorf("素材 ID %d 没有可删除的磁盘路径", id)
		}
	}

	// 先删磁盘再删记录。中途失败时记录还在，管理员可以重试。
	for _, id := range uniq {
		f := byID[id]
		storagePath := repository.StoragePath(&f)
		if err := os.Remove(storagePath); err != nil && !os.IsNotExist(err) {
			return fmt.Errorf("删除磁盘文件失败: %w", err)
		}
	}
	return repository.DelFileInfos(uniq)
}
