package service

import (
	"errors"
	"log"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/spider"
)

type SpiderService struct{}

var SpiderSvc = new(SpiderService)

// StartCollect 执行对指定站点的采集任务
func (s *SpiderService) StartCollect(id string, h int) error {
	fs := repository.FindCollectSourceById(id)
	if fs == nil {
		return errors.New("采集任务开启失败，采集站信息不存在")
	}
	if !fs.State {
		return errors.New("采集任务开启失败，该采集站已被禁用，请先启用后再采集")
	}
	go func() {
		err := spider.HandleCollect(id, h)
		if err != nil {
			log.Printf("[SpiderService] 资源站[%s]采集任务执行失败: %s", id, err)
		}
	}()
	return nil
}

// BatchCollect 批量采集
func (s *SpiderService) BatchCollect(time int, ids []string) error {
	go spider.BatchCollect(time, ids...)
	return nil
}

// AutoCollect 自动采集
func (s *SpiderService) AutoCollect(time int) {
	go spider.AutoCollect(time)
}

// ClearFilms 删除采集的数据信息
func (s *SpiderService) ClearFilms() {
	go spider.ClearSpider()
}

// SyncCollect 同步主站单片采集
func (s *SpiderService) SyncCollect(ids string) {
	go spider.CollectSingleFilm(ids)
}

// FilmClassCollect 影视分类采集, 直接覆盖当前分类数据
func (s *SpiderService) FilmClassCollect() error {
	l := repository.GetCollectSourceListByGrade(model.MasterCollect)
	if l == nil {
		return errors.New("未获取到主采集站信息")
	}
	for _, fs := range l {
		if fs.State {
			go spider.CollectCategory(&fs)
			return nil
		}
	}
	return errors.New("未获取到已启用的主采集站信息")
}

// StopAllTasks 强制停止所有採集任務
func (s *SpiderService) StopAllTasks() {
	spider.StopAllTasks()
}
