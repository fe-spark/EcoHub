package siteops

import (
	"context"
	"errors"
	"fmt"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/analytics"
	"server/internal/collect"
	"server/internal/collectctl"
	"server/internal/infra/syslog"
	"server/internal/utils"
)

type ResetService struct{}

var ResetSvc = new(ResetService)

var (
	// resetSiteDataRunning 防止并发重复触发站点数据重置。
	resetSiteDataRunning atomic.Bool
	resetSiteLifecycle   atomic.Bool
	siteMaintActive      atomic.Bool
	// siteMaintMu 串行化站点数据重置与配置备份导入，消除并发交错与 TOCTOU。
	siteMaintMu sync.Mutex

	// 站点重置跨模块钩子，由装配层注入。
	OnResetRecoverAccess  func()
	OnResetRecoverCrontab func()
	OnResetReinitSite     func() error
)

// TryLockSiteMaintenance 非阻塞申请全局站点维护独占锁（供重置、配置导入与在线升级互斥）。
func TryLockSiteMaintenance() (func(), bool) {
	if !siteMaintMu.TryLock() {
		return nil, false
	}
	siteMaintActive.Store(true)
	return func() {
		siteMaintActive.Store(false)
		siteMaintMu.Unlock()
	}, true
}

func init() {
	collect.RegisterSiteResetChecker(func() bool {
		return resetSiteDataRunning.Load() || resetSiteLifecycle.Load()
	})
}

// SiteMaintenanceActive 是否正处于全站清空重置或维护期（供 HTTP 中间件阻断并发读写）。
func (s *ResetService) SiteMaintenanceActive() bool {
	return resetSiteLifecycle.Load() || siteMaintActive.Load()
}

// triggerResetCategorySync 数据重置后异步回补默认站分类，测试可替换避免访问外网。
var triggerResetCategorySync = func() {
	utils.GoSafe("重置后补全缺失来源分类", func() {
		collect.SpiderSvc.SyncMissingSourceCategories()
	})
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
	unlockMaint, ok := TryLockSiteMaintenance()
	if !ok {
		return errors.New("正在执行站点维护操作（重置或配置导入），请稍后再试")
	}
	defer unlockMaint()

	resetSiteLifecycle.Store(true)
	defer resetSiteLifecycle.Store(false)

	StartResetProgress()
	enteredReset := false
	defer func() {
		if retErr != nil {
			// 清库前置或半途失败时恢复访问统计开关与定时任务，避免调度停摆。
			if OnResetRecoverAccess != nil {
				OnResetRecoverAccess()
			}
			if enteredReset && OnResetRecoverCrontab != nil {
				OnResetRecoverCrontab()
			}
			FinishResetProgress(retErr)
		}
	}()

	ReportResetProgress(2, "正在停止后台任务")
	analytics.SetEnabled(false)
	analytics.Shutdown(500 * time.Millisecond)
	collect.StopAllCollect("数据重置，停止采集")
	collectctl.CollectSvc.CancelDelFilmSources(nil)
	collectctl.WaitAllAsyncWork()

	enteredReset = true
	if err := collect.PrepareAndRunReset(ReportResetProgress, FilmZero); err != nil {
		return err
	}
	if _, err := analytics.ClearData(0); err != nil {
		return err
	}

	ReportResetProgress(85, "正在写入初始化数据")
	if OnResetReinitSite != nil {
		if err := OnResetReinitSite(); err != nil {
			return err
		}
	}
	if OnResetRecoverAccess != nil {
		OnResetRecoverAccess()
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
	pushProgress := func(p ResetProgress) {
		// 非阻塞投递，慢速 SSE 客户端满队列时丢弃旧快照保留最新进度，不反向阻塞重置主流程。
		select {
		case events <- p:
		default:
			select {
			case <-events:
			default:
			}
			select {
			case events <- p:
			default:
			}
		}
	}
	unsub := WatchResetProgress(pushProgress)
	defer unsub()

	jobDone := make(chan struct{})
	go func() {
		defer close(jobDone)
		defer resetSiteDataRunning.Store(false)
		defer func() {
			if r := recover(); r != nil {
				err := fmt.Errorf("站点重置异常中止: %v", r)
				syslog.Errorf("[数据重置] %v", err)
				FinishResetProgress(err)
			}
		}()
		if err := s.ClearFilms(); err != nil {
			syslog.Errorf("[数据重置] 重置失败: %v", err)
			pushProgress(ResetProgress{Running: false, Error: err.Error()})
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
		default:
			select {
			case <-events:
			default:
			}
			select {
			case events <- p:
			default:
			}
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
