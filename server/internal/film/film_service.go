package film

import (
	"errors"
	"fmt"
	"server/internal/film/cache"
	"server/internal/film/catalog"
	"server/internal/film/converter"
	"server/internal/film/snapshot"
	"server/internal/film/writer"
	"server/internal/infra/db"
	"server/internal/mediaproxy"
	"server/internal/model"
	"server/internal/sourcecatalog"
	"strings"
	"sync/atomic"
	"time"
)

type FilmService struct{}

var FilmSvc = new(FilmService)

var (
	// OnRefreshFilms 触发指定影片重采，由装配层注入 collect 实现。
	OnRefreshFilms func(ids []int64)
	// IsFilmCollectingFn 判断当前是否有采集任务进行中，由装配层注入。
	IsFilmCollectingFn func() bool
)

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
	if primary := sourcecatalog.CurrentPrimarySource(); primary != nil {
		vo.SourceId = primary.Id
	} else {
		// 无主站：空列表，不回退其他站点
		return []model.FilmIndex{}, nil
	}
	list, err := snapshot.GetSearchPageReadModel(*vo)
	if err != nil {
		if snapshot.IsSearchIndexBuilding() && errors.Is(err, snapshot.ErrSearchIndexMissing) {
			return []model.FilmIndex{}, nil
		}
		return nil, err
	}
	return list, nil
}

// SearchIndexState 返回当前检索索引是否正在构建及最新构建进度
func (s *FilmService) SearchIndexState() (bool, any) {
	return snapshot.IsSearchIndexBuilding(), snapshot.GetLatestSearchIndexProgress()
}

// GetSearchOptions 获取影片检索的筛选项（固定主站，D3/X-12：不再返回站点列表与默认站点）。
func (s *FilmService) GetSearchOptions() (map[string]any, error) {
	options := make(map[string]any)
	primary := sourcecatalog.CurrentPrimarySource()
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
	tree, err := catalog.GetActiveCategoryTree(primaryID)
	if err != nil {
		return nil, err
	}
	tree.Name = "全部分类"
	options["class"] = converter.ConvertCategoryList(&tree)
	options["categoryTree"] = tree.Children
	return options, nil
}

// GetStoredFilmDetail 后台编辑影片专用详情接口：绕过前台分类隐藏校验，直读片库正文、封面与全部播放线路。
func (s *FilmService) GetStoredFilmDetail(id int) (model.MovieDetailVo, bool, error) {
	if id <= 0 {
		return model.MovieDetailVo{}, false, errors.New("影片信息无效")
	}
	row, err := FindFilmIndex(int64(id))
	if err != nil || row.Mid <= 0 {
		return model.MovieDetailVo{}, false, errors.New("暂无影片信息")
	}
	movieDetail, err := s.loadStoredMovieDetail(row)
	if err != nil {
		return model.MovieDetailVo{}, false, err
	}
	playList, downloadList, err := LoadPlayAndDownloadSourcesByMid(row.Mid)
	if err != nil {
		return model.MovieDetailVo{}, false, err
	}
	movieDetail.DownloadList = downloadList
	primaryID := ""
	if primary := sourcecatalog.CurrentPrimarySource(); primary != nil {
		primaryID = primary.Id
	}
	isManualFilm := writer.IsManualSourceFilm(db.Mdb, row.Mid, primaryID, row.FirstSourceId)
	movieDetail.SourceId = row.FirstSourceId
	// 仅当前主站（或未配置主站）及自建影片可回传原始 SourceTypeId，避免跨站聚合影片把来源站 type_id 误当主站分类。
	if primaryID == "" || strings.TrimSpace(row.FirstSourceId) == primaryID || isManualFilm {
		if id := parseCategoryKeyTypeID(row.CategoryKey); id > 0 {
			movieDetail.RawCid = id
		}
		if id := parseCategoryKeyTypeID(row.RootCategoryKey); id > 0 {
			movieDetail.RawPid = id
		}
	}
	res := model.MovieDetailVo{
		MovieDetail:     movieDetail,
		LocalUpdateTime: row.UpdateStamp,
		UpdateReason:    strings.TrimSpace(row.UpdateReason),
		List:            playList,
	}
	res.PlayList, res.PlayFrom = PlayGroupsFromPlayLinkVos(playList)
	return mediaproxy.WrapMovieDetailVo(res), isManualFilm, nil
}

func (s *FilmService) loadStoredMovieDetail(row model.FilmIndex) (model.MovieDetail, error) {
	detail := model.MovieDetail{
		Id:                 row.Mid,
		Cid:                row.Cid,
		Pid:                row.Pid,
		Name:               row.Name,
		Picture:            row.Picture,
		PictureSlide:       row.PictureSlide,
		CustomPicture:      row.CustomPicture,
		CustomPictureSlide: row.CustomPictureSlide,
		IsCustomPicture:    row.IsCustomPicture,
		MovieDescriptor: model.MovieDescriptor{
			SubTitle:    row.SubTitle,
			CName:       row.CName,
			ClassTag:    row.ClassTag,
			Actor:       row.Actor,
			Director:    row.Director,
			Writer:      row.Writer,
			Blurb:       row.Blurb,
			Remarks:     row.Remarks,
			ReleaseDate: row.ReleaseDate,
			Area:        row.Area,
			Language:    row.Language,
			Initial:     row.Initial,
			State:       row.State,
			DbId:        row.DbId,
			Hits:        row.Hits,
			Content:     row.Content,
		},
	}
	if row.Year > 0 {
		detail.Year = fmt.Sprintf("%d", row.Year)
	}
	if row.Score > 0 {
		detail.DbScore = fmt.Sprintf("%g", row.Score)
	}
	return detail, nil
}

// SaveFilmDetail 自定义上传保存影片信息
func (s *FilmService) SaveFilmDetail(fd model.FilmDetailVo) error {
	if fd.Cid >= catalog.SyntheticRootTypeOffset {
		return errors.New("不能直接选择合成主分类，请选择其下的子分类")
	}
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
	err = snapshot.WithPrimaryMutation(func(_ bool) error {
		sourceId := strings.TrimSpace(fd.SourceId)
		// D3：保存影片固定当前主站；携带其他站点时拒绝
		primary := sourcecatalog.CurrentPrimarySource()
		if primary == nil {
			return errors.New("暂无主站，无法保存影片")
		}
		if sourceId != "" && sourceId != primary.Id {
			return sourcecatalog.ErrPrimaryMismatch
		}
		if isUpdate {
			owned, err := FilmIDsBelongToSource([]int64{fd.Id}, primary.Id)
			if err != nil {
				return err
			}
			if !owned {
				return sourcecatalog.ErrPrimaryMismatch
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
		_, _, _ = snapshot.UpsertActiveSnapshotsByMids(savedMid)
	}
	return nil
}

// validatePrimaryFilmIDs 校验写操作的影片 ID 均属于当前主站（D3）。
func validatePrimaryFilmIDs(ids []int64, expectedSourceID string) (string, error) {
	primary := sourcecatalog.CurrentPrimarySource()
	if primary == nil {
		return "", errors.New("暂无主站")
	}
	expectedSourceID = strings.TrimSpace(expectedSourceID)
	if expectedSourceID != "" && expectedSourceID != primary.Id {
		return "", sourcecatalog.ErrPrimaryMismatch
	}
	owned, err := FilmIDsBelongToSource(ids, primary.Id)
	if err != nil {
		return "", err
	}
	if !owned {
		return "", sourcecatalog.ErrPrimaryMismatch
	}
	return primary.Id, nil
}

// DeletePrimaryFilms 删除主站影片。校验与数据库删库在同一互斥锁内完成，避免切主 TOCTOU；快照与缓存清理在锁外执行避免重入重建锁。
func (s *FilmService) DeletePrimaryFilms(ids []int64, sourceID string) error {
	var cleanIds, pids, playPeers []int64
	err := snapshot.WithPrimaryMutation(func(_ bool) error {
		if _, err := validatePrimaryFilmIDs(ids, sourceID); err != nil {
			return err
		}
		playPeers = snapshot.ExpandPlayInfoPeerMids(ids)
		var delErr error
		cleanIds, pids, delErr = DelFilmSearchBatchDB(ids)
		return delErr
	})
	if err != nil {
		return err
	}
	InvalidateFilmSearchBatchCaches(cleanIds, pids, playPeers...)
	return nil
}

// RefreshPrimaryFilms 校验影片属于当前主站后异步启动采集更新。校验与派发在同一互斥锁内完成，消除切主 TOCTOU。
func (s *FilmService) RefreshPrimaryFilms(ids []int64, sourceID string) error {
	return snapshot.WithPrimaryMutation(func(_ bool) error {
		if _, err := validatePrimaryFilmIDs(ids, sourceID); err != nil {
			return err
		}
		if OnRefreshFilms == nil {
			return errors.New("采集刷新钩子未初始化")
		}
		OnRefreshFilms(ids)
		return nil
	})
}

// GetFilmClassTree 获取影片分类信息
func (s *FilmService) GetFilmClassTree() model.CategoryTree {
	return catalog.GetCategoryTree()
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
	sources, err := sourcecatalog.GetCollectSourceListDB(db.Mdb)
	if err != nil {
		return ClassTreeScope{}, err
	}
	primaryID := ""
	if primary := sourcecatalog.CurrentPrimarySource(); primary != nil {
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
		var (
			keyCounts map[string]int64
			ok        bool
		)
		// 采集中或待检索封口期间内存快照尚未纳入新入库影片，主站同样实时回源统计。
		if IsFilmCollectingFn == nil || !IsFilmCollectingFn() {
			keyCounts, ok = snapshot.CountFilmsByCategoryKeys(targetID)
		}
		if !ok {
			dbCounts, err := catalog.SourceCategoryFilmCountsFromDB(targetID)
			if err != nil {
				return ClassTreeScope{}, err
			}
			keyCounts = dbCounts
		}
		tree = catalog.GetSourceBoundCategoryTreeWithCounts(targetID, keyCounts)
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
	return catalog.GetCategoryTreeByID(id)
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
		primary := sourcecatalog.CurrentPrimarySource()
		if primary == nil {
			return errors.New("暂无主站，无法保存分类结构")
		}
		targetID = primary.Id
	} else {
		sources, err := sourcecatalog.GetCollectSourceListDB(db.Mdb)
		if err != nil {
			return err
		}
		if !collectSourceExists(sources, targetID) {
			return errors.New("采集站不存在")
		}
	}
	if err := catalog.SaveSourceCategoryOrder(targetID, nodes); err != nil {
		return err
	}
	if err := snapshot.RefreshActiveProjectedReadModel(); err != nil {
		return err
	}
	snapshot.ClearDynamicPlayCaches()
	cache.ClearTVBoxConfigCache()
	cache.ClearTVBoxListCache()
	return nil
}

func (s *FilmService) SaveClassTree(nodes []*model.CategoryTree) error {
	cleanNodes := sanitizeCategoryTreeNodes(nodes)
	if len(cleanNodes) == 0 {
		return errors.New("分类结构不能为空")
	}
	if err := catalog.SaveCategoryTreeStructure(cleanNodes); err != nil {
		return err
	}
	if err := snapshot.RefreshActiveProjectedReadModel(); err != nil {
		return err
	}
	cache.ClearTVBoxConfigCache()
	cache.ClearTVBoxListCache()
	RefreshInventoryStatsAsync()
	return nil
}
