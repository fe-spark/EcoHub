package service

import (
	"errors"
	"log"

	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/spider"
)

type SpiderService struct{}

var SpiderSvc = new(SpiderService)

func clearCategorySyncRedisCaches() {
	repository.ClearCategoryCache()
	filmrepo.ClearAllSearchTagsCache()
	filmrepo.ClearTVBoxListCache()
	repository.ClearIndexPageCache()
}

func finalizeCategorySync() {
	repository.RefreshCategoryCache()
	clearCategorySyncRedisCaches()
}

// StartCollect 执行对指定站点的采集任务
func (s *SpiderService) StartCollect(id string, h int) error {
	fs := repository.FindCollectSourceById(id)
	if fs == nil {
		return errors.New("采集任务开启失败，采集站信息不存在")
	}
	if !fs.State {
		return errors.New("采集任务开启失败，该采集站已被禁用，请先启用后再采集")
	}
	if err := spider.PrepareSingleCollectStart(*fs); err != nil {
		return err
	}
	go func() {
		err := spider.HandlePreparedCollect(id, h)
		if err != nil {
			log.Printf("[SpiderService] 资源站[%s]采集任务执行失败: %s", id, err)
		}
	}()
	return nil
}

// BatchCollect 批量采集：同步标记 starting(0%) 后异步执行，列表可立即显示进度。
func (s *SpiderService) BatchCollect(time int, ids []string) error {
	sources, err := spider.PrepareBatchCollectStart(ids)
	if err != nil {
		return err
	}
	go spider.BatchCollectPrepared(model.NotifyTriggerManual, time, sources)
	return nil
}

// AutoCollect 自动采集
func (s *SpiderService) AutoCollect(time int) {
	go spider.AutoCollect(time)
}

// ClearFilms 重置站点业务数据：清空影视/采集派生数据。
// 清空完成后自动同步主站分类，避免后续增量采集因分类映射缺失导致影片“已入库但列表不可见”。
// 注意：不重置任何账号与密码，也不恢复任何配置类数据（网站配置、轮播、映射规则、采集源、定时任务均保留）。
// 全程按关键节点上报真实进度，供前端轮询展示。
func (s *SpiderService) ClearFilms() (retErr error) {
	filmrepo.StartResetProgress()
	defer func() {
		if retErr != nil {
			filmrepo.FinishResetProgress(retErr)
		}
	}()
	if err := spider.ClearSpider(); err != nil {
		return err
	}
	// 分类与影视库存一并清空后必须立即重建，否则主站采集写入的 pid/cid 为 0，
	// 读模型会按不可见过滤，出现日志入库成功但影片列表为空。
	filmrepo.ReportResetProgress(96, "正在同步主站分类")
	if err := s.SyncMasterCategoryTree(); err != nil {
		log.Printf("[SpiderService] 重置后主站分类同步失败: %v", err)
		return err
	}
	filmrepo.ReportResetProgress(100, "重置完成，主站分类已同步")
	filmrepo.FinishResetProgress(nil)
	return nil
}

// ResetProgress 返回数据重置实时进度
func (s *SpiderService) ResetProgress() filmrepo.ResetProgress {
	return filmrepo.GetResetProgress()
}

// ResetImpactStats 返回数据重置影响面统计（将清空的数据量）
func (s *SpiderService) ResetImpactStats() filmrepo.ResetImpactStats {
	return filmrepo.GetResetImpactStats()
}

// SyncCollect 同步主站单片采集
func (s *SpiderService) SyncCollect(ids string) {
	go spider.CollectSingleFilm(ids)
}

// FilmClassCollect 重置为主站原始分类并清空业务属性
func (s *SpiderService) FilmClassCollect() error {
	l := repository.GetCollectSourceListByGrade(model.MasterCollect)
	if l == nil {
		return errors.New("未获取到主采集站信息")
	}
	for _, fs := range l {
		if fs.State {
			if err := spider.ResetCategory(&fs); err != nil {
				return err
			}
			finalizeCategorySync()
			return nil
		}
	}
	return errors.New("未获取到已启用的主采集站信息")
}

func (s *SpiderService) SyncMasterCategoryTree() error {
	l := repository.GetCollectSourceListByGrade(model.MasterCollect)
	if len(l) == 0 {
		return errors.New("未获取到主采集站信息")
	}
	for _, fs := range l {
		if !fs.State {
			continue
		}
		log.Printf("[SpiderService] 启动同步主站分类: name=%s id=%s uri=%s", fs.Name, fs.Id, fs.Uri)
		if err := spider.CollectCategory(&fs); err != nil {
			log.Printf("[SpiderService] 主站分类同步失败: name=%s id=%s err=%v", fs.Name, fs.Id, err)
			return err
		}
		finalizeCategorySync()
		log.Printf("[SpiderService] 主站分类同步完成: name=%s id=%s", fs.Name, fs.Id)
		return nil
	}
	return errors.New("未获取到已启用的主采集站信息")
}

// StopAllTasks 强制停止所有採集任務
func (s *SpiderService) StopAllTasks() {
	spider.StopAllTasks()
}

// StopTask 停止指定采集站的任务（仅停止任务，不影响采集站启用状态）。
func (s *SpiderService) StopTask(id string) error {
	fs := repository.FindCollectSourceById(id)
	if fs == nil {
		return errors.New("采集站信息不存在")
	}
	spider.StopTask(id)
	return nil
}
