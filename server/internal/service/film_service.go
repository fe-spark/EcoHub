package service

import (
	"errors"
	"fmt"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	filmcache "server/internal/repository/film/cache"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/film/writer"
	"server/internal/spider/converter"
	"strings"
	"time"
)

type FilmService struct{}

var FilmSvc = new(FilmService)

// GetFilmPage 获取影片检索信息分页数据
func (s *FilmService) GetFilmPage(vo *model.SearchVo) ([]model.FilmIndex, error) {
	if vo == nil {
		return nil, errors.New("search params cannot be nil")
	}
	if strings.TrimSpace(vo.SourceId) == "" {
		if activeSrc := repository.GetActiveCollectSource(); activeSrc != nil {
			vo.SourceId = activeSrc.Id
		} else if sources, err := repository.GetCollectSourceList(); err == nil && len(sources) > 0 {
			vo.SourceId = sources[0].Id
		}
	}
	return filmsnapshot.GetSearchPageReadModel(*vo)
}

// GetSearchOptions 获取影片检索的select的选项options
func (s *FilmService) GetSearchOptions(sourceIdOpt ...string) (map[string]any, error) {
	options := make(map[string]any)

	var sourceId string
	if len(sourceIdOpt) > 0 {
		sourceId = strings.TrimSpace(sourceIdOpt[0])
	}

	sources, err := repository.GetCollectSourceList()
	if err != nil {
		return nil, err
	}
	defaultSourceId := ""
	if active := repository.GetActiveCollectSource(); active != nil {
		defaultSourceId = active.Id
	} else if len(sources) > 0 {
		defaultSourceId = sources[0].Id
	}
	if sourceId == "" {
		sourceId = defaultSourceId
	}

	tree, err := repository.GetActiveCategoryTree(sourceId)
	if err != nil {
		return nil, err
	}
	tree.Name = "全部分类"
	options["sources"] = sources
	options["defaultSourceId"] = defaultSourceId
	options["currentSourceId"] = sourceId
	options["class"] = converter.ConvertCategoryList(&tree)
	options["categoryTree"] = tree.Children
	return options, nil
}

// SaveFilmDetail 自定义上传保存影片信息
func (s *FilmService) SaveFilmDetail(fd model.FilmDetailVo) error {
	now := time.Now()
	fd.UpdateTime = now.Format(time.DateTime)
	fd.AddTime = fd.UpdateTime
	if fd.Id == 0 {
		fd.Id = now.Unix()
	}
	detail, err := converter.CovertFilmDetailVo(fd)
	if err != nil {
		return errors.New("影片参数格式异常或缺少关键信息")
	}

	sourceId := strings.TrimSpace(fd.SourceId)
	if sourceId != "" {
		if src := repository.FindCollectSourceById(sourceId); src == nil {
			return fmt.Errorf("指定的采集站不存在: %s", sourceId)
		}
	} else {
		if active := repository.GetActiveCollectSource(); active != nil {
			sourceId = active.Id
		} else if sources, err := repository.GetCollectSourceList(); err == nil && len(sources) > 0 {
			sourceId = sources[0].Id
		} else {
			sourceId = "manual"
		}
	}

	if err := writer.SaveDetail(sourceId, detail); err != nil {
		return err
	}
	return nil
}

// DelFilms 删除影片（支持单个与批量）
func (s *FilmService) DelFilms(ids []int64) error {
	if len(ids) == 0 {
		return nil
	}
	return filmrepo.DelFilmSearchBatch(ids)
}

// DelFilm 删除分类影片
func (s *FilmService) DelFilm(id int64) error {
	return s.DelFilms([]int64{id})
}

// GetFilmClassTree 获取影片分类信息
func (s *FilmService) GetFilmClassTree() model.CategoryTree {
	return repository.GetCategoryTree()
}

// ClassTreeScope 分类管理按采集站查看时的树和站点选项。
type ClassTreeScope struct {
	Tree            model.CategoryTree
	Sources         []model.FilmSource
	DefaultSourceId string
	CurrentSourceId string
}

// GetScopedClassTree 按采集站返回该站分类。未指定站点时用首选站。
func (s *FilmService) GetScopedClassTree(sourceID string) (ClassTreeScope, error) {
	sources, err := repository.GetCollectSourceList()
	if err != nil {
		return ClassTreeScope{}, err
	}
	defaultSourceID := ""
	if active := repository.GetActiveCollectSource(); active != nil {
		defaultSourceID = active.Id
	}
	if defaultSourceID == "" && len(sources) > 0 {
		defaultSourceID = sources[0].Id
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		sourceID = defaultSourceID
	}
	tree := repository.GetSourceBoundCategoryTree(sourceID)
	return ClassTreeScope{
		Tree:            tree,
		Sources:         sources,
		DefaultSourceId: defaultSourceID,
		CurrentSourceId: sourceID,
	}, nil
}

// GetFilmClassById 通过ID获取影片分类信息
func (s *FilmService) GetFilmClassById(id int64) *model.CategoryTree {
	return repository.GetCategoryTreeByID(id)
}

func sanitizeCategoryTreeNodes(nodes []*model.CategoryTree) []*model.CategoryTree {
	if len(nodes) == 0 {
		return []*model.CategoryTree{}
	}
	res := make([]*model.CategoryTree, 0, len(nodes))
	for _, node := range nodes {
		if node == nil || node.Id <= 0 {
			continue
		}
		res = append(res, &model.CategoryTree{
			Id:       node.Id,
			Name:     strings.TrimSpace(node.Name),
			Show:     node.Show,
			Children: sanitizeCategoryTreeNodes(node.Children),
		})
	}
	return res
}

func (s *FilmService) SaveSourceClassTree(sourceID string, nodes []*model.CategoryTree) error {
	if len(nodes) == 0 {
		return errors.New("分类结构不能为空")
	}
	if err := repository.SaveSourceCategoryOrder(sourceID, nodes); err != nil {
		return err
	}
	if err := filmsnapshot.RefreshActiveProjectedReadModel(); err != nil {
		return err
	}
	filmcache.ClearTVBoxConfigCache()
	filmcache.ClearTVBoxListCache()
	return nil
}

func (s *FilmService) SaveClassTree(nodes []*model.CategoryTree) error {
	cleanNodes := sanitizeCategoryTreeNodes(nodes)
	if len(cleanNodes) == 0 {
		return errors.New("分类结构不能为空")
	}
	if err := repository.SaveCategoryTreeStructure(cleanNodes); err != nil {
		return err
	}
	if err := filmsnapshot.RefreshActiveProjectedReadModel(); err != nil {
		return err
	}
	filmcache.ClearTVBoxConfigCache()
	filmcache.ClearTVBoxListCache()
	return nil
}
