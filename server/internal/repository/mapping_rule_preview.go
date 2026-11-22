package repository

import (
	"errors"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
)

// MappingRuleSourceCategory 分类规则预览上下文中的采集站原始分类项
type MappingRuleSourceCategory struct {
	ID       int64  `json:"id"`
	ParentID int64  `json:"parentId"`
	Name     string `json:"name"`
	Sort     int    `json:"sort"`
	Show     bool   `json:"show"`
}

// MappingRuleContextResult 分类规则配置弹窗所需的采集站原始分类与已保存规则集合
type MappingRuleContextResult struct {
	SourceName    string                      `json:"sourceName"`
	RawCategories []MappingRuleSourceCategory `json:"rawCategories"`
	Rules         []model.MappingRule         `json:"rules"`
}

// GetMappingRuleContext 一次性拉取指定采集站的原始分类及全部分类映射规则，供前端本地计算冲突与全量分类树预览。
func GetMappingRuleContext(sourceID string) (MappingRuleContextResult, error) {
	sourceID = strings.TrimSpace(sourceID)
	empty := MappingRuleContextResult{
		RawCategories: []MappingRuleSourceCategory{},
		Rules:         []model.MappingRule{},
	}
	if sourceID == "" {
		return empty, errors.New("分类规则必须指定采集站")
	}

	var source model.FilmSource
	if err := db.Mdb.Where("id = ?", sourceID).First(&source).Error; err != nil {
		return empty, errors.New("采集站不存在")
	}

	var allRows []model.SourceCategory
	if err := db.Mdb.Where("source_id = ?", sourceID).
		Order("depth ASC, sort ASC, source_type_id ASC").
		Find(&allRows).Error; err != nil {
		return empty, err
	}

	placements, err := sourceTypePlacements(sourceID)
	if err != nil {
		return empty, err
	}

	rawCategories := make([]MappingRuleSourceCategory, 0, len(allRows))
	for _, row := range allRows {
		name := strings.TrimSpace(row.RawName)
		if row.SourceTypeId <= 0 || name == "" {
			continue
		}
		sortVal := row.Sort
		showVal := true
		if placement, ok := placements[row.SourceTypeId]; ok {
			sortVal = placement.Sort
			showVal = placement.Show
		}
		rawCategories = append(rawCategories, MappingRuleSourceCategory{
			ID:       row.SourceTypeId,
			ParentID: row.ParentSourceTypeId,
			Name:     name,
			Sort:     sortVal,
			Show:     showVal,
		})
	}

	rules := make([]model.MappingRule, 0)
	if err := db.Mdb.Unscoped().Model(&model.MappingRule{}).
		Where("deleted_at IS NULL").
		Where("source_id = ? AND `group` IN ?", sourceID, []string{"CategoryRoot", "CategorySub"}).
		Order("id ASC").
		Find(&rules).Error; err != nil {
		return empty, err
	}

	return MappingRuleContextResult{
		SourceName:    strings.TrimSpace(source.Name),
		RawCategories: rawCategories,
		Rules:         rules,
	}, nil
}
