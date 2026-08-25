package spider

import (
	"errors"
	"fmt"
	"log"
	"net/url"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/utils"
)

// ensureMasterCategoriesReady 在主站影片采集前确保本地分类与映射可用。
// 仅在分类表为空时触发，避免覆盖用户已调整的业务分类属性。
func ensureMasterCategoriesReady(s *model.FilmSource) error {
	if s == nil || s.Grade != model.MasterCollect {
		return nil
	}
	if repository.ExistsCategoryTree() {
		return nil
	}
	log.Printf("[Spider] 分类为空，采集前自动同步主站分类: name=%s id=%s uri=%s", s.Name, s.Id, s.Uri)
	if err := CollectCategory(s); err != nil {
		return fmt.Errorf("采集前同步主站分类失败: %w", err)
	}
	log.Printf("[Spider] 采集前主站分类同步完成: name=%s id=%s", s.Name, s.Id)
	return nil
}

// CollectCategory 影视分类采集
func CollectCategory(s *model.FilmSource) error {
	return collectCategoryWithMode(s, true)
}

// ResetCategory 重置主站分类并清除业务属性
func ResetCategory(s *model.FilmSource) error {
	return collectCategoryWithMode(s, false)
}

func collectCategoryWithMode(s *model.FilmSource, preserveBusinessFields bool) error {
	if s == nil {
		return errors.New("采集站信息不存在")
	}
	// 硬约束：分类树只能来自主站，禁止附属站写入分类树
	if s.Grade != model.MasterCollect {
		return fmt.Errorf("分类树只能从主采集站同步，当前站 grade=%d name=%s", s.Grade, s.Name)
	}
	// 获取分类树形数据
	categoryTree, err := spiderCore.GetCategoryTree(utils.RequestInfo{Uri: s.Uri, Params: url.Values{}})
	if err != nil {
		return fmt.Errorf("获取主站分类树失败: %w", err)
	}
	// 保存 tree 到 MySQL
	if preserveBusinessFields {
		err = repository.SaveCategoryTree(s.Id, categoryTree)
	} else {
		err = repository.ResetCategoryTree(s.Id, categoryTree)
	}
	if err != nil {
		return fmt.Errorf("保存主站分类树失败: %w", err)
	}
	return nil
}
