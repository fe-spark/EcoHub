package utils

import (
	"errors"
	"fmt"
	"net/http"
	"net/url"
	"os"
	"path"
	"path/filepath"
	"server/internal/config"
	"strings"
)

/*
数据请求保存,文件读写
*/

// SaveOnlineFile 保存网络文件, 提供下载url和保存路径, 返回保存后的文件名(非绝对路径)
func SaveOnlineFile(rawURL, dir string) (pathName string, err error) {
	r := &RequestInfo{Uri: rawURL}
	ApiGet(r)
	if len(r.Resp) <= 0 {
		msg := "SyncPicture Failed: response is empty"
		if r.Err != "" {
			msg = fmt.Sprintf("%s (%s)", msg, r.Err)
		}
		return "", errors.New(msg)
	}

	if err = os.MkdirAll(dir, os.ModePerm); err != nil {
		return "", err
	}

	ext := detectImageExt(rawURL, r.Resp)
	fileName := RandomString(16) + ext
	fullPath := filepath.Join(dir, fileName)

	if err = os.WriteFile(fullPath, r.Resp, 0o644); err != nil {
		return "", err
	}
	return fileName, nil
}

func detectImageExt(rawURL string, body []byte) string {
	// 1) URL path 扩展名
	if u, err := url.Parse(rawURL); err == nil {
		ext := strings.ToLower(path.Ext(u.Path))
		switch ext {
		case ".jpg", ".jpeg", ".png", ".webp", ".ico", ".gif", ".bmp":
			return ext
		}
	}
	// 2) 内容嗅探
	ct := http.DetectContentType(body)
	switch {
	case strings.Contains(ct, "jpeg"):
		return ".jpg"
	case strings.Contains(ct, "png"):
		return ".png"
	case strings.Contains(ct, "webp"):
		return ".webp"
	case strings.Contains(ct, "gif"):
		return ".gif"
	case strings.Contains(ct, "x-icon"), strings.Contains(ct, "vnd.microsoft.icon"):
		return ".ico"
	}
	// 3) 魔数兜底 webp (RIFF....WEBP)
	if len(body) >= 12 && string(body[0:4]) == "RIFF" && string(body[8:12]) == "WEBP" {
		return ".webp"
	}
	return ".jpg"
}

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
