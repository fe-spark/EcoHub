package spider

import (
	"errors"
	"fmt"

	"server/internal/model"
	"server/internal/repository"
)

// ensureMasterCategoriesReady 采集前确认该站已经有自己的分类副本。
// 分类在添加或编辑采集站时拉取。采集过程中不再临时请求分类。
func ensureMasterCategoriesReady(s *model.FilmSource) error {
	if s == nil {
		return nil
	}
	if repository.SourceHasCategoryMapping(s.Id) {
		return nil
	}
	return fmt.Errorf("采集站 %s 还没有分类，请在采集中心重新保存该站后再采集", s.Name)
}

// CollectCategory 影视分类采集
func CollectCategory(s *model.FilmSource) (int, error) {
	if s == nil {
		return 0, errors.New("采集站信息不存在")
	}
	pageCount := 0
	err := WithSourceExclusive(s.Id, func() error {
		if s.Id != "" && repository.FindCollectSourceById(s.Id) == nil {
			return errors.New("采集站信息不存在")
		}
		tree, count, err := FetchCategoryTree(s)
		if err != nil {
			return err
		}
		pageCount = count
		if err := repository.SaveCategoryTree(s.Id, tree); err != nil {
			return fmt.Errorf("保存采集站分类失败: %w", err)
		}
		return nil
	})
	return pageCount, err
}

// FetchCategoryTree 发起网络请求获取采集源分类树与全站总页，并同步更新采集站全站总页
func FetchCategoryTree(s *model.FilmSource) (*model.CategoryTree, int, error) {
	if s == nil {
		return nil, 0, errors.New("采集站信息不存在")
	}
	req, err := requestForSource(s)
	if err != nil {
		return nil, 0, err
	}
	categoryTree, pageCount, err := ResolveCollector(s.ResolveFormat()).GetCategoryTree(req)
	if err != nil {
		return nil, 0, fmt.Errorf("获取采集站分类树失败: %w", err)
	}
	if s.Id != "" && pageCount > 0 {
		_ = repository.UpdateSitePageTotal(s.Id, pageCount)
	}
	return categoryTree, pageCount, nil
}

// ApplySourceCategory 在互斥锁内将分类树写入库，并根据选项清空数据
func ApplySourceCategory(s *model.FilmSource, tree *model.CategoryTree, clearData bool) (repository.CategoryApplyResult, error) {
	if s == nil {
		return repository.CategoryApplyResult{}, errors.New("采集站信息不存在")
	}
	var res repository.CategoryApplyResult
	err := WithSourceExclusive(s.Id, func() error {
		if s.Id != "" && repository.FindCollectSourceById(s.Id) == nil {
			return errors.New("采集站信息不存在")
		}
		applyRes, applyErr := repository.ApplyCategoryTree(s.Id, tree, clearData)
		if applyErr != nil {
			return fmt.Errorf("保存采集站分类失败: %w", applyErr)
		}
		res = applyRes
		return nil
	})
	return res, err
}
