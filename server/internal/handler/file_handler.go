package handler

import (
	"fmt"
	"path/filepath"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

type FileHandler struct{}

var FileHd = new(FileHandler)

var allowedImageExt = map[string]bool{
	"jpg":  true,
	"jpeg": true,
	"png":  true,
	"webp": true,
	"ico":  true,
}

func isAllowedImage(filename string) bool {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(filename), "."))
	return allowedImageExt[ext]
}

// Upload 统一文件上传接口（支持图片上传）
func (h *FileHandler) Upload(c *gin.Context) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.Failed("上传失败, 当前用户信息异常", c)
		return
	}
	file, err := c.FormFile("file")
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	if !isAllowedImage(file.Filename) {
		dto.Failed("仅支持上传 JPG/JPEG/PNG/WebP/ICO 格式的图片", c)
		return
	}

	fileName := fmt.Sprintf("%s/%s%s", config.FilmPictureUploadDir, utils.RandomString(8), filepath.Ext(file.Filename))
	err = c.SaveUploadedFile(file, fileName)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}

	uc := v.(*utils.UserClaims)
	name := strings.TrimSpace(c.PostForm("name"))
	if name == "" {
		name = strings.TrimSuffix(file.Filename, filepath.Ext(file.Filename))
	}
	link := service.FileSvc.SingleFileUpload(fileName, name, int(uc.UserID))
	dto.Success(link, "上传成功", c)
}

// RenameFile 重命名素材
func (h *FileHandler) RenameFile(c *gin.Context) {
	var req struct {
		Id   uint   `json:"id"`
		Name string `json:"name"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if req.Id == 0 {
		dto.Failed("操作失败, 未获取到需重命名的文件标识信息", c)
		return
	}
	name := strings.TrimSpace(req.Name)
	if name == "" {
		dto.Failed("素材名称不能为空", c)
		return
	}
	if e := service.FileSvc.RenameFile(req.Id, name); e != nil {
		dto.Failed(fmt.Sprint("重命名失败", e.Error()), c)
		return
	}
	dto.SuccessOnlyMsg("素材名称已更新", c)
}

// DelFile 删除文件（统一批量/单文件删除）
func (h *FileHandler) DelFile(c *gin.Context) {
	var req struct {
		Ids []uint `json:"ids"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	cleanIds := make([]uint, 0, len(req.Ids))
	for _, id := range req.Ids {
		if id > 0 {
			cleanIds = append(cleanIds, id)
		}
	}
	if len(cleanIds) == 0 {
		dto.Failed("操作失败, 未获取到需删除的文件标识信息", c)
		return
	}
	for _, id := range cleanIds {
		if e := service.FileSvc.RemoveFileById(id); e != nil {
			dto.Failed(fmt.Sprint("删除失败: ", e.Error()), c)
			return
		}
	}
	dto.SuccessOnlyMsg("文件已删除", c)
}

// PhotoWall 照片墙数据（仅用户上传素材）
func (h *FileHandler) PhotoWall(c *gin.Context) {
	page := dto.GetPageParams(c)
	name := strings.TrimSpace(c.DefaultQuery("name", ""))
	beginTime, ok := parsePhotoTimeQuery(c.DefaultQuery("beginTime", ""), c)
	if !ok {
		return
	}
	endTime, ok := parsePhotoTimeQuery(c.DefaultQuery("endTime", ""), c)
	if !ok {
		return
	}
	pl := service.FileSvc.GetPhotoPage(name, beginTime, endTime, page)
	dto.Success(gin.H{"list": pl, "page": page, "storage": service.FileSvc.StorageStatus()}, "图片分页数据获取成功", c)
}

func parsePhotoTimeQuery(raw string, c *gin.Context) (time.Time, bool) {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return time.Time{}, true
	}
	t, err := time.ParseInLocation(time.DateTime, raw, time.Local)
	if err != nil {
		dto.Failed("图片分页数据获取失败, 请求参数异常", c)
		return time.Time{}, false
	}
	return t, true
}
