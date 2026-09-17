package service

import (
	"encoding/json"
	"errors"
	"log"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	filmcache "server/internal/repository/film/cache"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/repository/film/writer"
	"server/internal/spider/converter"
)

type FilmService struct{}

var FilmSvc = new(FilmService)

// GetFilmPage 获取影片检索信息分页数据
func (s *FilmService) GetFilmPage(vo model.SearchVo) []model.FilmIndex {
	return filmsnapshot.GetSearchPageReadModel(vo)
}

// GetSearchOptions 获取影片检索的select的选项options
func (s *FilmService) GetSearchOptions() map[string]any {
	startedAt := time.Now()
	options := make(map[string]any)
	tree := repository.GetActiveCategoryTree()
	tree.Name = "全部分类"
	options["class"] = converter.ConvertCategoryList(&tree)
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
	log.Printf("[ManageFilmSearch] 筛选选项快照读取 cost=%s", time.Since(startedAt))
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

	// 若更新既有影片且未传入播放资源，保留已有的播放资源和线路（绝不覆盖更新播放资源）
	if fd.Id > 0 && len(detail.PlayList) == 0 {
		var existingDetailRec model.MovieDetailInfo
		if db.Mdb.Where("mid = ?", fd.Id).First(&existingDetailRec).Error == nil && existingDetailRec.Content != "" {
			var oldDetail model.MovieDetail
			if json.Unmarshal([]byte(existingDetailRec.Content), &oldDetail) == nil {
				detail.PlayList = oldDetail.PlayList
				if len(detail.PlayFrom) == 0 {
					detail.PlayFrom = oldDetail.PlayFrom
				}
				if len(detail.DownloadList) == 0 {
					detail.DownloadList = oldDetail.DownloadList
				}
				if detail.DownFrom == "" {
					detail.DownFrom = oldDetail.DownFrom
				}
			}
		}
	}

	if detail.PlayList == nil {
		detail.PlayList = [][]model.MovieUrlInfo{}
	}

	// 手动上传的影片，尝试归属于当前主站 ID，如果没有主站则标记为 "manual"
	sourceId := "manual"
	if master := repository.GetCollectSourceListByGrade(model.MasterCollect); len(master) > 0 {
		sourceId = master[0].Id
	}

	if err := writer.SaveDetail(sourceId, detail); err != nil {
		return err
	}
	return nil
}

// DelFilm 删除分类影片
func (s *FilmService) DelFilm(id int64) error {
	filmIndex := filmrepo.GetFilmIndexById(id)
	if filmIndex == nil || filmIndex.ID == 0 {
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
