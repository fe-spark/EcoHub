package service

import (
	"errors"
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

// GetFilmPage 获取影片检索信息分页数据（D3：读请求忽略 sourceId，固定当前主站）。
func (s *FilmService) GetFilmPage(vo *model.SearchVo) ([]model.FilmIndex, error) {
	if vo == nil {
		return nil, errors.New("search params cannot be nil")
	}
	if primary := repository.GetActiveCollectSource(); primary != nil {
		vo.SourceId = primary.Id
	} else {
		// 无主站：空列表，不回退其他站点
		return []model.FilmIndex{}, nil
	}
	list, err := filmsnapshot.GetSearchPageReadModel(*vo)
	if err != nil {
		if filmsnapshot.IsSearchIndexBuilding() && errors.Is(err, filmsnapshot.ErrSearchIndexMissing) {
			return []model.FilmIndex{}, nil
		}
		return nil, err
	}
	return list, nil
}

// SearchIndexState 返回当前检索索引是否正在构建及最新构建进度
func (s *FilmService) SearchIndexState() (bool, any) {
	return filmsnapshot.IsSearchIndexBuilding(), filmsnapshot.GetLatestSearchIndexProgress()
}

// GetSearchOptions 获取影片检索的筛选项（固定主站，D3/X-12：不再返回站点列表与默认站点）。
func (s *FilmService) GetSearchOptions() (map[string]any, error) {
	options := make(map[string]any)
	primary := repository.GetActiveCollectSource()
	primaryID := ""
	if primary != nil {
		primaryID = primary.Id
	}
	options["primarySourceId"] = primaryID
	options["class"] = []any{}
	options["categoryTree"] = []*model.CategoryTree{}
	if primaryID == "" {
		return options, nil
	}
	tree, err := repository.GetActiveCategoryTree(primaryID)
	if err != nil {
		return nil, err
	}
	tree.Name = "全部分类"
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
	// D3：保存影片固定当前主站；携带其他站点时拒绝
	primary := repository.GetActiveCollectSource()
	if primary == nil {
		return errors.New("暂无主站，无法保存影片")
	}
	if sourceId != "" && sourceId != primary.Id {
		return ErrPrimaryMismatch
	}
	sourceId = primary.Id

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

// validatePrimaryFilmIDs 校验写操作的影片 ID 均属于当前主站（D3）。
func validatePrimaryFilmIDs(ids []int64, expectedSourceID string) (string, error) {
	primary := repository.GetActiveCollectSource()
	if primary == nil {
		return "", errors.New("暂无主站")
	}
	expectedSourceID = strings.TrimSpace(expectedSourceID)
	if expectedSourceID != "" && expectedSourceID != primary.Id {
		return "", ErrPrimaryMismatch
	}
	owned, err := filmrepo.FilmIDsBelongToSource(ids, primary.Id)
	if err != nil {
		return "", err
	}
	if !owned {
		return "", ErrPrimaryMismatch
	}
	return primary.Id, nil
}

// DeletePrimaryFilms 删除主站影片。校验与删除串行化，避免切主竞态。
func (s *FilmService) DeletePrimaryFilms(ids []int64, sourceID string) error {
	return withPrimaryMutation(func(_ bool) error {
		if _, err := validatePrimaryFilmIDs(ids, sourceID); err != nil {
			return err
		}
		return filmrepo.DelFilmSearchBatch(ids)
	})
}

// RefreshPrimaryFilms 校验影片属于当前主站后启动采集更新。
func (s *FilmService) RefreshPrimaryFilms(ids []int64, sourceID string) error {
	return withPrimaryMutation(func(_ bool) error {
		if _, err := validatePrimaryFilmIDs(ids, sourceID); err != nil {
			return err
		}
		SpiderSvc.CollectFilms(ids)
		return nil
	})
}

// DelFilm 删除分类影片
func (s *FilmService) DelFilm(id int64) error {
	return s.DelFilms([]int64{id})
}

// GetFilmClassTree 获取影片分类信息
func (s *FilmService) GetFilmClassTree() model.CategoryTree {
	return repository.GetCategoryTree()
}

// ClassTreeScope 分类管理的树和站点选项。
type ClassTreeScope struct {
	Tree            model.CategoryTree
	Sources         []model.FilmSource
	DefaultSourceId string
	CurrentSourceId string
}

// GetPrimaryClassTree 主站分类展示树（D2：前台导航来源，只读主站）。
// Sources 保留给分类规则页做采集侧站点选择（D2），树本身与 sourceId 无关。
func (s *FilmService) GetPrimaryClassTree() (ClassTreeScope, error) {
	sources, err := repository.GetCollectSourceList()
	if err != nil {
		return ClassTreeScope{}, err
	}
	primaryID := ""
	if primary := repository.GetActiveCollectSource(); primary != nil {
		primaryID = primary.Id
	}
	tree := model.CategoryTree{}
	if primaryID != "" {
		tree = repository.GetSourceBoundCategoryTree(primaryID)
	}
	return ClassTreeScope{
		Tree:            tree,
		Sources:         sources,
		DefaultSourceId: primaryID,
		CurrentSourceId: primaryID,
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

// SaveSourceClassTree 保存分类展示树结构（D2：只允许保存主站）。
func (s *FilmService) SaveSourceClassTree(sourceID string, nodes []*model.CategoryTree) error {
	if len(nodes) == 0 {
		return errors.New("分类结构不能为空")
	}
	primary := repository.GetActiveCollectSource()
	if primary == nil {
		return errors.New("暂无主站，无法保存分类结构")
	}
	// D3：写请求携带的站点与主站不一致时拒绝
	if sourceID = strings.TrimSpace(sourceID); sourceID != "" && sourceID != primary.Id {
		return ErrPrimaryMismatch
	}
	if err := repository.SaveSourceCategoryOrder(primary.Id, nodes); err != nil {
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
