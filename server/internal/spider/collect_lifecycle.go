package spider

import (
	"context"
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"server/internal/model"
	"server/internal/spider/scheduler"
)

var collectLifecycle = newCollectLifecycle()

type collectLifecycleState struct {
	mu            sync.Mutex
	cond          *sync.Cond
	activeSources map[string]struct{}
	activeCount   int
	publishing    int
	draining      bool
}

func newCollectLifecycle() *collectLifecycleState {
	state := &collectLifecycleState{
		activeSources: make(map[string]struct{}),
	}
	state.cond = sync.NewCond(&state.mu)
	return state
}

func (s *collectLifecycleState) beginSource(sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return errors.New("采集站点不存在")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draining {
		return errCollectDuringMaintenance
	}
	if _, ok := s.activeSources[sourceID]; ok {
		return fmt.Errorf("站点 %s 已有任务正在运行，已跳过本次采集", sourceID)
	}
	s.activeSources[sourceID] = struct{}{}
	s.activeCount++
	return nil
}

func (s *collectLifecycleState) endSource(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishSourceLocked(sourceID)
}

func (s *collectLifecycleState) finishSourceLocked(sourceID string) {
	if sourceID = strings.TrimSpace(sourceID); sourceID == "" {
		return
	}
	if _, ok := s.activeSources[sourceID]; !ok {
		return
	}
	delete(s.activeSources, sourceID)
	if s.activeCount > 0 {
		s.activeCount--
	}
	s.cond.Broadcast()
}

func (s *collectLifecycleState) waitIdleLocked() {
	for s.activeCount > 0 {
		s.cond.Wait()
	}
}

func (s *collectLifecycleState) isBusy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.activeCount > 0 || s.publishing > 0
}
func (s *collectLifecycleState) isMaintenanceBusy() bool {
	s.mu.Lock()
	defer s.mu.Unlock()
	return s.draining
}

func (s *collectLifecycleState) beginPublish() {
	s.mu.Lock()
	s.publishing++
	s.mu.Unlock()
}

func (s *collectLifecycleState) endPublish() {
	s.mu.Lock()
	if s.publishing > 0 {
		s.publishing--
	}
	s.cond.Broadcast()
	s.mu.Unlock()
}

func (s *collectLifecycleState) runExclusive(action func() error) error {
	if err := s.beginDrain(30 * time.Second); err != nil {
		return err
	}
	defer s.endDrain()

	s.beginPublish()
	defer s.endPublish()
	publishMu.Lock()
	defer publishMu.Unlock()

	return action()
}

var (
	errMaintenanceBusy          = errors.New("已有维护任务在执行")
	errCollectDuringMaintenance = errors.New("维护中，已跳过")
	errDrainTimeout             = errors.New("等待采集空闲超时")
)

// maintenanceExitWait 停采集后等待在途写事务和本次运行退出的上限。
// 沿用删除站点的退出上限。到点仍未退出则维护失败，不开始清理。
var maintenanceExitWait = deleteStopTimeout

// beginMaintenance 立刻占用闸门并拒绝新采集。闸门已被占用时直接返回，不等待。
func (s *collectLifecycleState) beginMaintenance() error {
	s.mu.Lock()
	defer s.mu.Unlock()
	if s.draining {
		return errMaintenanceBusy
	}
	s.draining = true
	return nil
}

// runMaintenance 占用闸门、停止在途采集，等已开始的写事务、运行和这批收尾发布退出后独占执行。
// 闸门被占用时立即返回，不等待另一条维护。
func (s *collectLifecycleState) runMaintenance(reason string, action func() error) error {
	if err := s.beginMaintenance(); err != nil {
		return err
	}
	defer s.endDrain()

	StopAllCollect(reason)
	if err := s.waitStoppedCollect(time.Now().Add(maintenanceExitWait)); err != nil {
		return err
	}

	s.beginPublish()
	defer s.endPublish()
	publishMu.Lock()
	defer publishMu.Unlock()
	return action()
}

// waitStoppedCollect 等待停止后的写事务、站点运行和批次收尾发布退出。
func (s *collectLifecycleState) waitStoppedCollect(deadline time.Time) error {
	ctx, cancel := context.WithDeadline(context.Background(), deadline)
	defer cancel()
	ticker := time.NewTicker(20 * time.Millisecond)
	defer ticker.Stop()
	for {
		if err := scheduler.WaitPendingWrites(ctx); err != nil {
			return fmt.Errorf("等待在途写入结束失败: %w", err)
		}
		s.mu.Lock()
		idle := s.activeCount == 0 && s.publishing == 0
		s.mu.Unlock()
		if idle && activeBatchCount() == 0 {
			return nil
		}
		if !time.Now().Before(deadline) {
			s.mu.Lock()
			activeCount := s.activeCount
			publishing := s.publishing
			s.mu.Unlock()
			return fmt.Errorf("等待在途写入结束失败: active=%d publishing=%d batches=%d", activeCount, publishing, activeBatchCount())
		}
		select {
		case <-ctx.Done():
			return fmt.Errorf("等待在途写入结束失败: %w", ctx.Err())
		case <-ticker.C:
		}
	}
}

func (s *collectLifecycleState) beginDrain(timeout time.Duration) error {
	s.mu.Lock()
	deadline := time.Now().Add(timeout)
	timer := time.AfterFunc(timeout, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	defer timer.Stop()

	for s.draining {
		if !time.Now().Before(deadline) {
			s.mu.Unlock()
			return fmt.Errorf("%w: 维护闸门仍被占用", errDrainTimeout)
		}
		s.cond.Wait()
	}
	s.draining = true

	for s.activeCount > 0 || s.publishing > 0 {
		if !time.Now().Before(deadline) {
			s.draining = false
			s.cond.Broadcast()
			s.mu.Unlock()
			return fmt.Errorf("%w: active=%d publishing=%d", errDrainTimeout, s.activeCount, s.publishing)
		}
		s.cond.Wait()
	}
	s.mu.Unlock()
	return nil
}

func (s *collectLifecycleState) endDrain() {
	s.mu.Lock()
	s.draining = false
	s.cond.Broadcast()
	s.mu.Unlock()
}

// waitSource 等到该站离开采集生命周期。超时表示这次运行还没退出。
func (s *collectLifecycleState) waitSource(sourceID string, timeout time.Duration) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return nil
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	if _, ok := s.activeSources[sourceID]; !ok {
		return nil
	}
	if timeout <= 0 {
		return errors.New("停止采集超时，未删除")
	}

	deadline := time.Now().Add(timeout)
	timer := time.AfterFunc(timeout, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	defer timer.Stop()

	for {
		if _, ok := s.activeSources[sourceID]; !ok {
			return nil
		}
		if !time.Now().Before(deadline) {
			return errors.New("停止采集超时，未删除")
		}
		s.cond.Wait()
	}
}

func (s *collectLifecycleState) waitIdle(timeout time.Duration) error {
	deadline := time.NewTimer(timeout)
	defer deadline.Stop()
	ticker := time.NewTicker(time.Millisecond * 200)
	defer ticker.Stop()

	for {
		s.mu.Lock()
		activeCount := s.activeCount
		s.mu.Unlock()
		if activeCount == 0 {
			return nil
		}

		select {
		case <-deadline.C:
			return fmt.Errorf("等待采集任务停止超时: active=%d", activeCount)
		case <-ticker.C:
		}
	}
}

func (s *collectLifecycleState) waitNotBusy(timeout time.Duration) error {
	s.mu.Lock()
	defer s.mu.Unlock()

	if s.activeCount == 0 && s.publishing == 0 {
		return nil
	}
	if timeout <= 0 {
		return fmt.Errorf("等待采集空闲超时: active=%d publishing=%d", s.activeCount, s.publishing)
	}

	deadline := time.Now().Add(timeout)
	timer := time.AfterFunc(timeout, func() {
		s.mu.Lock()
		s.cond.Broadcast()
		s.mu.Unlock()
	})
	defer timer.Stop()

	for s.activeCount > 0 || s.publishing > 0 {
		if !time.Now().Before(deadline) {
			return fmt.Errorf("等待采集空闲超时: active=%d publishing=%d", s.activeCount, s.publishing)
		}
		s.cond.Wait()
	}
	return nil
}

func shouldSkipCollectPublishOnError(source model.FilmSource, h int) bool {
	return false
}
