package model

// ConfigBackupVersion 配置备份格式版本（导入时校验）
const ConfigBackupVersion = 1

// ConfigBackup 站点配置备份包（不含影视库存与账号密码）
type ConfigBackup struct {
	Version    int                `json:"version"`
	ExportedAt string             `json:"exportedAt"`
	AppVersion string             `json:"appVersion,omitempty"`
	Site       *BasicConfig       `json:"site,omitempty"`
	FilmSources []FilmSource      `json:"filmSources,omitempty"`
	CronTasks  []FilmCollectTask  `json:"cronTasks,omitempty"`
	Banners    Banners            `json:"banners,omitempty"`
	Notify     *NotifyConfig      `json:"notify,omitempty"`
	MappingRules []MappingRuleExport `json:"mappingRules,omitempty"`
}

// MappingRuleExport 映射规则导出结构（不带数据库 ID）
type MappingRuleExport struct {
	Group     string `json:"group"`
	Raw       string `json:"raw"`
	Target    string `json:"target"`
	MatchType string `json:"matchType"`
	Remarks   string `json:"remarks"`
}

// ConfigBackupModules 导入时选择恢复的模块
type ConfigBackupModules struct {
	Site         bool `json:"site"`
	FilmSources  bool `json:"filmSources"`
	CronTasks    bool `json:"cronTasks"`
	Banners      bool `json:"banners"`
	Notify       bool `json:"notify"`
	MappingRules bool `json:"mappingRules"`
}

// ConfigBackupImportRequest 配置备份导入请求
type ConfigBackupImportRequest struct {
	Password string              `json:"password"`
	Modules  ConfigBackupModules `json:"modules"`
	Backup   ConfigBackup        `json:"backup"`
}

// Any 是否至少勾选一个模块
func (m ConfigBackupModules) Any() bool {
	return m.Site || m.FilmSources || m.CronTasks || m.Banners || m.Notify || m.MappingRules
}
