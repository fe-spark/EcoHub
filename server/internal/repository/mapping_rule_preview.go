package repository

import (
	"errors"
	"regexp"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
)

type MappingRulePreviewHit struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
}

type MappingRulePreviewResult struct {
	SourceName string                  `json:"sourceName"`
	Hits       []MappingRulePreviewHit `json:"hits"`
	Target     string                  `json:"target"`
	Keep       []MappingRulePreviewHit `json:"keep"`
}

func PreviewMappingRule(sourceID, group, raw, matchType, target string) (MappingRulePreviewResult, error) {
	sourceID = strings.TrimSpace(sourceID)
	group = strings.TrimSpace(group)
	raw = strings.TrimSpace(raw)
	matchType = strings.TrimSpace(matchType)
	target = strings.TrimSpace(target)
	empty := MappingRulePreviewResult{Hits: []MappingRulePreviewHit{}, Keep: []MappingRulePreviewHit{}, Target: target}
	if sourceID == "" {
		return empty, errors.New("分类规则必须指定采集站")
	}
	if !IsCategoryMappingGroup(group) {
		return empty, errors.New("仅一级/二级分类规则支持合并预览")
	}
	if raw == "" {
		return empty, errors.New("原始值不能为空")
	}
	if matchType != "exact" && matchType != "regex" {
		return empty, errors.New("不支持的匹配方式")
	}

	var matcher func(string) bool
	if matchType == "regex" {
		pattern, err := regexp.Compile(raw)
		if err != nil {
			return empty, errors.New("正则表达式不合法")
		}
		matcher = pattern.MatchString
	} else {
		matcher = func(name string) bool { return name == raw }
	}

	var source model.FilmSource
	if err := db.Mdb.Where("id = ?", sourceID).First(&source).Error; err != nil {
		return empty, errors.New("采集站不存在")
	}
	empty.SourceName = strings.TrimSpace(source.Name)

	query := db.Mdb.Where("source_id = ?", sourceID)
	if group == "CategoryRoot" {
		query = query.Where("parent_source_type_id = 0")
	} else {
		query = query.Where("parent_source_type_id > 0")
	}
	var rows []model.SourceCategory
	if err := query.Order("sort ASC, source_type_id ASC").Find(&rows).Error; err != nil {
		return empty, err
	}

	hits := make([]MappingRulePreviewHit, 0)
	hitIDs := map[int64]struct{}{}
	for _, row := range rows {
		name := strings.TrimSpace(row.RawName)
		if name == "" || !matcher(name) {
			continue
		}
		hits = append(hits, MappingRulePreviewHit{ID: row.SourceTypeId, Name: name})
		hitIDs[row.SourceTypeId] = struct{}{}
	}
	keep := make([]MappingRulePreviewHit, 0)
	if target != "" {
		for _, row := range rows {
			name := strings.TrimSpace(row.RawName)
			if name != target {
				continue
			}
			if _, ok := hitIDs[row.SourceTypeId]; ok {
				continue
			}
			keep = append(keep, MappingRulePreviewHit{ID: row.SourceTypeId, Name: name})
		}
	}
	return MappingRulePreviewResult{
		SourceName: empty.SourceName,
		Hits:       hits,
		Target:     target,
		Keep:       keep,
	}, nil
}
