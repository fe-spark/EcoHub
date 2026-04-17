package model

import "time"

// Category 分类信息 (统一层级模型)
type Category struct {
	Id        int64     `gorm:"primaryKey;autoIncrement:true" json:"id"`                                // 分类ID
	Pid       int64     `gorm:"uniqueIndex:uidx_pid_name;index;constraint:OnDelete:CASCADE" json:"pid"` // 父级分类ID (Pid=0 表示顶级大类)
	Name      string    `gorm:"size:64;uniqueIndex:uidx_pid_name" json:"name"`                          // 分类名称
	StableKey string    `gorm:"size:128;uniqueIndex" json:"stable_key"`                                 // 稳定分类标识
	Alias     string    `gorm:"size:128" json:"alias"`                                                  // 别名/匹配规则 (仅大类有用)
	Show      bool      `gorm:"default:true" json:"show"`                                               // 是否展示
	Sort      int       `gorm:"default:0" json:"sort"`                                                  // 排序权重
	CreatedAt time.Time `json:"created_at"`
	UpdatedAt time.Time `json:"updated_at"`
}

func (Category) TableName() string {
	return TableCategory
}

// CategoryMapping 采集源分类与本地分类的强绑定映射 (方案B: 100% 识别)
type CategoryMapping struct {
	Id             int64  `gorm:"primaryKey;autoIncrement:true" json:"id"`
	SourceId       string `gorm:"uniqueIndex:idx_source_type;index:idx_source_version;size:32" json:"source_id"` // 资源站ID
	SourceTypeId   int64  `gorm:"uniqueIndex:idx_source_type" json:"source_type_id"`                             // 采集站分类ID (type_id)
	CategoryId     int64  `gorm:"index" json:"category_id"`                                                      // 本地分类ID (对应 Category.Id)
	MappingVersion int64  `gorm:"index:idx_source_version" json:"mapping_version"`
}

func (CategoryMapping) TableName() string {
	return "category_mappings"
}

// CategoryTree 分類信息樹形結構 (扁平化 JSON)
type CategoryTree struct {
	Id        int64           `json:"id"`
	Pid       int64           `json:"pid"`
	Name      string          `json:"name"`
	StableKey string          `json:"stable_key,omitempty"`
	Alias     string          `json:"alias"`
	Show      bool            `json:"show"`
	Sort      int             `json:"sort"`
	CreatedAt time.Time       `json:"created_at"`
	UpdatedAt time.Time       `json:"updated_at"`
	Children  []*CategoryTree `json:"children"` // 子分類信息
}
