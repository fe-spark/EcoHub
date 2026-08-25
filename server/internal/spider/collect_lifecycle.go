package spider

import (
	"errors"
	"fmt"
	"sort"
	"strings"
	"sync"
	"time"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/notify"
)

var collectLifecycle = newCollectLifecycle()

type collectLifecycleState struct {
	mu                  sync.Mutex
	cond                *sync.Cond
	activeSources       map[string]struct{}
	activeCount         int
	pendingFlushSources map[string]model.FilmSource
	affectedMIDs        map[int64]struct{}
	masterAffectedMIDs  map[int64]struct{}
	pendingMasterMIDs   map[string]map[int64]struct{}
	flushing            bool
}

func newCollectLifecycle() *collectLifecycleState {
	state := &collectLifecycleState{
		activeSources:       make(map[string]struct{}),
		pendingFlushSources: make(map[string]model.FilmSource),
		affectedMIDs:        make(map[int64]struct{}),
		masterAffectedMIDs:  make(map[int64]struct{}),
		pendingMasterMIDs:   make(map[string]map[int64]struct{}),
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
	for s.flushing {
		s.cond.Wait()
	}
	if _, ok := s.activeSources[sourceID]; ok {
		return fmt.Errorf("站点 %s 已有任务正在运行，已跳过本次采集", sourceID)
	}
	s.activeSources[sourceID] = struct{}{}
	s.activeCount++
	return nil
}

func (s *collectLifecycleState) beginMasterRebuild(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	s.pendingMasterMIDs[sourceID] = make(map[int64]struct{})
}

func (s *collectLifecycleState) waitAndBeginSource(sourceID string) error {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return errors.New("采集站点不存在")
	}

	s.mu.Lock()
	defer s.mu.Unlock()
	for s.flushing {
		s.cond.Wait()
	}
	for {
		if _, ok := s.activeSources[sourceID]; !ok {
			s.activeSources[sourceID] = struct{}{}
			s.activeCount++
			return nil
		}
		s.cond.Wait()
		for s.flushing {
			s.cond.Wait()
		}
	}
}

func (s *collectLifecycleState) endSource(sourceID string) {
	s.mu.Lock()
	defer s.mu.Unlock()
	s.finishSourceLocked(sourceID)
}

func (s *collectLifecycleState) endSourceAndFlush(source model.FilmSource) error {
	return s.finishSourceAndFlush(source)
}

func (s *collectLifecycleState) endSourceAndQueueFlush(source model.FilmSource) {
	source.Id = strings.TrimSpace(source.Id)
	if source.Id == "" {
		return
	}
	s.mu.Lock()
	s.finishSourceLocked(source.Id)
	s.pendingFlushSources[source.Id] = source
	s.mu.Unlock()
}

func (s *collectLifecycleState) addAffectedMIDs(mids []int64) {
	if len(mids) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, mid := range mids {
		if mid > 0 {
			s.affectedMIDs[mid] = struct{}{}
		}
	}
}

func (s *collectLifecycleState) addPendingMasterMIDs(sourceID string, mids []int64) {
	if len(mids) == 0 {
		return
	}
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pendingMasterMIDs[sourceID]
	if pending == nil {
		pending = make(map[int64]struct{})
		s.pendingMasterMIDs[sourceID] = pending
	}
	for _, mid := range mids {
		if mid > 0 {
			pending[mid] = struct{}{}
		}
	}
}

func (s *collectLifecycleState) publishPendingMasterMIDs(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	pending := s.pendingMasterMIDs[sourceID]
	delete(s.pendingMasterMIDs, sourceID)
	for mid := range pending {
		if mid > 0 {
			s.affectedMIDs[mid] = struct{}{}
			s.masterAffectedMIDs[mid] = struct{}{}
		}
	}
}

func (s *collectLifecycleState) discardPendingMasterMIDs(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	delete(s.pendingMasterMIDs, sourceID)
}

func (s *collectLifecycleState) addMasterAffectedMIDs(mids []int64) {
	if len(mids) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for _, mid := range mids {
		if mid > 0 {
			s.affectedMIDs[mid] = struct{}{}
			s.masterAffectedMIDs[mid] = struct{}{}
		}
	}
}

func (s *collectLifecycleState) restorePendingFlush(pending map[string]model.FilmSource, affectedMIDs []int64, masterMIDs []int64) {
	if len(pending) == 0 && len(affectedMIDs) == 0 && len(masterMIDs) == 0 {
		return
	}
	s.mu.Lock()
	defer s.mu.Unlock()
	for id, source := range pending {
		if strings.TrimSpace(id) != "" {
			s.pendingFlushSources[id] = source
		}
	}
	for _, mid := range affectedMIDs {
		if mid > 0 {
			s.affectedMIDs[mid] = struct{}{}
		}
	}
	for _, mid := range masterMIDs {
		if mid > 0 {
			s.affectedMIDs[mid] = struct{}{}
			s.masterAffectedMIDs[mid] = struct{}{}
		}
	}
	s.cond.Broadcast()
}

func (s *collectLifecycleState) drainAffectedMIDsLocked() []int64 {
	if len(s.affectedMIDs) == 0 {
		return nil
	}
	mids := make([]int64, 0, len(s.affectedMIDs))
	for mid := range s.affectedMIDs {
		if mid > 0 {
			mids = append(mids, mid)
		}
	}
	s.affectedMIDs = make(map[int64]struct{})
	sort.Slice(mids, func(i, j int) bool { return mids[i] < mids[j] })
	return mids
}

func (s *collectLifecycleState) drainMasterAffectedMIDsLocked() []int64 {
	if len(s.masterAffectedMIDs) == 0 {
		return nil
	}
	mids := make([]int64, 0, len(s.masterAffectedMIDs))
	for mid := range s.masterAffectedMIDs {
		if mid > 0 {
			mids = append(mids, mid)
		}
	}
	s.masterAffectedMIDs = make(map[int64]struct{})
	sort.Slice(mids, func(i, j int) bool { return mids[i] < mids[j] })
	return mids
}

func (s *collectLifecycleState) waitIdleLocked() {
	for s.flushing || s.activeCount > 0 {
		s.cond.Wait()
	}
}

func (s *collectLifecycleState) drainPendingLocked() (map[string]model.FilmSource, []int64, []int64) {
	var pending map[string]model.FilmSource
	if len(s.pendingFlushSources) > 0 {
		pending = s.pendingFlushSources
		s.pendingFlushSources = make(map[string]model.FilmSource)
	}
	affectedMIDs := s.drainAffectedMIDsLocked()
	masterMIDs := s.drainMasterAffectedMIDsLocked()
	s.flushing = true
	return pending, affectedMIDs, masterMIDs
}

func (s *collectLifecycleState) finishFlushing() {
	s.mu.Lock()
	s.flushing = false
	s.mu.Unlock()
	s.cond.Broadcast()
}

func (s *collectLifecycleState) flushPending() error {
	s.mu.Lock()
	s.waitIdleLocked()
	pending, affectedMIDs, masterMIDs := s.drainPendingLocked()
	s.mu.Unlock()
	defer s.finishFlushing()

	if pending == nil {
		return nil
	}
	finalMIDs, finalMasterMIDs, err := flushPendingSources(pending, affectedMIDs, masterMIDs)
	if err != nil {
		s.restorePendingFlush(pending, finalMIDs, finalMasterMIDs)
	}
	return err
}

func (s *collectLifecycleState) finishSourceAndFlush(source model.FilmSource) error {
	source.Id = strings.TrimSpace(source.Id)
	if source.Id == "" {
		return nil
	}

	s.mu.Lock()
	s.finishSourceLocked(source.Id)
	s.pendingFlushSources[source.Id] = source
	s.waitIdleLocked()
	pending, affectedMIDs, masterMIDs := s.drainPendingLocked()
	s.mu.Unlock()
	defer s.finishFlushing()

	if pending == nil {
		return nil
	}
	finalMIDs, finalMasterMIDs, err := flushPendingSources(pending, affectedMIDs, masterMIDs)
	if err != nil {
		s.restorePendingFlush(pending, finalMIDs, finalMasterMIDs)
		return err
	}
	return nil
}

func (s *collectLifecycleState) runFlush(flush func([]int64, []int64) error) error {
	s.mu.Lock()
	s.waitIdleLocked()
	pending, affectedMIDs, masterMIDs := s.drainPendingLocked()
	s.mu.Unlock()
	defer s.finishFlushing()

	var err error
	if pending != nil {
		var finalMIDs []int64
		var finalMasterMIDs []int64
		finalMIDs, finalMasterMIDs, err = flushPendingSources(pending, affectedMIDs, masterMIDs)
		if err != nil {
			s.restorePendingFlush(pending, finalMIDs, finalMasterMIDs)
		}
	}
	if err == nil {
		err = flush(affectedMIDs, masterMIDs)
	}
	return err
}

func (s *collectLifecycleState) runExclusive(action func() error) error {
	s.mu.Lock()
	s.waitIdleLocked()
	pending, affectedMIDs, masterMIDs := s.drainPendingLocked()
	s.mu.Unlock()
	defer s.finishFlushing()

	if pending != nil {
		finalMIDs, finalMasterMIDs, flushErr := flushPendingSources(pending, affectedMIDs, masterMIDs)
		if flushErr != nil {
			s.restorePendingFlush(pending, finalMIDs, finalMasterMIDs)
			return flushErr
		}
	}
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
		flushing := s.flushing
		s.mu.Unlock()
		if activeCount == 0 && !flushing {
			return nil
		}

		select {
		case <-deadline.C:
			return fmt.Errorf("等待采集任务停止超时: active=%d flushing=%t", activeCount, flushing)
		case <-ticker.C:
		}
	}
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

func flushPendingSources(sourceMap map[string]model.FilmSource, affectedMIDs []int64, masterMIDs []int64) ([]int64, []int64, error) {
	if len(sourceMap) == 0 {
		return affectedMIDs, masterMIDs, nil
	}
	sources := make([]model.FilmSource, 0, len(sourceMap))
	for _, source := range sourceMap {
		sources = append(sources, source)
	}
	sort.Slice(sources, func(i, j int) bool {
		if sources[i].Grade == sources[j].Grade {
			return sources[i].Id < sources[j].Id
		}
		return sources[i].Grade == model.MasterCollect
	})

	markSourcesFinalizing(sourceMap)
	finalMIDs, finalMasterMIDs, err := finalizeCollectRun(sources, affectedMIDs, masterMIDs)
	if err != nil {
		markSourcesFinalizeFailed(sourceMap)
		return finalMIDs, finalMasterMIDs, err
	}
	markSourcesPublished(sourceMap)
	return finalMIDs, finalMasterMIDs, nil
}

func flushSourcesPending(tag, trigger string, sources []model.FilmSource, startedAt time.Time, batch *notify.ChangeBatch) {
	if len(sources) == 0 {
		return
	}
	if startedAt.IsZero() {
		startedAt = time.Now()
	}

	flushMap := make(map[string]model.FilmSource, len(sources))
	ordered := make([]model.FilmSource, 0, len(sources))
	for _, source := range sources {
		source.Id = strings.TrimSpace(source.Id)
		if source.Id == "" {
			continue
		}
		if _, ok := flushMap[source.Id]; ok {
			continue
		}
		flushMap[source.Id] = source
		ordered = append(ordered, source)
	}
	if len(flushMap) == 0 {
		return
	}

	var finalizeErr error
	if err := collectLifecycle.runFlush(func(affectedMIDs []int64, masterMIDs []int64) error {
		_, _, err := flushPendingSources(flushMap, affectedMIDs, masterMIDs)
		return err
	}); err != nil {
		syslog.Errorf("[%s] 收尾刷新失败: %v", tag, err)
		finalizeErr = err
	}
	if trigger != "" {
		emitBatchSummaryForSources(batch, trigger, ordered, startedAt, finalizeErr)
	}
}

func shouldSkipCollectPublishOnError(source model.FilmSource, h int) bool {
	return source.Grade == model.MasterCollect && h < 0
}
