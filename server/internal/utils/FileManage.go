package utils

import (
	"os"
	"path/filepath"
	"server/internal/config"
)

/*
文件读写
*/

func CreateBaseDir() error {
	if _, err := os.Stat(config.FilmPictureUploadDir); os.IsNotExist(err) {
		return os.MkdirAll(config.FilmPictureUploadDir, os.ModePerm)
	}
	return nil
}

func RemoveFile(path string) error {
	return os.Remove(path)
}

// ClearGalleryDir 清空图库目录下的文件（保留目录）
func ClearGalleryDir() error {
	dir := config.FilmPictureUploadDir
	if err := os.MkdirAll(dir, os.ModePerm); err != nil {
		return err
	}
	entries, err := os.ReadDir(dir)
	if err != nil {
		return err
	}
	for _, e := range entries {
		_ = os.RemoveAll(filepath.Join(dir, e.Name()))
	}
	return nil
}
