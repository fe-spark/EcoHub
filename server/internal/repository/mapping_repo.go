package repository

import (
	"fmt"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/support"

	"gorm.io/gorm"
)

const (
	mappingRuleLegacyUniqueIndex = "uidx_group_raw"
	mappingRuleEffectUniqueIndex = "uidx_group_raw_match_type"
)

// InitMappingEngine 从数据库加载映射规则并初始化内存缓存
func InitMappingEngine() {
	support.InitMappingEngine()
}

// ReloadMappingRules 强制重新从数据库加载所有映射规则
func ReloadMappingRules() {
	support.ReloadMappingRules()
}

func EnsureMappingRuleIndexes() error {
	if err := db.Mdb.AutoMigrate(&model.MappingRule{}); err != nil {
		return err
	}
	if err := db.Mdb.Unscoped().Where("deleted_at IS NOT NULL").Delete(&model.MappingRule{}).Error; err != nil {
		return err
	}
	if err := db.Mdb.Exec("UPDATE mapping_rules SET match_type = 'exact' WHERE match_type = '' OR match_type IS NULL").Error; err != nil {
		return err
	}
	if err := db.Mdb.Exec(fmt.Sprintf("ALTER TABLE %s DROP INDEX %s", model.MappingRule{}.TableName(), mappingRuleLegacyUniqueIndex)).Error; err != nil && !isIgnorableMappingRuleIndexError(err) {
		return err
	}
	createIndexSQL := fmt.Sprintf("CREATE UNIQUE INDEX %s ON %s (`group`, raw, match_type)", mappingRuleEffectUniqueIndex, model.MappingRule{}.TableName())
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
	return strings.Contains(message, "check that column/key exists") || strings.Contains(message, "duplicate key name")
}

type MappingRuleQuery struct {
	Group   string
	Keyword string
	Paging  *dto.Page
}

func buildMappingRuleQuery(query MappingRuleQuery) *gorm.DB {
	dbQuery := db.Mdb.Unscoped().Model(&model.MappingRule{}).Where("deleted_at IS NULL")
	group := strings.TrimSpace(query.Group)
	if group != "" {
		dbQuery = dbQuery.Where("`group` = ?", group)
	}
	keyword := strings.TrimSpace(query.Keyword)
	if keyword != "" {
		like := "%" + keyword + "%"
		dbQuery = dbQuery.Where("raw LIKE ? OR target LIKE ? OR remarks LIKE ?", like, like, like)
	}
	return dbQuery
}

func ListMappingRules(query MappingRuleQuery) ([]model.MappingRule, dto.Page, error) {
	page := dto.Page{Current: 1, PageSize: 20}
	if query.Paging != nil {
		page = *query.Paging
	}

	dbQuery := buildMappingRuleQuery(query)

	dto.GetPage(dbQuery, &page)
	list := make([]model.MappingRule, 0)
	err := dbQuery.Order("`group` ASC, raw ASC, id ASC").Offset((page.Current - 1) * page.PageSize).Limit(page.PageSize).Find(&list).Error
	return list, page, err
}

func ListAllMappingRules(query MappingRuleQuery) ([]model.MappingRule, error) {
	list := make([]model.MappingRule, 0)
	err := buildMappingRuleQuery(query).Order("`group` ASC, raw ASC, id ASC").Find(&list).Error
	return list, err
}

func FindMappingRulesByEffectPoint(group, raw, matchType string, excludeID uint) ([]model.MappingRule, error) {
	list := make([]model.MappingRule, 0)
	query := db.Mdb.Unscoped().Model(&model.MappingRule{}).
		Where("deleted_at IS NULL").
		Where("`group` = ? AND raw = ? AND match_type = ?", strings.TrimSpace(group), strings.TrimSpace(raw), strings.TrimSpace(matchType))
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

func IsCategoryMappingGroup(group string) bool {
	switch strings.TrimSpace(group) {
	case "CategoryRoot", "CategorySub":
		return true
	default:
		return false
	}
}

func CreateMappingRule(rule *model.MappingRule) error {
	if err := EnsureMappingRuleIndexes(); err != nil {
		return err
	}
	rule.MatchType = strings.TrimSpace(rule.MatchType)
	if rule.MatchType == "" {
		rule.MatchType = "exact"
	}
	if err := db.Mdb.Create(rule).Error; err != nil {
		return err
	}
	ReloadMappingRules()
	if IsCategoryMappingGroup(rule.Group) {
		if err := RebuildCategoriesFromSourceCategories(); err != nil {
			return err
		}
	}
	return nil
}

func UpdateMappingRule(rule *model.MappingRule) error {
	if err := EnsureMappingRuleIndexes(); err != nil {
		return err
	}
	oldRule, err := GetMappingRuleByID(rule.ID)
	if err != nil {
		return err
	}
	rule.MatchType = strings.TrimSpace(rule.MatchType)
	if rule.MatchType == "" {
		rule.MatchType = "exact"
	}
	if err := db.Mdb.Model(&model.MappingRule{}).Where("id = ?", rule.ID).Updates(map[string]any{
		"group":      rule.Group,
		"raw":        rule.Raw,
		"target":     rule.Target,
		"match_type": rule.MatchType,
		"remarks":    rule.Remarks,
	}).Error; err != nil {
		return err
	}
	ReloadMappingRules()
	if IsCategoryMappingGroup(rule.Group) || (oldRule != nil && IsCategoryMappingGroup(oldRule.Group)) {
		if err := RebuildCategoriesFromSourceCategories(); err != nil {
			return err
		}
	}
	return nil
}

func DeleteMappingRule(id uint) error {
	rule, err := GetMappingRuleByID(id)
	if err != nil {
		return err
	}
	if err := db.Mdb.Unscoped().Delete(&model.MappingRule{}, id).Error; err != nil {
		return err
	}
	ReloadMappingRules()
	if rule != nil && IsCategoryMappingGroup(rule.Group) {
		if err := RebuildCategoriesFromSourceCategories(); err != nil {
			return err
		}
	}
	return nil
}

func GetAreaMapping() map[string]string {
	return support.GetAreaMapping()
}

func GetLangMapping() map[string]string {
	return support.GetLangMapping()
}

func GetFilterMap() map[string]bool {
	return support.GetFilterMap()
}

func GetAttributeMapping() map[string]string {
	return support.GetAttributeMapping()
}

func GetPlotMapping() map[string]string {
	return support.GetPlotMapping()
}

func GetCategoryNameFromCache(id int64) (string, bool) {
	return support.GetCategoryNameFromCache(id)
}

func SetCategoryNameCache(id int64, name string) {
	support.SetCategoryNameCache(id, name)
}
