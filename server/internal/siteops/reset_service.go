package siteops

import (
	"context"
	"errors"
	"sync/atomic"

	"server/internal/access"
	"server/internal/collect"
	"server/internal/collectctl"
	"server/internal/infra/syslog"
)

type ResetService struct{}

var ResetSvc = new(ResetService)

var (
	// resetSiteDataRunning 防止并发重复触发站点数据重置。
	resetSiteDataRunning atomic.Bool
	resetSiteLifecycle   atomic.Bool

	// 站点重置跨模块钩子，由装配层注入。
	OnResetRecoverAccess  func()
	OnResetRecoverCrontab func()
	OnResetReinitSite     func() error
)

func init() {
	collect.RegisterSiteResetChecker(func() bool {
		return resetSiteDataRunning.Load() || resetSiteLifecycle.Load()
	})
}

// triggerResetCategorySync 数据重置后异步回补默认站分类，测试可替换避免访问外网。
var triggerResetCategorySync = func() {
	go collect.SpiderSvc.SyncMissingSourceCategories()
}

func GetTriggerResetCategorySync() func()   { return triggerResetCategorySync }
func SetTriggerResetCategorySync(fn func()) { triggerResetCategorySync = fn }

// ClearFilms 重置站点数据：清空业务与配置数据表（保留账号表 user 与迁移版本表 schema_migrations），
// 同步清空图库与全部缓存，并重新写入初始化数据与启动默认定时任务。
// 全程按关键节点上报真实进度，由 SSE 推给页面。
func (s *ResetService) ClearFilms() (retErr error) {
	if OnResetRecoverAccess == nil || OnResetRecoverCrontab == nil || OnResetReinitSite == nil {
		return errors.New("站点重置钩子未初始化")
	}
	resetSiteLifecycle.Store(true)
	defer resetSiteLifecycle.Store(false)

	StartResetProgress()
	clearedDB := false
	defer func() {
		if retErr != nil {
			// 清库前置阶段失败或半途失败时尽力恢复访问统计开关与定时任务，避免系统停摆
			if OnResetRecoverAccess != nil {
				OnResetRecoverAccess()
			}
			if clearedDB && OnResetRecoverCrontab != nil {
				OnResetRecoverCrontab()
			}
			FinishResetProgress(retErr)
		}
	}()

	ReportResetProgress(2, "正在停止后台任务")
	access.SetEnabled(false)
	collect.StopAllCollect("数据重置，停止采集")
	collectctl.CollectSvc.CancelDelFilmSources(nil)
	collectctl.WaitAllAsyncWork()

	if err := collect.PrepareAndRunReset(ReportResetProgress, FilmZero); err != nil {
		return err
	}
	clearedDB = true
	if _, err := access.ClearAccessData(0); err != nil {
		return err
	}

	ReportResetProgress(85, "正在写入初始化数据")
	if OnResetReinitSite != nil {
		if err := OnResetReinitSite(); err != nil {
			return err
		}
	}

	ReportResetProgress(95, "正在同步默认分类")
	if triggerResetCategorySync != nil {
		triggerResetCategorySync()
	}

	ReportResetProgress(100, "数据重置完成，已恢复初始状态")
	FinishResetProgress(nil)
	return nil
}

// SiteResetRunning 是否已有数据重置在执行。
func (s *ResetService) SiteResetRunning() bool {
	return resetSiteDataRunning.Load()
}

// StreamSiteReset 执行数据重置，并把每个进度快照交给 onEvent。
// 客户端断开后停止推送，重置本身继续跑完。
func (s *ResetService) StreamSiteReset(ctx context.Context, onEvent func(any) error) error {
	if !resetSiteDataRunning.CompareAndSwap(false, true) {
		return errors.New("站点数据正在重置中，请勿重复操作")
	}
	if ctx == nil {
		ctx = context.Background()
	}
	events := make(chan ResetProgress, 16)
	unsub := WatchResetProgress(func(p ResetProgress) {
		select {
		case events <- p:
		case <-ctx.Done():
		}
	})
	defer unsub()

	jobDone := make(chan struct{})
	go func() {
		defer close(jobDone)
		defer resetSiteDataRunning.Store(false)
		if err := s.ClearFilms(); err != nil {
			syslog.Errorf("[数据重置] 重置失败: %v", err)
			return
		}
		syslog.Infof("[数据重置] 重置完成")
	}()

	send := func(p ResetProgress) bool {
		if onEvent != nil {
			if err := onEvent(p); err != nil {
				return true
			}
		}
		return !p.Running
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case p := <-events:
			if send(p) {
				return nil
			}
		case <-jobDone:
			for {
				select {
				case p := <-events:
					if send(p) {
						return nil
					}
				default:
					return nil
				}
			}
		}
	}
}

// FollowSiteReset 订阅当前重置进度。先推一次快照；没在重置就结束，正在重置则推到结束。
func (s *ResetService) FollowSiteReset(ctx context.Context, onEvent func(any) error) error {
	if ctx == nil {
		ctx = context.Background()
	}
	events := make(chan ResetProgress, 16)
	unsub := WatchResetProgress(func(p ResetProgress) {
		select {
		case events <- p:
		case <-ctx.Done():
		}
	})
	defer unsub()

	snap := GetResetProgress()
	if onEvent != nil {
		if err := onEvent(snap); err != nil {
			return nil
		}
	}
	if !snap.Running {
		return nil
	}
	for {
		select {
		case <-ctx.Done():
			return nil
		case p := <-events:
			if onEvent != nil {
				if err := onEvent(p); err != nil {
					return nil
				}
			}
			if !p.Running {
				return nil
			}
		}
	}
}
