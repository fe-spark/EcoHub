package utils

import (
	"os"
	"path/filepath"
	"strings"

	"server/internal/config"
)

// IsGalleryImage 图库只接受这些图片后缀。
func IsGalleryImage(filename string) bool {
	switch strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), ".")) {
	case "jpg", "jpeg", "png", "webp", "ico":
		return true
	default:
		return false
	}
}

/*
文件读写
*/

func CreateBaseDir() error {
	return os.MkdirAll(config.FilmPictureUploadDir, os.ModePerm)
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
