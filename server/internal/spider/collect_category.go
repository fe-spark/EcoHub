package spider

import (
	"errors"
	"fmt"
	"net/url"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/utils"
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
func CollectCategory(s *model.FilmSource) error {
	return collectCategoryWithMode(s, true)
}

// ResetCategory 重置分类并清除业务属性
func ResetCategory(s *model.FilmSource) error {
	return collectCategoryWithMode(s, false)
}

// FetchCategoryTree 仅发起网络请求获取采集源分类树，不写库
func FetchCategoryTree(s *model.FilmSource) (*model.CategoryTree, error) {
	if s == nil {
		return nil, errors.New("采集站信息不存在")
	}
	req := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	if s.ProxyCollect {
		if ok, proxy := repository.ResolveSpiderProxy(); ok {
			req.ProxyURL = proxy
		}
	} else if s.Id != "" {
		if ok, proxy := repository.ResolveSourceProxy(s.Id); ok {
			req.ProxyURL = proxy
		}
	}
	categoryTree, err := ResolveCollector(s.ResolveFormat()).GetCategoryTree(req)
	if err != nil {
		return nil, fmt.Errorf("获取采集站分类树失败: %w", err)
	}
	return categoryTree, nil
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

func collectCategoryWithMode(s *model.FilmSource, preserveBusinessFields bool) error {
	if s == nil {
		return errors.New("采集站信息不存在")
	}
	return WithSourceExclusive(s.Id, func() error {
		if s.Id != "" && repository.FindCollectSourceById(s.Id) == nil {
			return errors.New("采集站信息不存在")
		}
		return saveSourceCategories(s, preserveBusinessFields)
	})
}

func saveSourceCategories(s *model.FilmSource, preserveBusinessFields bool) error {
	categoryTree, err := FetchCategoryTree(s)
	if err != nil {
		return err
	}
	if preserveBusinessFields {
		err = repository.SaveCategoryTree(s.Id, categoryTree)
	} else {
		err = repository.ResetCategoryTree(s.Id, categoryTree)
	}
	if err != nil {
		return fmt.Errorf("保存采集站分类失败: %w", err)
	}
	return nil
}
