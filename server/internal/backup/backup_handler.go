package backup

import (
	"net/http"
	"strings"

	"server/internal/httpx"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/user"

	"github.com/gin-gonic/gin"
)

type BackupHandler struct{}

var BackupHd = new(BackupHandler)

// ExportConfigBackup 导出站点配置备份（需管理密码，不含影视库存与账号，仅超级管理员）
func (h *BackupHandler) ExportConfigBackup(c *gin.Context) {
	if _, ok := httpx.RequireAdminClaims(c, "权限不足，仅超级管理员可导出配置备份"); !ok {
		return
	}
	var req model.ConfigBackupExportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if strings.TrimSpace(req.Password) == "" || !httpx.VerifyCurrentPassword(c, req.Password, user.UserSvc.VerifyUserPassword) {
		dto.Failed("导出失败, 密钥校验失败!!!", c)
		return
	}
	bk, err := BackupSvc.ExportConfig()
	if err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.Success(bk, "配置备份导出成功", c)
}

// maxBackupImportBodyBytes 限制配置备份导入请求体上限（16MB），避免密码校验前超大 JSON 触发 OOM。
const maxBackupImportBodyBytes = 16 << 20

// ImportConfigBackup 导入站点配置备份（需管理密码，仅超级管理员）
func (h *BackupHandler) ImportConfigBackup(c *gin.Context) {
	if _, ok := httpx.RequireAdminClaims(c, "权限不足，仅超级管理员可导入配置备份"); !ok {
		return
	}
	c.Request.Body = http.MaxBytesReader(c.Writer, c.Request.Body, maxBackupImportBodyBytes)
	var req model.ConfigBackupImportRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数异常", c)
		return
	}
	if !httpx.VerifyCurrentPassword(c, req.Password, user.UserSvc.VerifyUserPassword) {
		dto.Failed("导入失败, 密钥校验失败!!!", c)
		return
	}
	if err := BackupSvc.ImportConfig(req); err != nil {
		dto.Failed(err.Error(), c)
		return
	}
	dto.SuccessOnlyMsg("配置备份导入成功", c)
}
