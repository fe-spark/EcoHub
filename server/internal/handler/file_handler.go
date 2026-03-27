package handler

import (
	"fmt"
	"path/filepath"
	"strconv"
	"strings"

	"server/internal/config"
	"server/internal/model/dto"
	"server/internal/service"
	"server/internal/utils"

	"github.com/gin-gonic/gin"
)

type FileHandler struct{}

var FileHd = new(FileHandler)

// SingleUpload 单文件上传, 暂定为图片上传
func (h *FileHandler) SingleUpload(c *gin.Context) {
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

	fileName := fmt.Sprintf("%s/%s%s", config.FilmPictureUploadDir, utils.RandomString(8), filepath.Ext(file.Filename))
	err = c.SaveUploadedFile(file, fileName)
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}

	uc := v.(*utils.UserClaims)
	link := service.FileSvc.SingleFileUpload(fileName, int(uc.UserID))
	dto.Success(link, "上传成功", c)
}

// MultipleUpload 批量文件上传
func (h *FileHandler) MultipleUpload(c *gin.Context) {
	v, ok := c.Get(config.AuthUserClaims)
	if !ok {
		dto.Failed("上传失败, 当前用户信息异常", c)
		return
	}
	form, err := c.MultipartForm()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	files := form.File["files"]
	uc := v.(*utils.UserClaims)

	var fileNames []string
	for _, file := range files {
		fileName := fmt.Sprintf("%s/%s%s", config.FilmPictureUploadDir, utils.RandomString(8), filepath.Ext(file.Filename))
		err = c.SaveUploadedFile(file, fileName)
		if err != nil {
			dto.Failed(err.Error(), c)
			return
		}
		fileNames = append(fileNames, service.FileSvc.SingleFileUpload(fileName, int(uc.UserID)))
	}

	dto.Success(fileNames, "上传成功", c)
}

// DelFile 删除文件
func (h *FileHandler) DelFile(c *gin.Context) {
	var req struct {
		Id string `json:"id"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	id, err := strconv.ParseUint(strings.TrimSpace(req.Id), 10, 64)
	if err != nil {
		dto.Failed("操作失败, 未获取到需删除的文件标识信息", c)
		return
	}
	if e := service.FileSvc.RemoveFileById(uint(id)); e != nil {
		dto.Failed(fmt.Sprint("删除失败", e.Error()), c)
		return
	}
	dto.SuccessOnlyMsg("文件已删除", c)
}

// PhotoWall 照片墙数据
func (h *FileHandler) PhotoWall(c *gin.Context) {
	page := dto.GetPageParams(c)
	page.PageSize = 39
	pl := service.FileSvc.GetPhotoPage(page)
	dto.Success(gin.H{"list": pl, "page": page}, "图片分页数据获取成功", c)
}
