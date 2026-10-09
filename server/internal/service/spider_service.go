package service

import (
	"errors"
	"fmt"
	"log"
	"strings"

	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	filmcache "server/internal/repository/film/cache"
	"server/internal/spider"
)

type SpiderService struct{}

var SpiderSvc = new(SpiderService)

func clearCategorySyncRedisCaches() {
	repository.ClearCategoryCache()
	filmcache.ClearAllSearchTagsCache()
	filmcache.ClearTVBoxListCache()
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
	if !repository.SourceHasCategoryMapping(fs.Id) {
		return fmt.Errorf("采集站 %s 还没有分类，请重新保存后再采集", fs.Name)
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
// 清空会连分类一起清掉，随后按每个采集站重新拉取分类副本。
// 注意：不重置任何账号与密码，也不恢复任何配置类数据（网站配置、映射规则、采集源、定时任务均保留；轮播图因强绑定影片 mid 一并清空）。
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
	// 分类与影视库存一并清空后，按每个采集站重新拉取，采集才能写上 pid/cid。
	filmrepo.ReportResetProgress(96, "正在同步各采集站分类")
	if err := s.SyncAllSourceCategories(); err != nil {
		log.Printf("[SpiderService] 重置后采集站分类同步失败: %v", err)
		return err
	}
	filmrepo.ReportResetProgress(100, "重置完成，各采集站分类已同步")
	filmrepo.FinishResetProgress(nil)
	return nil
}

// ResetProgress 返回数据重置实时进度
func (s *SpiderService) ResetProgress() filmrepo.ResetProgress {
	return filmrepo.GetResetProgress()
}

// InventoryStats 返回工作台片库规模（整库合计，并按采集站拆开）。
func (s *SpiderService) InventoryStats() filmrepo.InventoryStats {
	return filmrepo.GetInventoryStats()
}

// SyncCollect 同步主站单片采集
func (s *SpiderService) SyncCollect(ids string) {
	go spider.CollectSingleFilm(ids)
}

// FilmClassCollect 重新拉取每个采集站的分类副本。已调整的显示和排序保留。
func (s *SpiderService) FilmClassCollect() error {
	return s.SyncAllSourceCategories()
}

// SyncSourceCategories 拉取一个采集站的分类，并写入该站自己的副本。
func (s *SpiderService) SyncSourceCategories(id string) error {
	id = strings.TrimSpace(id)
	source := repository.FindCollectSourceById(id)
	if source == nil {
		return errors.New("采集站信息不存在")
	}
	log.Printf("[SpiderService] 拉取采集站分类: name=%s id=%s uri=%s", source.Name, source.Id, source.Uri)
	if err := spider.CollectCategory(source); err != nil {
		return fmt.Errorf("获取采集站分类失败: %w", err)
	}
	finalizeCategorySync()
	return nil
}

// SyncAllSourceCategories 按现有采集站逐个拉取分类。某一站失败不影响其他站，最后返回首个错误。
func (s *SpiderService) SyncAllSourceCategories() error {
	sources := repository.GetCollectSourceList()
	if len(sources) == 0 {
		return errors.New("没有采集站，不能同步分类")
	}
	var first error
	failed := 0
	for i := range sources {
		source := sources[i]
		if err := spider.CollectCategory(&source); err != nil {
			log.Printf("[SpiderService] 采集站分类同步失败: name=%s id=%s err=%v", source.Name, source.Id, err)
			failed++
			if first == nil {
				first = fmt.Errorf("%s: %w", source.Name, err)
			}
			continue
		}
		log.Printf("[SpiderService] 采集站分类已保存: name=%s id=%s", source.Name, source.Id)
	}
	finalizeCategorySync()
	if failed == 0 {
		return nil
	}
	if failed == len(sources) {
		return first
	}
	return fmt.Errorf("有 %d 个采集站分类获取失败，首个错误: %w", failed, first)
}

// SyncMissingSourceCategories 只给还没有分类副本的采集站补拉一次。已有副本的站不覆盖。
func (s *SpiderService) SyncMissingSourceCategories() {
	sources := repository.GetCollectSourceList()
	synced := 0
	for i := range sources {
		source := sources[i]
		if repository.SourceHasCategoryMapping(source.Id) {
			continue
		}
		log.Printf("[SpiderService] 采集站还没有分类副本，补拉: name=%s id=%s", source.Name, source.Id)
		if err := spider.CollectCategory(&source); err != nil {
			log.Printf("[SpiderService] 补拉采集站分类失败: name=%s id=%s err=%v", source.Name, source.Id, err)
			continue
		}
		synced++
	}
	if synced > 0 {
		finalizeCategorySync()
	}
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
