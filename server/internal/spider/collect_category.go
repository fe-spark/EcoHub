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

// ensureMasterCategoriesReady 在影片采集前确保本地分类与映射可用。
// 仅在分类表为空时触发，避免覆盖用户已调整的业务分类属性。
func ensureMasterCategoriesReady(s *model.FilmSource) error {
	if s == nil {
		return nil
	}
	if repository.ExistsCategoryTree() {
		return nil
	}
	log.Printf("[Spider] 分类为空，采集前自动同步站点分类: name=%s id=%s uri=%s", s.Name, s.Id, s.Uri)
	if err := CollectCategory(s); err != nil {
		return fmt.Errorf("采集前同步站点分类失败: %w", err)
	}
	log.Printf("[Spider] 采集前站点分类同步完成: name=%s id=%s", s.Name, s.Id)
	return nil
}

// CollectCategory 影视分类采集
func CollectCategory(s *model.FilmSource) error {
	return collectCategoryWithMode(s, true)
}

// ResetCategory 重置分类并清除业务属性
func ResetCategory(s *model.FilmSource) error {
	return collectCategoryWithMode(s, false)
}

func collectCategoryWithMode(s *model.FilmSource, preserveBusinessFields bool) error {
	if s == nil {
		return errors.New("采集站信息不存在")
	}
	// 获取分类树形数据
	req := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	if ok, proxy := repository.ResolveSourceProxy(s.Id); ok {
		req.ProxyURL = proxy
	}
	categoryTree, err := ResolveCollector(s.ResolveFormat()).GetCategoryTree(req)
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
