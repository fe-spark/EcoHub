package service

import (
	"errors"
	"fmt"
	"time"

	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/spider/conver"
)

type FilmService struct{}

var FilmSvc = new(FilmService)

// GetFilmPage 获取影片检索信息分页数据
func (s *FilmService) GetFilmPage(vo model.SearchVo) []model.SearchInfo {
	return filmrepo.GetSearchPage(vo)
}

// GetSearchOptions 获取影片检索的select的选项options
func (s *FilmService) GetSearchOptions() map[string]any {
	options := make(map[string]any)
	tree := repository.GetActiveCategoryTree()
	tree.Name = "全部分类"
	options["class"] = conver.ConvertCategoryList(&tree)
	options["year"] = make([]map[string]string, 0)
	tagGroup := make(map[int64]map[string]any)
	if tree.Children != nil {
		for _, t := range tree.Children {
			option := filmrepo.GetSearchOptions(model.SearchTagsVO{Pid: t.Id})
			if len(option) > 0 {
				tagGroup[t.Id] = option
				if v, ok := options["year"].([]map[string]string); !ok || len(v) == 0 {
					options["year"] = tagGroup[t.Id]["Year"]
				}
			}

		}
	}
	options["tags"] = tagGroup
	return options
}

// SaveFilmDetail 自定义上传保存影片信息
func (s *FilmService) SaveFilmDetail(fd model.FilmDetailVo) error {
	now := time.Now()
	fd.UpdateTime = now.Format(time.DateTime)
	fd.AddTime = fd.UpdateTime
	if fd.Id == 0 {
		fd.Id = now.Unix()
	}
	detail, err := conver.CovertFilmDetailVo(fd)
	if err != nil || detail.PlayList == nil {
		return errors.New("影片参数格式异常或缺少关键信息")
	}

	// 手动上传的影片，尝试归属于当前主站 ID，如果没有主站则标记为 "manual"
	sourceId := "manual"
	if master := repository.GetCollectSourceListByGrade(model.MasterCollect); len(master) > 0 {
		sourceId = master[0].Id
	}

	return filmrepo.SaveDetail(sourceId, detail)
}

// DelFilm 删除分类影片
func (s *FilmService) DelFilm(id int64) error {
	sInfo := filmrepo.GetSearchInfoById(id)
	if sInfo == nil || sInfo.ID == 0 {
		return errors.New("影片信息不存在")
	}
	return filmrepo.DelFilmSearch(id)
}

// GetFilmClassTree 获取影片分类信息
func (s *FilmService) GetFilmClassTree() model.CategoryTree {
	return repository.GetCategoryTree()
}

// GetFilmClassById 通过ID获取影片分类信息
func (s *FilmService) GetFilmClassById(id int64) *model.CategoryTree {
	tree := repository.GetCategoryTree()
	for _, c := range tree.Children {
		if c.Id == id {
			return c
		}
		if c.Children != nil {
			for _, subC := range c.Children {
				if subC.Id == id {
					return subC
				}
			}
		}
	}
	return nil
}

// UpdateClass 更新分类信息
func (s *FilmService) UpdateClass(class model.CategoryTree) error {
	updates := make(map[string]any)
	if class.Name != "" {
		updates["name"] = class.Name
	}
	updates["show"] = class.Show

	// 1. 查找旧状态以判断是否需要同步处理搜索可见性
	oldClass := s.GetFilmClassById(class.Id)
	if oldClass == nil {
		return errors.New("需要更新的分类信息不存在")
	}

	// 2. 如果是父类且 Show 状态变更，处理子类可见性
	if oldClass.Pid == 0 && oldClass.Show != class.Show && oldClass.Children != nil {
		for _, subC := range oldClass.Children {
			var err error
			if class.Show {
				err = filmrepo.RecoverFilmSearch(subC.Id)
			} else {
				err = filmrepo.ShieldFilmSearch(subC.Id)
			}
			if err != nil {
				return fmt.Errorf("分类 [%d] 搜索可见性更新失败: %s", subC.Id, err.Error())
			}
		}
	} else if oldClass.Pid != 0 && oldClass.Show != class.Show {
		// 如果是子类且 Show 状态变更
		var err error
		if class.Show {
			err = filmrepo.RecoverFilmSearch(class.Id)
		} else {
			err = filmrepo.ShieldFilmSearch(class.Id)
		}
		if err != nil {
			return err
		}
	}

	// 3. 执行原子更新并清除缓存
	return repository.UpdateCategoryStatus(class.Id, updates)
}

// DelClass 删除分类信息
func (s *FilmService) DelClass(id int64) error {
	// 简单的删除逻辑，删除后清除缓存
	if err := repository.UpdateCategoryStatus(id, map[string]any{"deleted_at": time.Now()}); err != nil {
		return err
	}
	return nil
}
