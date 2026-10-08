package service

import (
	"errors"
	"log"
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
func (s *FilmService) GetFilmPage(vo model.SearchVo) []model.FilmIndex {
	if strings.TrimSpace(vo.SourceId) == "" {
		if activeSrc := repository.GetActiveCollectSource(); activeSrc != nil {
			vo.SourceId = activeSrc.Id
		}
	}
	return filmsnapshot.GetSearchPageReadModel(vo)
}

// GetSearchOptions 获取影片检索的select的选项options
func (s *FilmService) GetSearchOptions(sourceIdOpt ...string) map[string]any {
	startedAt := time.Now()
	options := make(map[string]any)

	var sourceId string
	if len(sourceIdOpt) > 0 {
		sourceId = strings.TrimSpace(sourceIdOpt[0])
	}

	sources := repository.GetEnabledCollectSourceList()
	defaultSourceId := ""
	if len(sources) > 0 {
		defaultSourceId = sources[0].Id
	}
	if sourceId == "" {
		sourceId = defaultSourceId
	}

	tree := repository.GetActiveCategoryTree(sourceId)
	tree.Name = "全部分类"
	options["sources"] = sources
	options["defaultSourceId"] = defaultSourceId
	options["currentSourceId"] = sourceId
	options["class"] = converter.ConvertCategoryList(&tree)
	options["categoryTree"] = tree.Children
	options["year"] = make([]map[string]string, 0)
	tagGroup := filmsnapshot.GetAdminFilterOptionSnapshots()
	if tree.Children != nil {
		for _, t := range tree.Children {
			option := tagGroup[t.Id]
			if len(option) == 0 {
				continue
			}
			if v, ok := options["year"].([]map[string]string); !ok || len(v) == 0 {
				options["year"] = option["Year"]
			}
		}
	}
	options["tags"] = tagGroup
	log.Printf("[ManageFilmSearch] 筛选选项快照读取 sourceId=%s cost=%s", sourceId, time.Since(startedAt))
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
	detail, err := converter.CovertFilmDetailVo(fd)
	if err != nil {
		return errors.New("影片参数格式异常或缺少关键信息")
	}

	if detail.PlayList == nil {
		detail.PlayList = [][]model.MovieUrlInfo{}
	}

	// 手动上传的影片，尝试归属于首个启用的采集站 ID，如果没有则标记为 "manual"
	sourceId := "manual"
	if sources := repository.GetEnabledCollectSourceList(); len(sources) > 0 {
		sourceId = sources[0].Id
	}

	if err := writer.SaveDetail(sourceId, detail); err != nil {
		return err
	}
	return nil
}

// DelFilm 删除分类影片
func (s *FilmService) DelFilm(id int64) error {
	filmIndex := filmrepo.GetFilmIndexById(id)
	if filmIndex == nil || filmIndex.Mid == 0 {
		return errors.New("影片信息不存在")
	}
	if err := filmrepo.DelFilmSearch(id); err != nil {
		return err
	}
	return nil
}

// GetFilmClassTree 获取影片分类信息
func (s *FilmService) GetFilmClassTree() model.CategoryTree {
	return repository.GetCategoryTree()
}

// GetFilmClassById 通过ID获取影片分类信息
func (s *FilmService) GetFilmClassById(id int64) *model.CategoryTree {
	return repository.GetCategoryTreeByID(id)
}

// UpdateClass 更新分类状态
func (s *FilmService) UpdateClass(class model.CategoryTree) error {
	updates := map[string]any{"show": class.Show}

	oldClass := s.GetFilmClassById(class.Id)
	if oldClass == nil {
		return errors.New("需要更新的分类信息不存在")
	}

	if err := repository.UpdateCategoryStatus(class.Id, updates); err != nil {
		return err
	}
	if err := filmsnapshot.RefreshActiveProjectedReadModel(); err != nil {
		return err
	}
	filmcache.ClearTVBoxConfigCache()
	filmcache.ClearTVBoxListCache()
	return nil
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
			Children: sanitizeCategoryTreeNodes(node.Children),
		})
	}
	return res
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
