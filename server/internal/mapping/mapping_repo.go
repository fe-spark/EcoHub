package mapping

import (
	"fmt"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repobase"
	"server/internal/taxonomy"

	"gorm.io/gorm"
)

const (
	mappingRuleLegacyUniqueIndex = "uidx_group_raw"
	mappingRuleEffectUniqueIndex = "uidx_group_raw_match_type"
	mappingRuleSourceUniqueIndex = "uidx_source_group_raw_match_type"
)

// InitMappingEngine 从数据库加载映射规则并初始化内存缓存
func InitMappingEngine() {
	taxonomy.InitMappingEngine()
}

// ReloadMappingRules 强制重新从数据库加载所有映射规则
func ReloadMappingRules() {
	taxonomy.ReloadMappingRules()
}

func ResetMappingRules() error {
	if err := repobase.TruncateTable(db.Mdb, model.MappingRule{}.TableName()); err != nil {
		return err
	}
	TouchRuleVersion()
	ReloadMappingRules()
	return nil
}

func TouchRuleVersion() {
	taxonomy.TouchRuleVersion()
}

func GetRuleVersion() string {
	return taxonomy.GetRuleVersion()
}

func EnsureMappingRuleIndexes() error {
	if err := db.Mdb.AutoMigrate(&model.MappingRule{}); err != nil {
		return err
	}
	if err := db.Mdb.Unscoped().Where("deleted_at IS NOT NULL").Delete(&model.MappingRule{}).Error; err != nil {
		return err
	}
	if err := db.Mdb.Exec("UPDATE " + model.TableMappingRule + " SET match_type = 'exact' WHERE match_type = '' OR match_type IS NULL").Error; err != nil {
		return err
	}

	isSQLite := db.Mdb.Dialector != nil && db.Mdb.Dialector.Name() == "sqlite"
	table := model.MappingRule{}.TableName()
	if isSQLite {
		_ = db.Mdb.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", mappingRuleLegacyUniqueIndex)).Error
		_ = db.Mdb.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", mappingRuleEffectUniqueIndex)).Error
		createIndexSQL := fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS %s ON %s (source_id, `group`, raw, match_type)", mappingRuleSourceUniqueIndex, table)
		if err := db.Mdb.Exec(createIndexSQL).Error; err != nil && !isIgnorableMappingRuleIndexError(err) {
			return err
		}
		return nil
	}

	for _, name := range []string{mappingRuleLegacyUniqueIndex, mappingRuleEffectUniqueIndex} {
		if err := db.Mdb.Exec(fmt.Sprintf("ALTER TABLE %s DROP INDEX %s", table, name)).Error; err != nil && !isIgnorableMappingRuleIndexError(err) {
			return err
		}
	}
	createIndexSQL := fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (source_id, `group`, raw, match_type)", mappingRuleSourceUniqueIndex, table)
	if err := db.Mdb.Exec(createIndexSQL).Error; err != nil && !isIgnorableMappingRuleIndexError(err) {
		return err
	}
	return nil
}

func isIgnorableMappingRuleIndexError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "check that column/key exists") ||
		strings.Contains(message, "duplicate key name") ||
		strings.Contains(message, "already exists") ||
		strings.Contains(message, "no such index")
}

type MappingRuleQuery struct {
	Group    string
	Keyword  string
	SourceId string
	Paging   *dto.Page
}

func buildMappingRuleQuery(query MappingRuleQuery) *gorm.DB {
	dbQuery := db.Mdb.Unscoped().Model(&model.MappingRule{}).Where("deleted_at IS NULL")
	group := strings.TrimSpace(query.Group)
	sourceID := strings.TrimSpace(query.SourceId)
	if group == "CategoryAll" {
		if sourceID == "" {
			return dbQuery.Where("1 = 0")
		}
		dbQuery = dbQuery.Where("`group` IN ? AND source_id = ?", []string{"CategoryRoot", "CategorySub"}, sourceID)
	} else {
		if group != "" {
			dbQuery = dbQuery.Where("`group` = ?", group)
		}
		if IsCategoryMappingGroup(group) {
			if sourceID == "" {
				dbQuery = dbQuery.Where("1 = 0")
			} else {
				dbQuery = dbQuery.Where("source_id = ?", sourceID)
			}
		} else if sourceID != "" {
			dbQuery = dbQuery.Where("source_id = ?", sourceID)
		}
	}
	keyword := strings.TrimSpace(query.Keyword)
	if keyword != "" {
		like := "%" + keyword + "%"
		dbQuery = dbQuery.Where("(raw LIKE ? OR target LIKE ? OR remarks LIKE ?)", like, like, like)
	}
	return dbQuery
}

func ListMappingRules(query MappingRuleQuery) ([]model.MappingRule, dto.Page, error) {
	page := dto.Page{Current: 1, PageSize: 20}
	if query.Paging != nil {
		page = *query.Paging
	}

	dbQuery := buildMappingRuleQuery(query)

	if err := dto.GetPage(dbQuery, &page); err != nil {
		return nil, page, err
	}
	list := make([]model.MappingRule, 0)
	err := dbQuery.Order("`group` ASC, raw ASC, id ASC").Offset((page.Current - 1) * page.PageSize).Limit(page.PageSize).Find(&list).Error
	return list, page, err
}

func ListAllMappingRules(query MappingRuleQuery) ([]model.MappingRule, error) {
	list := make([]model.MappingRule, 0)
	err := buildMappingRuleQuery(query).Order("`group` ASC, raw ASC, id ASC").Find(&list).Error
	return list, err
}

// CountCategoryMappingRulesByGroup 统计指定采集站的一级/二级分类规则总数
func CountCategoryMappingRulesByGroup(sourceID string) (map[string]int, error) {
	totals := map[string]int{
		"CategoryRoot": 0,
		"CategorySub":  0,
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return totals, nil
	}
	var rows []struct {
		Group string `gorm:"column:group"`
		Total int64  `gorm:"column:total"`
	}
	err := db.Mdb.Unscoped().Model(&model.MappingRule{}).
		Select("`group`, COUNT(*) AS total").
		Where("deleted_at IS NULL").
		Where("source_id = ? AND `group` IN ?", sourceID, []string{"CategoryRoot", "CategorySub"}).
		Group("`group`").
		Scan(&rows).Error
	if err != nil {
		return nil, err
	}
	for _, r := range rows {
		totals[r.Group] = int(r.Total)
	}
	return totals, nil
}

func FindMappingRulesByEffectPoint(sourceID, group, raw, matchType string, excludeID uint) ([]model.MappingRule, error) {
	list := make([]model.MappingRule, 0)
	query := db.Mdb.Unscoped().Model(&model.MappingRule{}).
		Where("deleted_at IS NULL").
		Where("source_id = ? AND `group` = ? AND raw = ? AND match_type = ?", strings.TrimSpace(sourceID), strings.TrimSpace(group), strings.TrimSpace(raw), strings.TrimSpace(matchType))
	if excludeID > 0 {
		query = query.Where("id <> ?", excludeID)
	}
	err := query.Order("id ASC").Find(&list).Error
	return list, err
}

func ListMappingRuleGroups() []string {
	groups := []string{"Area", "Language", "Filter", "Attribute", "Plot", "CategoryRoot", "CategorySub"}
	return groups
}

// IsDuplicateKeyError 判断是否为数据库唯一键冲突。
func IsDuplicateKeyError(err error) bool {
	return db.IsDuplicateKeyError(err)
}

func GetMappingRuleByID(id uint) (*model.MappingRule, error) {
	if id == 0 {
		return nil, nil
	}
	var rule model.MappingRule
	if err := db.Mdb.Where("id = ?", id).First(&rule).Error; err != nil {
		return nil, err
	}
	return &rule, nil
}

// GetMappingRulesByIDs 按 ID 列表读取多条映射规则。
func GetMappingRulesByIDs(ids []uint) ([]model.MappingRule, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var rules []model.MappingRule
	if err := db.Mdb.Where("id IN ?", ids).Find(&rules).Error; err != nil {
		return nil, err
	}
	return rules, nil
}

func IsCategoryMappingGroup(group string) bool {
	switch strings.TrimSpace(group) {
	case "CategoryRoot", "CategorySub":
		return true
	default:
		return false
	}
}

func CreateMappingRule(rule *model.MappingRule) error {
	rule.MatchType = strings.TrimSpace(rule.MatchType)
	if rule.MatchType == "" {
		rule.MatchType = "exact"
	}
	if err := db.Mdb.Create(rule).Error; err != nil {
		return err
	}
	TouchRuleVersion()
	ReloadMappingRules()
	repobase.ClearIndexPageCache()
	return nil
}

func UpdateMappingRule(rule *model.MappingRule) error {
	rule.MatchType = strings.TrimSpace(rule.MatchType)
	if rule.MatchType == "" {
		rule.MatchType = "exact"
	}
	if err := db.Mdb.Model(&model.MappingRule{}).Where("id = ?", rule.ID).Updates(map[string]any{
		"source_id":  rule.SourceId,
		"group":      rule.Group,
		"raw":        rule.Raw,
		"target":     rule.Target,
		"match_type": rule.MatchType,
		"remarks":    rule.Remarks,
	}).Error; err != nil {
		return err
	}
	TouchRuleVersion()
	ReloadMappingRules()
	repobase.ClearIndexPageCache()
	return nil
}

func DeleteMappingRule(id uint) error {
	return DeleteMappingRules([]uint{id})
}

// DeleteMappingRules 批量硬删映射规则并单次更新版本与缓存。
func DeleteMappingRules(ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	if err := db.Mdb.Unscoped().Where("id IN ?", ids).Delete(&model.MappingRule{}).Error; err != nil {
		return err
	}
	TouchRuleVersion()
	ReloadMappingRules()
	repobase.ClearIndexPageCache()
	return nil
}

func DeleteMappingRulesBySourceTx(tx *gorm.DB, sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if tx == nil || sourceID == "" || !tx.Migrator().HasTable(&model.MappingRule{}) {
		return nil
	}
	return tx.Unscoped().Where("source_id = ?", sourceID).Delete(&model.MappingRule{}).Error
}

// ReplaceMappingRules 用导出规则整体替换映射规则表
func ReplaceMappingRules(exports []model.MappingRuleExport) error {
	if err := EnsureMappingRuleIndexes(); err != nil {
		return err
	}
	if err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Unscoped().Delete(&model.MappingRule{}).Error; err != nil {
			return err
		}
		if len(exports) == 0 {
			return nil
		}
		rows := make([]model.MappingRule, 0, len(exports))
		for _, item := range exports {
			group := strings.TrimSpace(item.Group)
			matchType := strings.TrimSpace(item.MatchType)
			if matchType == "" {
				matchType = "exact"
			}
			sourceID := strings.TrimSpace(item.SourceId)
			if IsCategoryMappingGroup(group) {
				if sourceID == "" {
					continue
				}
			} else {
				sourceID = ""
			}
			rows = append(rows, model.MappingRule{
				SourceId:  sourceID,
				Group:     group,
				Raw:       strings.TrimSpace(item.Raw),
				Target:    strings.TrimSpace(item.Target),
				MatchType: matchType,
				Remarks:   strings.TrimSpace(item.Remarks),
			})
		}
		if len(rows) == 0 {
			return nil
		}
		return tx.Create(&rows).Error
	}); err != nil {
		return err
	}
	TouchRuleVersion()
	ReloadMappingRules()
	repobase.ClearIndexPageCache()
	return nil
}

func GetAreaMapping() map[string]string {
	return taxonomy.GetAreaMapping()
}

func GetLangMapping() map[string]string {
	return taxonomy.GetLangMapping()
}

func GetFilterMap() map[string]bool {
	return taxonomy.GetFilterMap()
}

func GetAttributeMapping() map[string]string {
	return taxonomy.GetAttributeMapping()
}

func GetPlotMapping() map[string]string {
	return taxonomy.GetPlotMapping()
}

func GetCategoryNameFromCache(id int64) (string, bool) {
	return taxonomy.GetCategoryNameFromCache(id)
}

func SetCategoryNameCache(id int64, name string) {
	taxonomy.SetCategoryNameCache(id, name)
}
