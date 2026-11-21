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
	"sync/atomic"
	"time"
)

type FilmService struct{}

var FilmSvc = new(FilmService)

var manualFilmSeq atomic.Int64

func nextManualSourceMid(now time.Time) int64 {
	candidate := -now.UnixNano()
	for {
		prev := manualFilmSeq.Load()
		next := candidate
		if prev != 0 && next >= prev {
			next = prev - 1
		}
		if manualFilmSeq.CompareAndSwap(prev, next) {
			return next
		}
	}
}

// GetFilmPage 获取影片检索信息分页数据（D3：读请求忽略 sourceId，固定当前主站）。
func (s *FilmService) GetFilmPage(vo *model.SearchVo) ([]model.FilmIndex, error) {
	if vo == nil {
		return nil, errors.New("search params cannot be nil")
	}
	if primary := repository.CurrentPrimarySource(); primary != nil {
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
	primary := repository.CurrentPrimarySource()
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
	isUpdate := fd.Id > 0
	if !isUpdate {
		// 手动新建分配单调唯一负数作为源站临时映射标识，避免与 source_mid=0 或源站正数 vod_id 碰撞
		fd.Id = nextManualSourceMid(now)
	}
	detail, err := converter.CovertFilmDetailVo(fd)
	if err != nil {
		return errors.New("影片参数格式异常或缺少关键信息")
	}

	var savedMid int64
	err = withPrimaryMutation(func(_ bool) error {
		sourceId := strings.TrimSpace(fd.SourceId)
		// D3：保存影片固定当前主站；携带其他站点时拒绝
		primary := repository.CurrentPrimarySource()
		if primary == nil {
			return errors.New("暂无主站，无法保存影片")
		}
		if sourceId != "" && sourceId != primary.Id {
			return ErrPrimaryMismatch
		}
		if isUpdate {
			owned, err := filmrepo.FilmIDsBelongToSource([]int64{fd.Id}, primary.Id)
			if err != nil {
				return err
			}
			if !owned {
				return ErrPrimaryMismatch
			}
		}
		mid, err := writer.SaveDetailWithOptions(primary.Id, detail, writer.SaveDetailOptions{PublishSnapshot: false})
		if err != nil {
			return err
		}
		savedMid = mid
		return nil
	})
	if err != nil {
		return err
	}
	if savedMid > 0 {
		_, _, _ = filmsnapshot.UpsertActiveSnapshotsByMids(savedMid)
	}
	return nil
}

// validatePrimaryFilmIDs 校验写操作的影片 ID 均属于当前主站（D3）。
func validatePrimaryFilmIDs(ids []int64, expectedSourceID string) (string, error) {
	primary := repository.CurrentPrimarySource()
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

// DeletePrimaryFilms 删除主站影片。校验与数据库删库在同一互斥锁内完成，避免切主 TOCTOU；快照与缓存清理在锁外执行避免重入重建锁。
func (s *FilmService) DeletePrimaryFilms(ids []int64, sourceID string) error {
	var cleanIds, pids, playPeers []int64
	err := withPrimaryMutation(func(_ bool) error {
		if _, err := validatePrimaryFilmIDs(ids, sourceID); err != nil {
			return err
		}
		playPeers = filmsnapshot.ExpandPlayInfoPeerMids(ids)
		var delErr error
		cleanIds, pids, delErr = filmrepo.DelFilmSearchBatchDB(ids)
		return delErr
	})
	if err != nil {
		return err
	}
	filmrepo.InvalidateFilmSearchBatchCaches(cleanIds, pids, playPeers...)
	return nil
}

// RefreshPrimaryFilms 校验影片属于当前主站后异步启动采集更新。校验与派发在同一互斥锁内完成，消除切主 TOCTOU。
func (s *FilmService) RefreshPrimaryFilms(ids []int64, sourceID string) error {
	return withPrimaryMutation(func(_ bool) error {
		if _, err := validatePrimaryFilmIDs(ids, sourceID); err != nil {
			return err
		}
		SpiderSvc.CollectFilms(ids)
		return nil
	})
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
	PrimarySourceId string
}

// GetPrimaryClassTree 获取当前主站的分类展示树及采集站选项。
func (s *FilmService) GetPrimaryClassTree() (ClassTreeScope, error) {
	return s.GetClassTreeScope("")
}

// GetClassTreeScope 按采集站获取分类展示树；sourceID 为空时默认使用当前主站。
func (s *FilmService) GetClassTreeScope(sourceID string) (ClassTreeScope, error) {
	sources, err := repository.GetCollectSourceList()
	if err != nil {
		return ClassTreeScope{}, err
	}
	primaryID := ""
	if primary := repository.CurrentPrimarySource(); primary != nil {
		primaryID = primary.Id
	}
	targetID := strings.TrimSpace(sourceID)
	if targetID == "" {
		targetID = primaryID
	} else if !collectSourceExists(sources, targetID) {
		return ClassTreeScope{}, errors.New("采集站不存在")
	}
	tree := model.CategoryTree{}
	if targetID != "" {
		tree = repository.GetSourceBoundCategoryTree(targetID)
	}
	return ClassTreeScope{
		Tree:            tree,
		Sources:         sources,
		DefaultSourceId: primaryID,
		CurrentSourceId: targetID,
		PrimarySourceId: primaryID,
	}, nil
}

func collectSourceExists(sources []model.FilmSource, sourceID string) bool {
	for _, src := range sources {
		if strings.TrimSpace(src.Id) == sourceID {
			return true
		}
	}
	return false
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

// SaveSourceClassTree 保存指定采集站的分类展示树结构；sourceID 为空时默认当前主站。
func (s *FilmService) SaveSourceClassTree(sourceID string, nodes []*model.CategoryTree) error {
	if len(nodes) == 0 {
		return errors.New("分类结构不能为空")
	}
	targetID := strings.TrimSpace(sourceID)
	if targetID == "" {
		primary := repository.CurrentPrimarySource()
		if primary == nil {
			return errors.New("暂无主站，无法保存分类结构")
		}
		targetID = primary.Id
	} else {
		sources, err := repository.GetCollectSourceList()
		if err != nil {
			return err
		}
		if !collectSourceExists(sources, targetID) {
			return errors.New("采集站不存在")
		}
	}
	if err := repository.SaveSourceCategoryOrder(targetID, nodes); err != nil {
		return err
	}
	if err := filmsnapshot.RefreshActiveProjectedReadModel(); err != nil {
		return err
	}
	filmsnapshot.ClearDynamicPlayCaches()
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
	filmrepo.RefreshInventoryStatsAsync()
	return nil
}
