package config

import (
	"fmt"
	"os"
	"path/filepath"
	"strings"
)

const (
	filmPictureUploadDirContainer = "/app/static/upload/gallery"
	filmPictureUploadDirLocal     = "./static/upload/gallery"
)

// resolveFilmPictureUploadDir 容器内写死发布卷路径；仅非容器（本地 go run）用项目根目录下的绝对路径。
func resolveFilmPictureUploadDir() string {
	if runningInContainer() {
		return filmPictureUploadDirContainer
	}
	cwd, err := os.Getwd()
	if err == nil {
		dir := cwd
		for {
			if _, err := os.Stat(filepath.Join(dir, "go.mod")); err == nil {
				return filepath.Join(dir, "static", "upload", "gallery")
			}
			parent := filepath.Dir(dir)
			if parent == dir {
				break
			}
			dir = parent
		}
	}
	return filmPictureUploadDirLocal
}

func runningInContainer() bool {
	if _, err := os.Stat("/.dockerenv"); err == nil {
		return true
	}
	exe, err := os.Executable()
	if err != nil {
		return false
	}
	switch filepath.Dir(exe) {
	case "/app", "/app/server":
		return true
	default:
		return false
	}
}

// ContainerUploadVolumeOK 非容器或已挂 /app/static/upload 为 true。
func ContainerUploadVolumeOK() bool {
	if !runningInContainer() {
		return true
	}
	return uploadPathIsMounted()
}

// EnsureContainerUploadVolume 未挂卷时返回错误供启动日志，不阻断启动。
func EnsureContainerUploadVolume() error {
	if ContainerUploadVolumeOK() {
		return nil
	}
	return fmt.Errorf("素材目录 %s 未挂载发布卷 /app/static/upload", FilmPictureUploadDir)
}

func uploadPathIsMounted() bool {
	data, err := os.ReadFile("/proc/self/mountinfo")
	if err != nil {
		return false
	}
	for _, line := range strings.Split(string(data), "\n") {
		fields := strings.Fields(line)
		if len(fields) < 5 {
			continue
		}
		switch fields[4] {
		case "/app/static/upload", "/app/static/upload/gallery":
			return true
		}
	}
	return false
}
