package spider

import (
	"errors"
	"fmt"
	"strings"
	"sync"
	"time"

	"server/internal/model"
)

var collectLifecycle = newCollectLifecycle()

type collectLifecycleState struct {
	mu            sync.Mutex
	cond          *sync.Cond
	activeSources map[string]struct{}
	activeCount   int
	publishing    int
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
	if _, ok := s.activeSources[sourceID]; ok {
		return fmt.Errorf("站点 %s 已有任务正在运行，已跳过本次采集", sourceID)
	}
	s.activeSources[sourceID] = struct{}{}
	s.activeCount++
	return nil
}

func (s *collectLifecycleState) waitAndBeginSource(sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return errors.New("采集站点不存在")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for {
		if _, ok := s.activeSources[sourceID]; !ok {
			s.activeSources[sourceID] = struct{}{}
			s.activeCount++
			return nil
		}
		s.cond.Wait()
	}
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
	s.mu.Lock()
	s.waitIdleLocked()
	s.mu.Unlock()

	s.beginPublish()
	defer s.endPublish()
	publishMu.Lock()
	defer publishMu.Unlock()

	return action()
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

	timer := time.NewTimer(timeout)
	defer timer.Stop()

	done := make(chan struct{})
	defer close(done)
	go func() {
		select {
		case <-timer.C:
			s.mu.Lock()
			s.cond.Broadcast()
			s.mu.Unlock()
		case <-done:
		}
	}()

	for s.activeCount > 0 || s.publishing > 0 {
		s.cond.Wait()
		select {
		case <-timer.C:
			if s.activeCount == 0 && s.publishing == 0 {
				return nil
			}
			return fmt.Errorf("等待采集空闲超时: active=%d publishing=%d", s.activeCount, s.publishing)
		default:
		}
	}
	return nil
}

func shouldSkipCollectPublishOnError(source model.FilmSource, h int) bool {
	return source.Grade == model.MasterCollect && h < 0
}
