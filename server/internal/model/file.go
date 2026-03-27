package model

import "gorm.io/gorm"

// FileInfo 图片信息对象
type FileInfo struct {
	gorm.Model
	Link        string `json:"link"`        // 图片链接
	Uid         int    `json:"uid"`         // 上传人ID
	RelevanceId int64  `json:"relevanceId"` // 关联资源ID
	Type        int    `json:"type"`        // 文件类型 (0 影片封面, 1 用户头像)
	Fid         string `json:"fid"`         // 图片唯一标识, 通常为文件名
	FileType    string `json:"fileType"`    // 文件类型, txt, png, jpg
}

// TableName 设置图片存储表的表名
func (f *FileInfo) TableName() string {
	return "files"
}

// VirtualPicture 采集入站,待同步的图片信息
type VirtualPicture struct {
	Id   int64  `json:"id"`
	Link string `json:"link"`
}

// VirtualPictureQueue 待同步图片队列 (MySQL)
type VirtualPictureQueue struct {
	gorm.Model
	Mid  int64  `gorm:"uniqueIndex"`
	Link string `gorm:"type:text"`
}
