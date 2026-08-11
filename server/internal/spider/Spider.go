package spider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"math/rand"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-sql-driver/mysql"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/utils"
)

/*
	采集逻辑 v3

*/

var spiderCore = &JsonCollect{}

const (
	pageCountRetryTimes   = 3
	filmDetailRetryTimes  = 3
	collectDBWriteRetries = 3
	recoverMaxRetryCount  = 5
)

var retryBackoffs = []time.Duration{
	2 * time.Second,
	5 * time.Second,
}

const (
	// 单站连续分页失败达到阈值后直接终止该站点，避免坏站点长期占用批量采集并发槽。
	collectSourceConsecutiveFailureLimit = 10
	// 批量采集只按低频进度打印，避免每页刷屏但保留观察采集是否推进的关键信息。
	collectProgressLogPageStep = 100
	collectFailureLogStep      = 10
)

// activeTasks 存储当前活跃采集任务的信息
var activeTasks sync.Map

// stopAllVersion 用于打断批量/自动采集的外层派发循环。
// 每次执行一键终止都会递增版本号，旧版本调度器检测到版本变化后不再继续启动新站点任务。
var stopAllVersion atomic.Uint64

// sourceWriteLocks 按站点串行化写库，避免同一站点多页事务并发 upsert/delete/update 时互相死锁。
var sourceWriteLocks sync.Map

var collectProgress sync.Map

var asyncMasterSearchTagsMu sync.Mutex

// taskMu 保护同一站点 cancel+Store 的原子性，防止并发截停竞态
var taskMu sync.Mutex

var collectLifecycle = newCollectLifecycle()

// requestGates 按站点控制外部采集接口请求间隔：同一站点两次请求之间至少等待站点 Interval。
var requestGates sync.Map

type sourceRequestGate struct {
	mu            sync.Mutex
	nextAllowedAt time.Time
	rateLimitHits int
}

func ClearLimiter(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return
	}
	if val, ok := requestGates.Load(sourceID); ok {
		gate := val.(*sourceRequestGate)
		gate.mu.Lock()
		gate.nextAllowedAt = time.Time{}
		gate.rateLimitHits = 0
		gate.mu.Unlock()
	}
}

func getSourceWriteLock(sourceID string) *sync.Mutex {
	if lock, ok := sourceWriteLocks.Load(sourceID); ok {
		return lock.(*sync.Mutex)
	}
	lock := &sync.Mutex{}
	actual, _ := sourceWriteLocks.LoadOrStore(sourceID, lock)
	return actual.(*sync.Mutex)
}

func runCollectDBWriteWithRetry(ctx context.Context, sourceName string, page int, write func() error) error {
	var err error
	for attempt := 1; attempt <= collectDBWriteRetries; attempt++ {
		err = write()
		if err == nil || !isRetryableDBWriteErr(err) || attempt == collectDBWriteRetries {
			return err
		}
		backoff := time.Duration(attempt*300) * time.Millisecond
		select {
		case <-ctx.Done():
			return ctx.Err()
		case <-time.After(backoff):
		}
	}
	return err
}

func isRetryableDBWriteErr(err error) bool {
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) {
		return mysqlErr.Number == 1213 || mysqlErr.Number == 1205
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "deadlock found") || strings.Contains(message, "lock wait timeout")
}

type collectTask struct {
	cancel context.CancelFunc
	reqId  string
}

type collectProgressState struct {
	mu      sync.RWMutex
	data    model.CollectProgress
	updated time.Time
}

func ensureCollectProgress(sourceID string, name string) *collectProgressState {
	if val, ok := collectProgress.Load(sourceID); ok {
		state := val.(*collectProgressState)
		state.mu.Lock()
		state.data.Id = sourceID
		if name != "" {
			state.data.Name = name
		}
		state.updated = time.Now()
		state.mu.Unlock()
		return state
	}
	state := &collectProgressState{data: model.CollectProgress{Id: sourceID, Name: name, Status: progressStatusStarting}, updated: time.Now()}
	actual, _ := collectProgress.LoadOrStore(sourceID, state)
	return actual.(*collectProgressState)
}

func updateCollectProgress(sourceID string, update func(*model.CollectProgress)) {
	if val, ok := collectProgress.Load(sourceID); ok {
		state := val.(*collectProgressState)
		state.mu.Lock()
		update(&state.data)
		state.updated = time.Now()
		state.mu.Unlock()
	}
}

func collectProgressSnapshot(sourceID string) (model.CollectProgress, bool) {
	if val, ok := collectProgress.Load(sourceID); ok {
		state := val.(*collectProgressState)
		state.mu.RLock()
		data := state.data
		state.mu.RUnlock()
		return data, true
	}
	return model.CollectProgress{}, false
}

// 采集进度状态机（SourceJob）：
//
//	starting → running → page_done → waiting_publish → finalizing → done
//	                ↘ stopped / failed
//
// page_done: 分页拉取+写库已结束（单站即将收尾）
// waiting_publish: 批量场景下本站已采完，等待整批统一发布
const (
	progressStatusStarting       = "starting"
	progressStatusRunning        = "running"
	progressStatusPageDone       = "page_done"
	progressStatusWaitingPublish = "waiting_publish"
	progressStatusFinalizing     = "finalizing"
	progressStatusDone           = "done"
	progressStatusFailed         = "failed"
	progressStatusStopped        = "stopped"
)

func isActiveCollectStatus(status string) bool {
	switch status {
	case progressStatusStarting, progressStatusRunning, progressStatusPageDone,
		progressStatusWaitingPublish, progressStatusFinalizing:
		return true
	default:
		return false
	}
}

func isTerminalCollectStatus(status string) bool {
	switch status {
	case progressStatusDone, progressStatusFailed, progressStatusStopped:
		return true
	default:
		return false
	}
}

// isPostFetchCollectStatus 分页已结束、等待整批收尾/发布的阶段。
// 这些状态可能因其它站仍在采而挂很久，禁止按单站 30m 未更新标 failed。
func isPostFetchCollectStatus(status string) bool {
	switch status {
	case progressStatusPageDone, progressStatusWaitingPublish, progressStatusFinalizing:
		return true
	default:
		return false
	}
}

// shouldMarkProgressStale 仅对「无 live task 的 starting/running」做超时 failed。
// waiting_publish / finalizing / page_done 属正常等待整批收尾，不超时。
func shouldMarkProgressStale(status string, live bool, age, staleAfter time.Duration) bool {
	if age < staleAfter {
		return false
	}
	if isPostFetchCollectStatus(status) {
		return false
	}
	if live && (status == progressStatusRunning || status == progressStatusStarting) {
		return false
	}
	return status == progressStatusStarting || status == progressStatusRunning
}

// hasAnyLiveOrActiveCollect 是否仍有未完成的采集（含 live task 与活跃进度）。
// 有则保留全部终态进度，避免部分卡片进度条先消失。
func hasAnyLiveOrActiveCollect() bool {
	has := false
	activeTasks.Range(func(key, value any) bool {
		has = true
		return false
	})
	if has {
		return true
	}
	collectProgress.Range(func(key, value any) bool {
		state := value.(*collectProgressState)
		state.mu.RLock()
		active := isActiveCollectStatus(state.data.Status)
		state.mu.RUnlock()
		if active {
			has = true
			return false
		}
		return true
	})
	return has
}

// latestTerminalProgressTime 最晚进入终态的时间；无终态时 ok=false。
func latestTerminalProgressTime() (latest time.Time, ok bool) {
	collectProgress.Range(func(key, value any) bool {
		state := value.(*collectProgressState)
		state.mu.RLock()
		status := state.data.Status
		updated := state.updated
		state.mu.RUnlock()
		if !isTerminalCollectStatus(status) {
			return true
		}
		if !ok || updated.After(latest) {
			latest = updated
			ok = true
		}
		return true
	})
	return latest, ok
}

// purgeAllTerminalProgress 删除全部 done/failed/stopped 进度（统一消失）。
func purgeAllTerminalProgress() {
	collectProgress.Range(func(key, value any) bool {
		id, _ := key.(string)
		state := value.(*collectProgressState)
		state.mu.RLock()
		terminal := isTerminalCollectStatus(state.data.Status)
		state.mu.RUnlock()
		if terminal {
			collectProgress.Delete(id)
		}
		return true
	})
}

// maybePurgeTerminalProgressTogether 全部采集结束后，以最晚终态时间为基准保留 retain，再一次性清空。
func maybePurgeTerminalProgressTogether(now time.Time) {
	if hasAnyLiveOrActiveCollect() {
		return
	}
	latest, ok := latestTerminalProgressTime()
	if !ok {
		return
	}
	if now.Sub(latest) < progressRetainDuration() {
		return
	}
	purgeAllTerminalProgress()
}

func canEnterFinalizing(status string) bool {
	switch status {
	case progressStatusStarting, progressStatusRunning, progressStatusPageDone,
		progressStatusWaitingPublish, progressStatusStopped, progressStatusDone:
		return true
	default:
		return false
	}
}

func isCollectProgressStopped(sourceID string) bool {
	if progress, ok := collectProgressSnapshot(sourceID); ok {
		return progress.Status == progressStatusStopped
	}
	return false
}

func isCollectProgressStarting(sourceID string) bool {
	if progress, ok := collectProgressSnapshot(sourceID); ok {
		return progress.Status == progressStatusStarting
	}
	return false
}

func isCollectAlreadyQueuedOrRunning(sourceID string) bool {
	if _, ok := activeTasks.Load(sourceID); ok {
		return true
	}
	// 只检查当前源，避免热路径全表扫；超时活跃状态就地标 failed 以允许重采。
	if refreshAndIsBlockingSourceProgress(sourceID) {
		return true
	}
	return false
}

func progressRetainDuration() time.Duration {
	sec := config.CollectProgressRetainSec
	if sec <= 0 {
		sec = config.DefaultCollectProgressRetainSec
	}
	return time.Duration(sec) * time.Second
}

func progressStaleDuration() time.Duration {
	sec := config.CollectProgressStaleSec
	if sec <= 0 {
		sec = config.DefaultCollectProgressStaleSec
	}
	return time.Duration(sec) * time.Second
}

// refreshAndIsBlockingSourceProgress 检查单站是否仍处于不可重入的活跃进度。
// 若活跃但已超时且无 live task，则标 failed 并返回 false。
func refreshAndIsBlockingSourceProgress(sourceID string) bool {
	val, ok := collectProgress.Load(sourceID)
	if !ok {
		return false
	}
	state := val.(*collectProgressState)
	now := time.Now()
	staleAfter := progressStaleDuration()

	state.mu.Lock()
	if !isActiveCollectStatus(state.data.Status) {
		state.mu.Unlock()
		return false
	}
	_, live := activeTasks.Load(sourceID)
	age := now.Sub(state.updated)
	// 收尾等待：仍阻挡重采，但不超时 failed。
	if isPostFetchCollectStatus(state.data.Status) {
		state.mu.Unlock()
		return true
	}
	if live && (state.data.Status == progressStatusRunning || state.data.Status == progressStatusStarting) {
		state.mu.Unlock()
		return true
	}
	// 无 live task 的 starting/running 超时 → failed，允许重采。
	if shouldMarkProgressStale(state.data.Status, live, age, staleAfter) {
		old := state.data.Status
		name := state.data.Name
		state.data.Status = progressStatusFailed
		state.updated = now
		state.mu.Unlock()
		log.Printf("[Spider] 进度超时清理 source=%s status=%s age=%s -> failed",
			sourceID, old, age.Round(time.Second))
		emitProgressStaleNotify(sourceID, name, old, age)
		return false
	}
	state.mu.Unlock()
	return true
}

// pruneStaleCollectProgress 后台巡检：超时活跃 → failed；全部结束后终态进度统一删除。
func pruneStaleCollectProgress() {
	now := time.Now()
	staleAfter := progressStaleDuration()
	collectProgress.Range(func(key, value any) bool {
		id, _ := key.(string)
		state := value.(*collectProgressState)
		state.mu.Lock()
		status := state.data.Status
		updated := state.updated
		age := now.Sub(updated)
		if isActiveCollectStatus(status) {
			_, live := activeTasks.Load(id)
			if shouldMarkProgressStale(status, live, age, staleAfter) {
				name := state.data.Name
				state.data.Status = progressStatusFailed
				state.updated = now
				state.mu.Unlock()
				log.Printf("[Spider] 进度超时清理 source=%s status=%s age=%s -> failed", id, status, age.Round(time.Second))
				emitProgressStaleNotify(id, name, status, age)
				return true
			}
		}
		state.mu.Unlock()
		return true
	})
	// 终态不按单卡超时删除：有活跃采集时全部保留；全部完成后再统一消失。
	maybePurgeTerminalProgressTogether(now)
}

var progressHousekeepingOnce sync.Once

func startCollectProgressHousekeeping() {
	progressHousekeepingOnce.Do(func() {
		go func() {
			ticker := time.NewTicker(30 * time.Second)
			defer ticker.Stop()
			for range ticker.C {
				pruneStaleCollectProgress()
			}
		}()
	})
}

// markSourcePagesFinished 分页阶段结束：单站进入 page_done，批量进入 waiting_publish。
func markSourcePagesFinished(sourceID string, flushAtEnd bool) {
	updateCollectProgress(sourceID, func(progress *model.CollectProgress) {
		if progress.Status != progressStatusRunning && progress.Status != progressStatusStarting {
			return
		}
		if flushAtEnd {
			progress.Status = progressStatusPageDone
			return
		}
		progress.Status = progressStatusWaitingPublish
	})
	// 站点分页结束：强制落盘合并的 stats / 缓存清理。
	flushCollectHotpathSideEffects(sourceID)
}

// flushCollectHotpathSideEffects 将采集热路径中合并的 stats 与缓存失效真正执行。
func flushCollectHotpathSideEffects(sourceIDs ...string) {
	if len(sourceIDs) == 0 {
		repository.FlushCollectSourceStats()
		filmrepo.FlushCollectCacheInvalidations()
		return
	}
	repository.FlushCollectSourceStats(sourceIDs...)
	filmrepo.FlushCollectCacheInvalidations()
}

func filterCollectableSources(sources []model.FilmSource, tag string) []model.FilmSource {
	filtered := make([]model.FilmSource, 0, len(sources))
	seen := make(map[string]struct{}, len(sources))
	for _, source := range sources {
		if _, ok := seen[source.Id]; ok {
			log.Printf("[%s] 站点 %s 在本轮采集列表中重复，跳过", tag, source.Name)
			continue
		}
		seen[source.Id] = struct{}{}
		if isCollectAlreadyQueuedOrRunning(source.Id) {
			log.Printf("[%s] 站点 %s 已在采集队列或正在运行，跳过", tag, source.Name)
			continue
		}
		filtered = append(filtered, source)
	}
	return filtered
}

func markSourcesCollectStarting(sources []model.FilmSource) {
	for _, source := range sources {
		state := ensureCollectProgress(source.Id, source.Name)
		state.mu.Lock()
		state.data.Total = 0
		state.data.Current = 0
		state.data.Success = 0
		state.data.Failed = 0
		state.data.Status = progressStatusStarting
		state.updated = time.Now()
		state.mu.Unlock()
	}
}

func markProgressStopped(sourceID string) {
	updateCollectProgress(sourceID, func(progress *model.CollectProgress) {
		if progress.Status == progressStatusStarting || progress.Status == progressStatusRunning {
			progress.Status = progressStatusStopped
		}
	})
}

func StopTask(sourceID string) {
	markProgressStopped(sourceID)
	if val, ok := activeTasks.Load(sourceID); ok {
		val.(collectTask).cancel()
	}
}

func PrepareSingleCollectStart(source model.FilmSource) error {
	if isCollectAlreadyQueuedOrRunning(source.Id) {
		return fmt.Errorf("站点 %s 已在采集队列或正在运行，已跳过本次采集", source.Name)
	}
	markSourcesCollectStarting([]model.FilmSource{source})
	return nil
}

func markSourcesFinalizing(sources map[string]model.FilmSource) {
	ids := make([]string, 0, len(sources))
	for _, source := range sources {
		ids = append(ids, source.Id)
		updateCollectProgress(source.Id, func(progress *model.CollectProgress) {
			if canEnterFinalizing(progress.Status) {
				progress.Status = progressStatusFinalizing
			}
		})
	}
	// 整批进入收尾前，确保合并的 stats/缓存已刷出。
	flushCollectHotpathSideEffects(ids...)
}

func markSourcesPublished(sources map[string]model.FilmSource) {
	for _, source := range sources {
		updateCollectProgress(source.Id, func(progress *model.CollectProgress) {
			if progress.Status == progressStatusFinalizing {
				progress.Status = progressStatusDone
			}
		})
	}
}

func markSourcesFinalizeFailed(sources map[string]model.FilmSource) {
	for _, source := range sources {
		updateCollectProgress(source.Id, func(progress *model.CollectProgress) {
			if progress.Status == progressStatusFinalizing {
				progress.Status = progressStatusFailed
			}
		})
	}
}

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

// drainPendingLocked snapshots and clears pendingFlushSources/affected MIDs
// and sets flushing=true. Caller must hold s.mu and call finishFlushing after.
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
	// 某个站点释放后，需要唤醒等待同站点重试的协程重新竞争。
	// flushPending/runExclusive 也依赖该广播在 activeCount 归零时继续推进。
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

func finalizeCollectRun(sources []model.FilmSource, affectedMIDs []int64, masterMIDs []int64) ([]int64, []int64, error) {
	if len(sources) == 0 {
		return affectedMIDs, masterMIDs, nil
	}
	start := time.Now()
	log.Printf("[Spider][Finalizer] 开始收尾发布 source_count=%d", len(sources))

	if err := flushMasterSideEffects(sources, masterMIDs); err != nil {
		return affectedMIDs, masterMIDs, err
	}
	playSummaryMIDs, err := flushPlaySummaryRefresh()
	affectedMIDs = append(affectedMIDs, playSummaryMIDs...)
	if err != nil {
		return affectedMIDs, masterMIDs, err
	}
	version, err := publishFilmSnapshot(affectedMIDs)
	if err != nil {
		return affectedMIDs, masterMIDs, err
	}
	log.Printf("[Spider][Finalizer] 收尾发布完成 version=%s source_count=%d cost=%s", version, len(sources), time.Since(start))
	return affectedMIDs, masterMIDs, nil
}

func flushMasterSideEffects(sources []model.FilmSource, masterMIDs []int64) error {
	for _, source := range sources {
		if source.Grade == model.MasterCollect {
			scheduleMasterSearchTagsRefresh(masterMIDs)
			filmrepo.ClearTVBoxConfigCache()
			return nil
		}
	}
	return nil
}

func scheduleMasterSearchTagsRefresh(masterMIDs []int64) {
	mids := normalizeAffectedMIDs(masterMIDs)
	if len(mids) == 0 {
		return
	}
	go func() {
		asyncMasterSearchTagsMu.Lock()
		defer asyncMasterSearchTagsMu.Unlock()

		start := time.Now()
		log.Printf("[Spider][Finalizer] 主站搜索标签异步刷新开始 mid_count=%d", len(mids))
		if err := filmrepo.RefreshSearchTagsByMids(mids...); err != nil {
			syslog.Errorf("[Spider][Finalizer] 主站搜索标签异步刷新失败 mid_count=%d err=%v", len(mids), err)
			return
		}
		filmrepo.ClearAllSearchTagsCache()
		filmrepo.ClearAdminFilmSearchCache()
		log.Printf("[Spider][Finalizer] 主站搜索标签异步刷新完成 mid_count=%d cost=%s", len(mids), time.Since(start))
	}()
}

func flushPlaySummaryRefresh() ([]int64, error) {
	start := time.Now()
	mids, err := filmrepo.FlushPendingPlaySummaryRefresh()
	if err != nil {
		return mids, fmt.Errorf("flush play summary refresh failed: %w", err)
	}
	log.Printf("[Spider][Finalizer] 播放源摘要刷新完成 mid_count=%d cost=%s", len(mids), time.Since(start))
	return mids, nil
}

func publishFilmSnapshot(affectedMIDs []int64) (string, error) {
	start := time.Now()
	mids := normalizeAffectedMIDs(affectedMIDs)
	if len(mids) == 0 {
		if hasSnapshot, err := filmrepo.HasPublishedFilmListSnapshot(); err != nil {
			return "", err
		} else if !hasSnapshot {
			log.Printf("[Spider][Finalizer] 主站快照未发布，跳过空增量快照发布 cost=%s", time.Since(start))
			return "", nil
		}
	}
	version, updated, err := filmrepo.UpsertActiveSnapshotsByMids(mids...)
	if err != nil {
		return "", fmt.Errorf("upsert film list snapshot failed: %w", err)
	}
	log.Printf("[Spider][Finalizer] 前台影片列表快照已增量发布 version=%s input=%d updated=%d cost=%s", version, len(mids), updated, time.Since(start))
	return version, nil
}

func normalizeAffectedMIDs(mids []int64) []int64 {
	if len(mids) == 0 {
		return nil
	}
	seen := make(map[int64]struct{}, len(mids))
	res := make([]int64, 0, len(mids))
	for _, mid := range mids {
		if mid <= 0 {
			continue
		}
		if _, ok := seen[mid]; ok {
			continue
		}
		seen[mid] = struct{}{}
		res = append(res, mid)
	}
	sort.Slice(res, func(i, j int) bool { return res[i] < res[j] })
	return res
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

func getSourceInterval(sourceID string, fallback *model.FilmSource) time.Duration {
	if sourceID = strings.TrimSpace(sourceID); sourceID != "" {
		if latest := repository.FindCollectSourceById(sourceID); latest != nil && latest.Interval > 0 {
			return time.Duration(latest.Interval) * time.Millisecond
		}
	}
	if fallback != nil && fallback.Interval > 0 {
		return time.Duration(fallback.Interval) * time.Millisecond
	}
	return config.DefaultSpiderInterval * time.Millisecond
}

func getSourceRequestGate(sourceID string) *sourceRequestGate {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" {
		return &sourceRequestGate{}
	}
	if val, ok := requestGates.Load(sourceID); ok {
		return val.(*sourceRequestGate)
	}
	gate := &sourceRequestGate{}
	actual, _ := requestGates.LoadOrStore(sourceID, gate)
	return actual.(*sourceRequestGate)
}

func waitSourceRequestTurn(ctx context.Context, s *model.FilmSource, tag string) (func(error), error) {
	if s == nil {
		return func(error) {}, errors.New("采集站信息不存在")
	}

	gate := getSourceRequestGate(s.Id)

	for {
		gate.mu.Lock()
		waitUntil := gate.nextAllowedAt
		now := time.Now()
		if waitUntil.IsZero() || !waitUntil.After(now) {
			grantedAt := now
			interval := getSourceInterval(s.Id, s)
			gate.nextAllowedAt = grantedAt.Add(interval)
			gate.mu.Unlock()
			return func(requestErr error) {
				if requestErr == nil {
					gate.mu.Lock()
					if gate.rateLimitHits > 0 {
						gate.rateLimitHits = 0
					}
					gate.mu.Unlock()
					return
				}
				if utils.IsRateLimitedErr(requestErr) {
					gate.mu.Lock()
					gate.rateLimitHits++
					hits := gate.rateLimitHits
					if hits > 5 {
						hits = 5
					}
					// 指数退避：每次 429 限流保护时间按 2^(hits-1) 翻倍延长 (1s, 2s, 4s, 8s, 16s)
					backoffFactor := time.Duration(1 << uint(hits-1))
					cooldown := interval * backoffFactor
					if cooldown < 1*time.Second {
						cooldown = 1 * time.Second
					}
					if cooldown > 15*time.Second {
						cooldown = 15 * time.Second
					}
					protectUntil := time.Now().Add(cooldown)
					if gate.nextAllowedAt.Before(protectUntil) {
						gate.nextAllowedAt = protectUntil
					}
					nextAllowedAt := gate.nextAllowedAt
					gate.mu.Unlock()
					log.Printf("[Spider][RateLimit] 站点 %s %s触发限流，指数退避冷却 cooldown_ms=%d (hits=%d) next_at=%d err=%v", s.Name, tag, cooldown.Milliseconds(), hits, nextAllowedAt.UnixMilli(), requestErr)
					return
				}
			}, nil
		}

		cooldown := waitUntil.Sub(now)
		gate.mu.Unlock()
		timer := time.NewTimer(cooldown)
		select {
		case <-ctx.Done():
			if !timer.Stop() {
				<-timer.C
			}
			return nil, ctx.Err()
		case <-timer.C:
		}
	}
}

func getPageCountWithRetry(ctx context.Context, s *model.FilmSource, r utils.RequestInfo) (int, error) {
	var lastErr error
	for attempt := 1; attempt <= pageCountRetryTimes; attempt++ {
		select {
		case <-ctx.Done():
			return 0, ctx.Err()
		default:
		}

		release, err := waitSourceRequestTurn(ctx, s, fmt.Sprintf("页数请求 attempt=%d ", attempt))
		if err != nil {
			return 0, err
		}
		pageCount, err := spiderCore.GetPageCount(r)
		if err == nil {
			release(nil)
			return pageCount, nil
		}
		release(err)
		lastErr = err
		if attempt < pageCountRetryTimes {
			if waitErr := waitRetryBackoff(ctx, attempt); waitErr != nil {
				return 0, waitErr
			}
		}
	}
	return 0, lastErr
}

func getFilmDetailWithRetry(ctx context.Context, s *model.FilmSource, r utils.RequestInfo) ([]model.MovieDetail, error) {
	var lastErr error
	page := r.Params.Get("pg")
	if page == "" {
		page = "-"
	}
	for attempt := 1; attempt <= filmDetailRetryTimes; attempt++ {
		select {
		case <-ctx.Done():
			return nil, ctx.Err()
		default:
		}

		release, err := waitSourceRequestTurn(ctx, s, fmt.Sprintf("分页请求 pg=%s attempt=%d ", page, attempt))
		if err != nil {
			return nil, err
		}
		list, err := spiderCore.GetFilmDetail(r)
		if err == nil && len(list) > 0 {
			release(nil)
			return list, nil
		}
		release(err)
		if err != nil {
			lastErr = err
		} else {
			lastErr = errors.New("response list is empty")
		}
		if attempt < filmDetailRetryTimes {
			if waitErr := waitRetryBackoff(ctx, attempt); waitErr != nil {
				return nil, waitErr
			}
		}
	}
	return nil, lastErr
}

func waitRetryBackoff(ctx context.Context, attempt int) error {
	if attempt <= 0 {
		attempt = 1
	}
	base := time.Duration(1<<uint(attempt-1)) * time.Second
	if base > 10*time.Second {
		base = 10 * time.Second
	}
	jitter := time.Duration(rand.Intn(500)) * time.Millisecond
	delay := base + jitter

	timer := time.NewTimer(delay)
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func countLiveCollectTasks() int {
	n := 0
	activeTasks.Range(func(key, value any) bool {
		n++
		return true
	})
	return n
}

// getSourcePageConcurrency 单站分页 HTTP 并发；仅 1 站在跑时用 Solo 档吃满写阀。
func getSourcePageConcurrency(_ *model.FilmSource) int {
	base := config.CollectPageWorkers
	if base <= 0 {
		base = config.DefaultCollectPageWorkers
	}
	solo := config.CollectPageWorkersSolo
	if solo <= 0 {
		solo = config.DefaultCollectPageWorkersSolo
	}
	if solo < base {
		solo = base
	}
	// live<=1：单站全量/仅剩一站时提高页并发
	if countLiveCollectTasks() <= 1 {
		if solo <= 0 {
			return 1
		}
		return solo
	}
	if base <= 0 {
		return 1
	}
	return base
}

// prioritizeCollectSources 主采集站优先派发，便于有限站并发时先跑主站。
func prioritizeCollectSources(sources []model.FilmSource) []model.FilmSource {
	if len(sources) <= 1 {
		return sources
	}
	out := make([]model.FilmSource, 0, len(sources))
	for _, s := range sources {
		if s.Grade == model.MasterCollect {
			out = append(out, s)
		}
	}
	for _, s := range sources {
		if s.Grade != model.MasterCollect {
			out = append(out, s)
		}
	}
	return out
}

func runSourcesWithLimit(sources []model.FilmSource, h int, tag, trigger string) {
	if len(sources) == 0 {
		return
	}
	sources = filterCollectableSources(sources, tag)
	if len(sources) == 0 {
		return
	}
	markSourcesCollectStarting(sources)
	runSourcesWithLimitCore(sources, h, tag, trigger)
}

// runSourcesWithLimitCore 假定 sources 已过滤且已 mark starting（或由调用方保证）。
func runSourcesWithLimitCore(sources []model.FilmSource, h int, tag, trigger string) {
	if len(sources) == 0 {
		return
	}

	// 闭环保底：本地分类树为空时，仅从主站同步（含未启用主站）。
	// 没有主站就不能有分类树——不从附属站拉分类。
	if db.Mdb != nil {
		var categoryCount int64
		_ = db.Mdb.Model(&model.Category{}).Count(&categoryCount).Error
		if categoryCount == 0 {
			syslog.Warnf("[Spider] 检测到本地分类树为空(0 个分类)，尝试从主站同步分类树...")
			target := repository.PickMasterSourceForCategory()
			if target == nil {
				syslog.Warnf("[Spider] 无主采集站，跳过分类树同步（没有主站就不能有分类树）")
			} else if err := CollectCategory(target); err != nil {
				syslog.Errorf("[Spider] 采集前自动同步主站分类失败 name=%s state=%v: %v",
					target.Name, target.State, err)
			} else {
				repository.RefreshCategoryCache()
				syslog.Infof("[Spider] 采集前自动同步主站分类成功 name=%s state=%v",
					target.Name, target.State)
			}
		}
	}

	sources = prioritizeCollectSources(sources)
	// 变更 mid 写入 MySQL 批次（无内存全量、无 Redis 会话），批次显式沿采集链传递
	batch := notify.StartChangeBatch()
	startedAt := time.Now()
	runVersion := stopAllVersion.Load()
	// 2C2G 默认限制同时跑的站点数，避免 N 站 × 页 worker 把内存/写阀打爆。
	sourceLimit := config.CollectSourceConcurrency
	if sourceLimit < 0 {
		sourceLimit = 0
	}
	limitDesc := "不限制"
	if sourceLimit > 0 {
		limitDesc = fmt.Sprintf("%d", sourceLimit)
	}
	log.Printf("[%s] 采集派发 站点数=%d 站点并发=%s 页并发=%d 写阀 inflight=%d pages/s=%d",
		tag, len(sources), limitDesc, config.CollectPageWorkers,
		config.CollectWriteMaxInflight, config.CollectWritePagesPerSec)
	runSourcesGroupWithLimit(sources, h, tag, sourceLimit, runVersion, batch)
	var finalizeErr error
	if err := collectLifecycle.flushPending(); err != nil {
		syslog.Errorf("[%s] 批量采集收尾刷新失败: %v", tag, err)
		finalizeErr = err
	}
	emitBatchSummaryForSources(batch, trigger, sources, startedAt, finalizeErr)
}

func isDispatchStopped(runVersion uint64) bool {
	return stopAllVersion.Load() != runVersion
}

func runSourcesGroupWithLimit(sources []model.FilmSource, h int, tag string, limit int, runVersion uint64, batch *notify.ChangeBatch) {
	if len(sources) == 0 {
		return
	}
	var sem chan struct{}
	if limit > 0 {
		sem = make(chan struct{}, limit)
	}
	var wg sync.WaitGroup

	for idx, src := range sources {
		if isDispatchStopped(runVersion) {
			log.Printf("[%s] 检测到一键终止，停止派发剩余站点任务", tag)
			for _, skipped := range sources[idx:] {
				collectWrites.finishSource(skipped.Grade, skipped.Id)
			}
			break
		}
		if isCollectProgressStopped(src.Id) {
			log.Printf("[%s] 站点 %s 已在排队中停止，跳过派发", tag, src.Name)
			collectWrites.finishSource(src.Grade, src.Id)
			continue
		}
		wg.Add(1)
		if sem != nil {
			sem <- struct{}{}
		}
		go func(fs model.FilmSource) {
			defer wg.Done()
			defer func() {
				if sem != nil {
					<-sem
				}
			}()
			defer collectWrites.finishSource(fs.Grade, fs.Id)
			if isDispatchStopped(runVersion) {
				log.Printf("[%s] 站点 %s 在启动前被一键终止拦截", tag, fs.Name)
				return
			}
			if isCollectProgressStopped(fs.Id) {
				log.Printf("[%s] 站点 %s 已在启动前停止，跳过采集", tag, fs.Name)
				return
			}
			if err := handleCollectWithStopVersion(fs.Id, h, &runVersion, false, false, batch); err != nil {
				syslog.Errorf("[%s] 采集站点 %s 失败: %v", tag, fs.Name, err)
			}
		}(src)
	}
	wg.Wait()
}

// ======================================================= 通用采集方法  =======================================================

// HandleCollect 影视采集  id-采集站ID h-时长/h
func HandleCollect(id string, h int) error {
	return handleCollectWithStopVersion(id, h, nil, true, false, nil)
}

func HandlePreparedCollect(id string, h int) error {
	return handleCollectWithStopVersion(id, h, nil, true, true, nil)
}

func handleCollectWithStopVersion(id string, h int, runVersion *uint64, flushAtEnd bool, allowPreparedStart bool, batch *notify.ChangeBatch) (retErr error) {
	hadWrites := false
	collectStartedAt := time.Now()
	if runVersion != nil && isDispatchStopped(*runVersion) {
		return errors.New("任务已被一键终止，跳过启动")
	}
	if (runVersion != nil || allowPreparedStart) && isCollectProgressStopped(id) {
		return errors.New("任务已被停止，跳过启动")
	}
	if runVersion == nil && !allowPreparedStart && isCollectProgressStarting(id) {
		return errors.New("该采集站已在批量队列中，已跳过本次采集")
	}
	// 1. 首先通过ID获取对应采集站信息
	s := repository.FindCollectSourceById(id)
	if s == nil {
		return errors.New("采集站点不存在")
	} else if !s.State {
		return errors.New("采集站点已停用")
	}
	if err := collectLifecycle.beginSource(id); err != nil {
		log.Printf("[Spider] 站点 %s 无法启动采集: %v\n", id, err)
		return err
	}
	// 主站采集前兜底：分类被重置/清空后若未同步，影片会以 pid=0 入库并被列表读模型隐藏。
	if err := ensureMasterCategoriesReady(s); err != nil {
		retErr = err
		return err
	}
	// 确认可开跑后再开变更批次，避免 early-return 泄漏/误开。
	// 单站（flushAtEnd）独立开批；批量由 runSourcesWithLimit 统一开启并传入。
	if flushAtEnd {
		batch = notify.StartChangeBatch()
	}
	isMasterFullCollect := s.Grade == model.MasterCollect && h < 0
	if isMasterFullCollect {
		collectLifecycle.beginMasterRebuild(s.Id)
	}
	defer func() {
		originalErr := retErr
		// 无论成功/失败/停止，结束本站时刷出合并的 stats 与缓存清理。
		flushCollectHotpathSideEffects(s.Id)
		if originalErr != nil && (!hadWrites || shouldSkipCollectPublishOnError(*s, h)) {
			if isMasterFullCollect {
				collectLifecycle.discardPendingMasterMIDs(s.Id)
			}
			collectLifecycle.endSource(s.Id)
			if flushAtEnd {
				// 未进入统一收尾的单站失败：只发批次摘要。
				// 即时 source_failed 由熔断路径发送，避免 minInterval=0 时双发。
				noteSourceError(s.Id, originalErr.Error())
				emitBatchSummaryForSources(batch, model.NotifyTriggerManual, []model.FilmSource{*s}, collectStartedAt, originalErr)
			}
			return
		}
		if isMasterFullCollect {
			collectLifecycle.publishPendingMasterMIDs(s.Id)
		}
		if flushAtEnd {
			updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
				if canEnterFinalizing(progress.Status) {
					progress.Status = progressStatusFinalizing
				}
			})
			flushErr := collectLifecycle.finishSourceAndFlush(*s)
			if originalErr == nil && flushErr != nil {
				retErr = flushErr
			}
			updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
				if flushErr != nil && progress.Status == progressStatusFinalizing {
					progress.Status = progressStatusFailed
					return
				}
				if progress.Status == progressStatusFinalizing {
					progress.Status = progressStatusDone
				}
			})
			// 单站采集（flushAtEnd）在收尾后直接发批次摘要
			emitBatchSummaryForSources(batch, model.NotifyTriggerManual, []model.FilmSource{*s}, collectStartedAt, flushErr)
			return
		}
		// 批量：分页已结束（含 0 页）统一 waiting_publish，等待整批 flushPending。
		if !isCollectProgressStopped(s.Id) {
			updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
				switch progress.Status {
				case progressStatusRunning, progressStatusStarting, progressStatusPageDone:
					progress.Status = progressStatusWaitingPublish
				}
			})
		}
		collectLifecycle.endSourceAndQueueFlush(*s)
	}()

	// 同站跳过：如果该站点已有采集任务在运行，则跳过此次采集任务
	reqId := utils.GenerateSalt()

	taskMu.Lock()
	if runVersion != nil && isDispatchStopped(*runVersion) {
		taskMu.Unlock()
		return errors.New("任务已被一键终止，跳过启动")
	}
	if _, ok := activeTasks.Load(id); ok {
		taskMu.Unlock()
		log.Printf("[Spider] 站点 %s 已有任务正在运行，跳过本次采集...\n", id)
		return fmt.Errorf("站点 %s 已有任务正在运行，已跳过本次采集", id)
	}
	ctx, cancel := context.WithCancel(context.Background())
	activeTasks.Store(id, collectTask{cancel: cancel, reqId: reqId})
	taskMu.Unlock()

	// 任务完成后清理（仅当当前任务仍是自己时）
	defer func() {
		if val, ok := activeTasks.Load(id); ok {
			if val.(collectTask).reqId == reqId {
				activeTasks.Delete(id)
				updateCollectProgress(id, func(progress *model.CollectProgress) {
					if retErr != nil {
						progress.Status = progressStatusFailed
						return
					}
				})
				if retErr != nil {
					noteSourceError(id, retErr.Error())
				}
				log.Printf("[Spider] 站点 %s 任务结束\n", id)
			}
		}
	}()

	log.Printf("[Spider] 站点 %s 任务启动 (reqId: %s)\n", id, reqId)
	ensureCollectProgress(id, s.Name)

	// 生成 RequestInfo
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	// 如果 h == 0 则直接返回错误信息
	if h == 0 {
		return errors.New("采集时长不能为 0")
	}
	// 如果 h = -1 则进行全量采集
	if h > 0 {
		r.Params.Set("h", fmt.Sprint(h))
	}
	// 2. 首先获取分页采集的页数
	pageCount, err := getPageCountWithRetry(ctx, s, r)
	if err != nil {
		return err
	}
	// pageCount = 0：时段内无新数据，无分页可跑。
	// 单站：page_done，由 defer 立即 finalizing→done；
	// 批量：waiting_publish，与有数据的源一样等整批 flushPending，列表状态与生命周期一致。
	if pageCount <= 0 {
		updateCollectProgress(id, func(progress *model.CollectProgress) {
			progress.Total = 0
			progress.Current = 0
			progress.Success = 0
			progress.Failed = 0
			if flushAtEnd {
				progress.Status = progressStatusPageDone
			} else {
				progress.Status = progressStatusWaitingPublish
			}
		})
		log.Printf("[Spider] 站点 %s 无需分页 (pageCount=%d，该时间段无新内容) flushAtEnd=%v\n", s.Name, pageCount, flushAtEnd)
		return nil
	}
	updateCollectProgress(id, func(progress *model.CollectProgress) {
		progress.Total = pageCount
		progress.Current = 0
		progress.Success = 0
		progress.Failed = 0
		progress.Status = progressStatusRunning
	})
	log.Printf("[Spider] 站点 %s 共 %d 页，开始采集...\n", s.Name, pageCount)

	pageWorkerLimit := getSourcePageConcurrency(s)
	hadWrites, err = collectFilmPages(ctx, pageCount, pageWorkerLimit, s, h, flushAtEnd, batch)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		log.Printf("[Spider] 站点 %s 已停止接收新分页，等待收尾刷新\n", s.Name)
	} else {
		// 分页阶段完成：单站 → page_done；批量 → waiting_publish（defer 中也会兜底）。
		markSourcePagesFinished(id, flushAtEnd)
	}
	return nil
}

// ensureMasterCategoriesReady 在主站影片采集前确保本地分类与映射可用。
// 仅在分类表为空时触发，避免覆盖用户已调整的业务分类属性。
func ensureMasterCategoriesReady(s *model.FilmSource) error {
	if s == nil || s.Grade != model.MasterCollect {
		return nil
	}
	if repository.ExistsCategoryTree() {
		return nil
	}
	log.Printf("[Spider] 分类为空，采集前自动同步主站分类: name=%s id=%s uri=%s", s.Name, s.Id, s.Uri)
	if err := CollectCategory(s); err != nil {
		return fmt.Errorf("采集前同步主站分类失败: %w", err)
	}
	log.Printf("[Spider] 采集前主站分类同步完成: name=%s id=%s", s.Name, s.Id)
	return nil
}

// CollectCategory 影视分类采集
func CollectCategory(s *model.FilmSource) error {
	return collectCategoryWithMode(s, true)
}

func ResetCategory(s *model.FilmSource) error {
	return collectCategoryWithMode(s, false)
}

func collectCategoryWithMode(s *model.FilmSource, preserveBusinessFields bool) error {
	if s == nil {
		return errors.New("采集站信息不存在")
	}
	// 硬约束：分类树只能来自主站，禁止附属站写入分类树
	if s.Grade != model.MasterCollect {
		return fmt.Errorf("分类树只能从主采集站同步，当前站 grade=%d name=%s", s.Grade, s.Name)
	}
	// 获取分类树形数据
	categoryTree, err := spiderCore.GetCategoryTree(utils.RequestInfo{Uri: s.Uri, Params: url.Values{}})
	if err != nil {
		return fmt.Errorf("获取主站分类树失败: %w", err)
	}
	// 保存 tree 到 MySQL (方案B: 传入 sourceId 建立映射)
	if preserveBusinessFields {
		err = repository.SaveCategoryTree(s.Id, categoryTree)
	} else {
		err = repository.ResetCategoryTree(s.Id, categoryTree)
	}
	if err != nil {
		return fmt.Errorf("保存主站分类树失败: %w", err)
	}
	return nil
}


func saveSlavePlaylists(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) ([]int64, error) {
	lock := getSourceWriteLock(s.Id)
	lock.Lock()
	defer lock.Unlock()
	var changedMids []int64
	err := runCollectDBWriteWithRetry(ctx, s.Name, page, func() error {
		mids, err := filmrepo.SaveSitePlayList(s.Id, list)
		if err != nil {
			return err
		}
		changedMids = mids
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("save slave playlists failed: %w", err)
	}
	return changedMids, nil
}

// saveCollectedFilm 将已采集的 list 按站点类型写入存储，消除 collectFilm/collectFilmById 中的重复 switch 块。
// saveMaster 由调用方注入，区分批量(SaveDetails)与单条(SaveDetail)两种写入策略。
func saveCollectedFilm(s *model.FilmSource, list []model.MovieDetail, saveMaster func(string, []model.MovieDetail) error) error {
	switch s.Grade {
	case model.MasterCollect:
		lock := getSourceWriteLock(s.Id)
		lock.Lock()
		defer lock.Unlock()
		if err := saveMaster(s.Id, list); err != nil {
			return fmt.Errorf("save master details failed: %w", err)
		}
		return nil
	case model.SlaveCollect:
		_, err := saveSlavePlaylists(context.Background(), s, 0, list)
		return err
	}
	return nil
}

// collectWriteMids 采集写结果。
// Notify：进 Telegram「更新列表」——主站新片/任一线路最后一项分集标签变化，
// 或附属站已匹配主站片的 playlist 最后一项分集标签变化（含新增/回退）；
// Affected：快照/缓存收尾用 mid。
type collectWriteMids struct {
	Notify   []int64
	Affected []int64
}

func saveCollectedFilmForCollect(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) (collectWriteMids, error) {
	if s.Grade != model.MasterCollect {
		// 附属站：扩展主站播放源；最后一项分集标签变化且已匹配主站 mid 的，进更新列表（仅链接/中间集变化不进）
		mids, err := saveSlavePlaylists(ctx, s, page, list)
		if err != nil {
			return collectWriteMids{}, err
		}
		return collectWriteMids{Notify: mids, Affected: mids}, nil
	}

	var result filmrepo.CollectWriteResult
	lock := getSourceWriteLock(s.Id)
	lock.Lock()
	defer lock.Unlock()
	err := runCollectDBWriteWithRetry(ctx, s.Name, page, func() error {
		r, err := filmrepo.SaveDetailsForCollect(s.Id, list)
		if err != nil {
			return err
		}
		result = r
		return nil
	})
	if err != nil {
		return collectWriteMids{}, fmt.Errorf("save master details failed: %w", err)
	}
	return collectWriteMids{Notify: result.NotifyMIDs, Affected: result.AffectedMIDs}, nil
}

func saveFilmPageFailure(s *model.FilmSource, h, pg int, phase string, err error) {
	if err == nil {
		err = errors.New("unknown error")
	}
	recordErr := repository.SaveFailureRecord(model.FailureRecord{
		OriginId:   s.Id,
		OriginName: s.Name,
		Uri:        s.Uri,
		PageNumber: pg,
		Hour:       h,
		Cause:      fmt.Sprintf("%s: %v", phase, err),
		Status:     model.FailureRecordStatusPending,
	})
	if recordErr != nil {
		syslog.Errorf("[Spider][Failure] 失败页记录保存失败 source_id=%s source=%s page=%d hour=%d phase=%s err=%v record_err=%v", s.Id, s.Name, pg, h, phase, err, recordErr)
	}
}

type collectPageStats struct {
	latestPage int
	success    int
	failed     int
}

func shouldLogCollectProgress(done, total int) bool {
	return done == total || done%collectProgressLogPageStep == 0
}

func shouldLogCollectFailure(failed int) bool {
	return failed == 1 || failed%collectFailureLogStep == 0
}

func buildPageRequest(s *model.FilmSource, h, pg int) utils.RequestInfo {
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("pg", fmt.Sprint(pg))
	if h > 0 {
		r.Params.Set("h", fmt.Sprint(h))
	}
	return r
}

// collectFilmPages 将请求与写库拆成流水线：请求并发执行，写库交给全局调度器按批次串行落库。
// flushAtEnd=true 为单站采集（独立批次+即时告警）；false 为批量采集（失败并入整批概要）。
func collectFilmPages(parentCtx context.Context, pageCount int, requestWorkerLimit int, s *model.FilmSource, h int, flushAtEnd bool, batch *notify.ChangeBatch) (bool, error) {
	if pageCount <= 0 {
		return false, nil
	}
	ctx, cancel := context.WithCancel(parentCtx)
	defer cancel()
	if requestWorkerLimit <= 0 {
		requestWorkerLimit = 1
	}
	requestWorkers := min(pageCount, requestWorkerLimit)

	pages := make(chan int, pageCount)
	writeCompletions := make(chan collectWriteCompletion, pageCount)
	for pg := 1; pg <= pageCount; pg++ {
		pages <- pg
	}
	close(pages)

	var writeWG sync.WaitGroup
	var consecutiveFailuresMu sync.Mutex
	consecutiveFailures := 0
	var stopErr error
	var stopOnce sync.Once
	var statsMu sync.Mutex
	stats := collectPageStats{}
	lastLoggedDone := 0
	logProgress := func(force bool) {
		statsMu.Lock()
		snapshot := stats
		done := snapshot.success + snapshot.failed
		if !force {
			if done == lastLoggedDone || !shouldLogCollectProgress(done, pageCount) {
				statsMu.Unlock()
				return
			}
		}
		lastLoggedDone = done
		statsMu.Unlock()

		log.Printf("[Spider] 站点 %s 采集进度 完成=%d/%d，成功=%d，失败=%d，最新页=%d", s.Name, done, pageCount, snapshot.success, snapshot.failed, snapshot.latestPage)
	}
	recordPageFinished := func(page int, success bool) collectPageStats {
		statsMu.Lock()
		if page > stats.latestPage {
			stats.latestPage = page
		}
		if success {
			stats.success++
		} else {
			stats.failed++
		}
		snapshot := stats
		statsMu.Unlock()
		return snapshot
	}
	recordFailure := func(page int, stage string, err error) {
		consecutiveFailuresMu.Lock()
		consecutiveFailures++
		currentFailures := consecutiveFailures
		consecutiveFailuresMu.Unlock()

		if currentFailures < collectSourceConsecutiveFailureLimit {
			return
		}
		stopOnce.Do(func() {
			stopErr = fmt.Errorf("站点 %s 连续采集失败 %d 次，已终止本次采集", s.Name, collectSourceConsecutiveFailureLimit)
			syslog.Errorf("[Spider] 站点 %s 连续失败达到阈值，终止采集 page=%d stage=%s err=%v", s.Name, page, stage, err)
			updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
				progress.Status = progressStatusFailed
			})
			// 批量采集且批次概要开启时，失败并入整批概要，避免逐源即时告警轰炸；
			// 单站采集或概要事件关闭时保持即时告警。
			if flushAtEnd || !notify.IsEventEnabled(model.NotifyEventCollectBatchSummary) {
				emitSourceFailedNotify(s.Id, s.Name, stopErr.Error())
			} else {
				noteSourceError(s.Id, stopErr.Error())
			}
			cancel()
		})
	}
	recordSuccess := func() {
		consecutiveFailuresMu.Lock()
		consecutiveFailures = 0
		consecutiveFailuresMu.Unlock()
	}
	markStopped := func() {
		markProgressStopped(s.Id)
	}
	// maybeMarkPagePhaseDone 最后一页完成后立刻切到 page_done，
	// 避免 success+failed 已满仍长期显示「采集中」。
	maybeMarkPagePhaseDone := func(progress *model.CollectProgress, snapshot collectPageStats) {
		if progress.Status != progressStatusRunning && progress.Status != progressStatusStarting {
			return
		}
		if progress.Total > 0 && snapshot.success+snapshot.failed >= progress.Total {
			progress.Status = progressStatusPageDone
		}
	}
	recordPageFailure := func(page int, stage string, err error) {
		snapshot := recordPageFinished(page, false)
		updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
			progress.Failed = snapshot.failed
			if page > progress.Current {
				progress.Current = page
			}
			maybeMarkPagePhaseDone(progress, snapshot)
		})
		saveFilmPageFailure(s, h, page, stage, err)
		if shouldLogCollectFailure(snapshot.failed) {
			syslog.Warnf("[Spider] 站点 %s 采集失败累计=%d，最近失败 page=%d stage=%s err=%v", s.Name, snapshot.failed, page, stage, err)
		}
		recordFailure(page, stage, err)
		logProgress(false)
	}
	recordPageSuccess := func(page int, notifyMIDs, affectedMIDs []int64) {
		snapshot := recordPageFinished(page, true)
		recordSuccess()
		// 更新列表记 Notify mid；快照/缓存收尾用 Affected
		noteCollectedMIDs(batch, s.Id, s.Name, notifyMIDs)
		if s.Grade == model.MasterCollect && h < 0 {
			collectLifecycle.addPendingMasterMIDs(s.Id, affectedMIDs)
		} else if s.Grade == model.MasterCollect {
			collectLifecycle.addMasterAffectedMIDs(affectedMIDs)
		} else {
			collectLifecycle.addAffectedMIDs(affectedMIDs)
		}
		updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
			progress.Success = snapshot.success
			progress.Failed = snapshot.failed
			if page > progress.Current {
				progress.Current = page
			}
			maybeMarkPagePhaseDone(progress, snapshot)
		})
		logProgress(false)
	}

	var requestWG sync.WaitGroup
	requestWG.Add(requestWorkers)
	for i := 0; i < requestWorkers; i++ {
		go func() {
			defer requestWG.Done()
			for {
				select {
				case <-ctx.Done():
					markStopped()
					return
				case pg, ok := <-pages:
					if !ok {
						return
					}
					updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
						if pg > progress.Current {
							progress.Current = pg
						}
						progress.Status = progressStatusRunning
					})
					list, err := getFilmDetailWithRetry(ctx, s, buildPageRequest(s, h, pg))
					if err == nil && len(list) == 0 {
						err = errors.New("response list is empty")
					}
					if err != nil {
						if ctx.Err() != nil {
							markStopped()
							return
						}
						recordPageFailure(pg, "fetch", err)
						continue
					}
					page := pg
					items := list
					writeWG.Add(1)
					// 使用采集 ctx：写库 buffer 反压等待时可被停止信号取消。
					submitErr := collectWrites.submit(ctx, collectWriteJob{
						sourceID:   s.Id,
						sourceName: s.Name,
						grade:      s.Grade,
						page:       page,
						write: func() (collectWriteMids, error) {
							return saveCollectedFilmForCollect(context.Background(), s, page, items)
						},
						complete: func(completion collectWriteCompletion) {
							writeCompletions <- completion
							writeWG.Done()
						},
					})
					if submitErr != nil {
						writeWG.Done()
						if ctx.Err() != nil {
							markStopped()
							return
						}
						recordPageFailure(page, "enqueue", submitErr)
						continue
					}
				}
			}
		}()
	}
	go func() {
		requestWG.Wait()
		collectWrites.finishSource(s.Grade, s.Id)
		writeWG.Wait()
		close(writeCompletions)
	}()

	for completion := range writeCompletions {
		if completion.err != nil {
			recordPageFailure(completion.page, completion.stage, completion.err)
			continue
		}
		recordPageSuccess(completion.page, completion.notifyMIDs, completion.affectedMIDs)
	}
	logProgress(true)
	if ctx.Err() != nil {
		log.Printf("[Spider] 站点 %s 并发采集任务已中断，worker 已全部退出\n", s.Name)
	}
	if stopErr != nil {
		return stats.success > 0, stopErr
	}
	if s.Grade == model.MasterCollect && h < 0 && stats.failed > 0 {
		return stats.success > 0, fmt.Errorf("主站全量采集存在失败页 failed=%d，跳过本次框架发布", stats.failed)
	}
	return stats.success > 0, nil
}

func shouldSkipCollectPublishOnError(source model.FilmSource, h int) bool {
	return source.Grade == model.MasterCollect && h < 0
}

// collectFilmById 采集指定ID的影片信息，返回实质变更的全局 mid（无变更则空）。
func collectFilmById(ids string, s *model.FilmSource, flushAtEnd bool) (changedMids []int64, retErr error) {
	if s == nil {
		return nil, errors.New("采集站信息不存在")
	}
	if err := collectLifecycle.beginSource(s.Id); err != nil {
		log.Printf("[Spider] 站点 %s 无法启动单片采集: %v\n", s.Id, err)
		return nil, err
	}
	defer func() {
		if flushAtEnd {
			flushErr := collectLifecycle.finishSourceAndFlush(*s)
			if retErr == nil && flushErr != nil {
				retErr = flushErr
			}
			return
		}
		collectLifecycle.endSource(s.Id)
	}()

	release, err := waitSourceRequestTurn(context.Background(), s, fmt.Sprintf("单片请求 ids=%s ", ids))
	if err != nil {
		return nil, err
	}
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("pg", "1")
	r.Params.Set("ids", ids)
	list, err := spiderCore.GetFilmDetail(r)
	if err != nil {
		release(err)
		return nil, fmt.Errorf("get movie detail failed: %w", err)
	}
	if len(list) <= 0 {
		release(errors.New("response list is empty"))
		return nil, errors.New("get movie detail failed: response list is empty")
	}
	release(nil)

	// 与批量路径一致：Notify 供更新列表；Affected 供收尾
	written, err := saveCollectedFilmForCollect(context.Background(), s, 1, list)
	if err != nil {
		return nil, err
	}
	if s.Grade == model.MasterCollect {
		collectLifecycle.addMasterAffectedMIDs(written.Affected)
	} else {
		collectLifecycle.addAffectedMIDs(written.Affected)
	}
	return written.Notify, nil
}

// PrepareBatchCollectStart 同步过滤可采站点并标记 starting（0%），供接口立即返回后列表可见进度。
// 返回已标记的站点列表；调用方应在 goroutine 中执行 BatchCollectPrepared。
func PrepareBatchCollectStart(ids []string) ([]model.FilmSource, error) {
	sources := make([]model.FilmSource, 0, len(ids))
	for _, id := range ids {
		if fs := repository.FindCollectSourceById(id); fs != nil && fs.State {
			sources = append(sources, *fs)
		}
	}
	sources = filterCollectableSources(sources, "Batch-Collect")
	if len(sources) == 0 {
		return nil, fmt.Errorf("没有可启动的采集站（均未启用或已在采集中）")
	}
	markSourcesCollectStarting(sources)
	return sources, nil
}

// BatchCollectPrepared 对已 Prepare 的站点执行批量采集（不再二次 filter/mark）。
func BatchCollectPrepared(trigger string, h int, sources []model.FilmSource) {
	if len(sources) == 0 {
		return
	}
	if trigger == "" {
		trigger = model.NotifyTriggerManual
	}
	runSourcesWithLimitCore(sources, h, "Batch-Collect", trigger)
}

// BatchCollect 批量采集, 采集指定的所有站点最近x小时内更新的数据
func BatchCollect(h int, ids ...string) {
	BatchCollectTriggered(model.NotifyTriggerManual, h, ids...)
}

// BatchCollectTriggered 带通知 trigger 的批量采集。
func BatchCollectTriggered(trigger string, h int, ids ...string) {
	sources := make([]model.FilmSource, 0)
	for _, id := range ids {
		if fs := repository.FindCollectSourceById(id); fs != nil && fs.State {
			sources = append(sources, *fs)
		}
	}

	if len(sources) == 0 {
		return
	}
	if trigger == "" {
		trigger = model.NotifyTriggerManual
	}
	runSourcesWithLimit(sources, h, "Batch-Collect", trigger)
}

func filterEnabledSources(sources []model.FilmSource) []model.FilmSource {
	enabled := make([]model.FilmSource, 0, len(sources))
	for _, s := range sources {
		if s.State {
			enabled = append(enabled, s)
		}
	}
	return enabled
}

func getEnabledSourcesByGrade(grade model.SourceGrade) []model.FilmSource {
	return filterEnabledSources(repository.GetCollectSourceListByGrade(grade))
}

// AutoCollect 自动进行对所有已启用站点的采集任务
func AutoCollect(h int) {
	AutoCollectTriggered(model.NotifyTriggerManual, h)
}

// AutoCollectTriggered 带通知 trigger 的自动采集。
func AutoCollectTriggered(trigger string, h int) {
	enabled := filterEnabledSources(repository.GetCollectSourceList())
	if len(enabled) == 0 {
		log.Println("[Spider] 自动采集：未找到任何启用的站点")
		return
	}
	if trigger == "" {
		trigger = model.NotifyTriggerManual
	}
	runSourcesWithLimit(enabled, h, "Auto-Collect", trigger)
}

// ClearSpider 删除所有已采集的影片信息，并与采集写库互斥。
func ClearSpider() error {
	filmrepo.ReportResetProgress(5, "正在停止采集任务")
	StopAllTasks()
	if err := collectLifecycle.waitIdle(time.Second * 30); err != nil {
		return err
	}
	filmrepo.ReportResetProgress(15, "正在清空数据")
	return collectLifecycle.runExclusive(func() error {
		// 清空采集统计合并缓冲，防止清表后内存 pending 的旧统计回写复活
		repository.ResetCollectStatsCoalescer()
		return filmrepo.FilmZero()
	})
}

// CollectSingleFilm 通过全局影片 ID 更新所有启用站点的对应影片。
// 主站会优先使用自身映射，附属站则通过 MovieSourceMapping 取回各自的 source_mid。
func CollectSingleFilm(ids string) {
	globalMid, err := strconv.ParseInt(strings.TrimSpace(ids), 10, 64)
	if err != nil || globalMid <= 0 {
		log.Printf("[Spider] CollectSingleFilm: 非法影片 ID %q\n", ids)
		return
	}

	enabled := filterEnabledSources(repository.GetCollectSourceList())
	if len(enabled) == 0 {
		log.Println("[Spider] CollectSingleFilm: 未找到任何启用的站点")
		return
	}

	startedAt := time.Now()
	batch := notify.StartChangeBatch()
	type singleResult struct {
		source model.FilmSource
		err    error
	}
	var (
		mu      sync.Mutex
		results []singleResult
	)
	var wg sync.WaitGroup
	for _, source := range enabled {
		requestID := resolveSingleCollectSourceMid(globalMid, source)
		if requestID == "" {
			continue
		}

		wg.Add(1)
		go func(src model.FilmSource, sourceMid string) {
			defer wg.Done()
			mids, err := collectFilmById(sourceMid, &src, false)
			if err != nil {
				syslog.Errorf("[Spider] CollectSingleFilm 站点 %s 更新失败: %v", src.Name, err)
			} else if len(mids) > 0 {
				// 主站结构变更/新片，或附属站已匹配主站片的播放源结构变更
				noteCollectedMIDs(batch, src.Id, src.Name, mids)
			}
			mu.Lock()
			results = append(results, singleResult{source: src, err: err})
			mu.Unlock()
		}(source, requestID)
	}
	wg.Wait()

	// 收尾发布（不经 emitBatchSummary，避免把未尝试的 enabled 源默认成成功）
	attempted := make([]model.FilmSource, 0, len(results))
	for _, r := range results {
		attempted = append(attempted, r.source)
	}
	var finalizeErr error
	if len(attempted) > 0 {
		// 复用 flush 但不发摘要：先 flush 再手动发
		flushMap := make(map[string]model.FilmSource, len(attempted))
		for _, s := range attempted {
			flushMap[s.Id] = s
		}
		if err := collectLifecycle.runFlush(func(affectedMIDs []int64, masterMIDs []int64) error {
			_, _, err := flushPendingSources(flushMap, affectedMIDs, masterMIDs)
			return err
		}); err != nil {
			syslog.Errorf("[CollectSingleFilm] 收尾刷新失败: %v", err)
			finalizeErr = err
		}
	}

	notifyResults := make([]model.SourceNotifyResult, 0, len(results))
	for _, r := range results {
		status := progressStatusDone
		errMsg := ""
		if r.err != nil {
			status = progressStatusFailed
			errMsg = r.err.Error()
		} else if finalizeErr != nil {
			// 写库成功但收尾失败
			status = progressStatusFailed
			errMsg = finalizeErr.Error()
		}
		notifyResults = append(notifyResults, notify.BuildSourceResultDirect(r.source, status, errMsg))
	}
	if len(notifyResults) == 0 {
		// 全部源映射不到：发一条失败摘要便于感知
		notifyResults = append(notifyResults, model.SourceNotifyResult{
			SourceName: "单片更新",
			Status:     progressStatusFailed,
			Error:      fmt.Sprintf("影片 #%d 未匹配到任何启用站点的 source_mid", globalMid),
			FailedCnt:  1,
		})
	}
	emitBatchSummaryDirect(batch, model.NotifyTriggerSingleUpdate, notifyResults, startedAt, finalizeErr)
}

func resolveSingleCollectSourceMid(globalMid int64, source model.FilmSource) string {
	if globalMid <= 0 {
		return ""
	}
	sourceMid := filmrepo.LoadSourceMidByGlobalMid(globalMid, source.Id)
	if sourceMid > 0 {
		return strconv.FormatInt(sourceMid, 10)
	}
	if source.Grade == model.MasterCollect {
		return strconv.FormatInt(globalMid, 10)
	}
	return ""
}

// recoverFilmPage 重试单条失败页：成功后出队，失败未达上限时继续保留待重试。
func recoverFilmPage(ctx context.Context, s *model.FilmSource, fr *model.FailureRecord, batch *notify.ChangeBatch) {
	if s == nil || fr == nil {
		return
	}
	select {
	case <-ctx.Done():
		return
	default:
	}
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("pg", fmt.Sprint(fr.PageNumber))
	if fr.Hour > 0 {
		r.Params.Set("h", fmt.Sprint(fr.Hour))
	}

	list, err := getFilmDetailWithRetry(ctx, s, r)
	if err != nil || len(list) <= 0 {
		markRecoverFailure(s, fr, "recover_fetch", err)
		log.Println("Recover GetMovieDetail Error: ", err)
		return
	}

	written, err := saveCollectedFilmForCollect(ctx, s, fr.PageNumber, list)
	if err != nil {
		markRecoverFailure(s, fr, "recover_save", err)
		log.Println("Recover saveCollectedFilm Error: ", err)
		return
	}
	noteCollectedMIDs(batch, s.Id, s.Name, written.Notify)
	if s.Grade == model.MasterCollect {
		collectLifecycle.addMasterAffectedMIDs(written.Affected)
	} else {
		collectLifecycle.addAffectedMIDs(written.Affected)
	}
	repository.UpdateFailureRecordStatus(fr, model.FailureRecordStatusSuccess)
}

func markRecoverFailure(s *model.FilmSource, fr *model.FailureRecord, phase string, err error) {
	if err == nil {
		err = errors.New("unknown error")
	}
	cause := fmt.Sprintf("%s: %v", phase, err)
	final, retryCount, updateErr := repository.MarkFailureRecordRetryFailed(fr, cause, recoverMaxRetryCount)
	if updateErr != nil {
		syslog.Errorf("[Spider][Recover] 失败记录更新失败 source_id=%s source=%s page=%d hour=%d err=%v", s.Id, s.Name, fr.PageNumber, fr.Hour, updateErr)
		return
	}
	if final {
		syslog.Warnf("[Spider][Recover] 失败页达到最大自动重试次数 source_id=%s source=%s page=%d hour=%d retry_count=%d", s.Id, s.Name, fr.PageNumber, fr.Hour, retryCount)
	}
}

// ======================================================= 采集拓展内容  =======================================================

// SingleRecoverSpider 二次采集
func SingleRecoverSpider(fr *model.FailureRecord) {
	// 仅对当前失败记录所属站点+失败页进行重试，不干扰正在运行的采集任务
	s := repository.FindCollectSourceById(fr.OriginId)
	if s == nil {
		syslog.Errorf("[Spider] 重试失败: 站点 %s 不存在", fr.OriginId)
		return
	}
	startedAt := time.Now()
	batch := notify.StartChangeBatch()
	if err := collectLifecycle.waitAndBeginSource(s.Id); err != nil {
		syslog.Errorf("[Spider] 站点 %s 无法启动失败页重试: %v", s.Id, err)
		// 概要事件开启时由概要承载失败信息，避免与即时告警双发；关闭时退回即时告警
		if notify.IsEventEnabled(model.NotifyEventCollectBatchSummary) {
			emitBatchSummaryDirect(batch, model.NotifyTriggerRecover, []model.SourceNotifyResult{
				notify.BuildSourceResultDirect(*s, progressStatusFailed, err.Error()),
			}, startedAt, err)
		} else {
			emitSourceFailedNotify(s.Id, s.Name, err.Error())
		}
		return
	}
	var finalizeErr error
	defer func() {
		if err := collectLifecycle.finishSourceAndFlush(*s); err != nil {
			syslog.Errorf("[Spider] 站点 %s 失败页重试收尾刷新失败: %v", s.Id, err)
			finalizeErr = err
		}
		emitBatchSummaryForSources(batch, model.NotifyTriggerRecover, []model.FilmSource{*s}, startedAt, finalizeErr)
	}()
	recoverFilmPage(context.Background(), s, fr, batch)
}

// FullRecoverSpider 扫描记录表中的失败记录, 并发重试各失败页
func FullRecoverSpider() {
	list := repository.PendingRecord()
	sourcesToFlush := make([]model.FilmSource, 0, len(list))
	seen := make(map[string]struct{}, len(list))
	recordsBySource := make(map[string][]model.FailureRecord, len(list))
	sourceByID := make(map[string]model.FilmSource, len(list))
	limit := config.CollectPageWorkers
	if limit <= 0 {
		limit = config.DefaultCollectPageWorkers
	}
	if limit <= 0 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	startedAt := time.Now()
	// 恢复重试同样开变更批次，避免失败页变更 mid 丢失
	batch := notify.StartChangeBatch()
	for i := range list {
		fr := list[i]
		s := repository.FindCollectSourceById(fr.OriginId)
		if s == nil {
			syslog.Errorf("[Spider] 重试失败: 站点 %s 不存在", fr.OriginId)
			continue
		}
		if _, ok := seen[s.Id]; !ok {
			seen[s.Id] = struct{}{}
			sourcesToFlush = append(sourcesToFlush, *s)
			sourceByID[s.Id] = *s
		}
		recordsBySource[s.Id] = append(recordsBySource[s.Id], fr)
	}
	for sourceID, records := range recordsBySource {
		src, ok := sourceByID[sourceID]
		if !ok {
			continue
		}
		recordsCopy := append([]model.FailureRecord(nil), records...)
		wg.Add(1)
		sem <- struct{}{}
		go func(source model.FilmSource, pending []model.FailureRecord) {
			defer wg.Done()
			defer func() { <-sem }()
			if err := collectLifecycle.waitAndBeginSource(source.Id); err != nil {
				syslog.Errorf("[Spider] 站点 %s 无法启动失败页重试: %v", source.Id, err)
				return
			}
			defer collectLifecycle.endSource(source.Id)
			for i := range pending {
				record := pending[i]
				recoverFilmPage(context.Background(), &source, &record, batch)
			}
		}(src, recordsCopy)
	}
	wg.Wait()
	flushSourcesPending("FullRecoverSpider", model.NotifyTriggerRecover, sourcesToFlush, startedAt, batch)
}

// ======================================================= 公共方法  =======================================================

// CollectApiTest 测试采集接口是否可用
func CollectApiTest(s model.FilmSource) error {
	return CollectApiTestWithTimeout(s, 0)
}

// CollectApiTestWithTimeout 测试采集接口是否可用；timeoutSeconds > 0 时使用指定请求超时（秒）。
func CollectApiTestWithTimeout(s model.FilmSource, timeoutSeconds int) error {
	// 使用 ac=list 测试：获取分类列表，所有标准 Mac CMS 站均支持，
	// 且不需要额外过滤参数（ac=detail 在无 h/t 参数时部分站点会返回 400）
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("ac", "list")
	r.Params.Set("pg", "1")
	if timeoutSeconds > 0 {
		r.Header = map[string][]string{"timeout": {strconv.Itoa(timeoutSeconds)}}
	}
	err := utils.ApiTest(&r)
	// 首先核对接口返回值类型
	if err == nil {
		lp := model.FilmListPage{}
		if err = json.Unmarshal(r.Resp, &lp); err != nil {
			return errors.New(fmt.Sprint("测试失败, 返回数据异常, JSON序列化失败: ", err))
		}
		return nil
	}
	return errors.New(fmt.Sprint("测试失败, 请求响应异常 : ", err.Error()))
}

// GetActiveTasks 返回当前正在采集的任务 ID 列表
func GetActiveTasks() []string {
	ids := make([]string, 0)
	activeTasks.Range(func(key, value any) bool {
		ids = append(ids, key.(string))
		return true
	})
	return ids
}

func GetActiveTaskProgress() []model.CollectProgress {
	startCollectProgressHousekeeping()

	list := make([]model.CollectProgress, 0)
	seen := make(map[string]struct{})
	staleAfter := progressStaleDuration()
	now := time.Now()

	activeTasks.Range(func(key, value any) bool {
		id := key.(string)
		seen[id] = struct{}{}
		if progress, ok := collectProgressSnapshot(id); ok {
			list = append(list, progress)
			return true
		}
		list = append(list, model.CollectProgress{Id: id, Status: progressStatusRunning})
		return true
	})
	collectProgress.Range(func(key, value any) bool {
		id := key.(string)
		if _, ok := seen[id]; ok {
			return true
		}
		state := value.(*collectProgressState)
		state.mu.Lock()
		progress := state.data
		age := now.Sub(state.updated)

		// 无 live 的 starting/running 超时 → failed；waiting_publish 等收尾态不超时。
		if isActiveCollectStatus(progress.Status) {
			_, live := activeTasks.Load(id)
			if shouldMarkProgressStale(progress.Status, live, age, staleAfter) {
				old := progress.Status
				name := progress.Name
				progress.Status = progressStatusFailed
				state.data.Status = progressStatusFailed
				state.updated = now
				log.Printf("[Spider] 进度超时清理 source=%s status=%s age=%s -> failed", id, old, age.Round(time.Second))
				state.mu.Unlock()
				emitProgressStaleNotify(id, name, old, age)
				list = append(list, progress)
				return true
			}
			state.mu.Unlock()
			list = append(list, progress)
			return true
		}

		// 终态：有活跃采集时一律保留；全部结束后由 maybePurge 统一清空。
		if isTerminalCollectStatus(progress.Status) {
			state.mu.Unlock()
			list = append(list, progress)
			return true
		}
		state.mu.Unlock()
		return true
	})

	// 列表读取时也尝试统一清理，避免仅依赖后台 ticker。
	maybePurgeTerminalProgressTogether(now)
	// purge 后剔除 map 中已不存在的终态快照
	if n := len(list); n > 0 {
		filtered := list[:0]
		for _, p := range list {
			if isTerminalCollectStatus(p.Status) {
				if _, still := collectProgress.Load(p.Id); !still {
					continue
				}
			}
			filtered = append(filtered, p)
		}
		list = filtered
	}

	return list
}

// StopAllTasks 强制停止当前系统中所有正在进行的采集任务
func StopAllTasks() {
	stopAllVersion.Add(1)
	count := 0
	collectProgress.Range(func(key, value any) bool {
		state := value.(*collectProgressState)
		state.mu.Lock()
		// 仅中断仍在拉页/排队的任务；已 page_done / waiting_publish 的交给收尾发布。
		if state.data.Status == progressStatusStarting || state.data.Status == progressStatusRunning {
			state.data.Status = progressStatusStopped
			state.updated = time.Now()
		}
		state.mu.Unlock()
		return true
	})
	activeTasks.Range(func(key, value any) bool {
		if ct, ok := value.(collectTask); ok {
			ct.cancel()
			count++
		}
		if id, ok := key.(string); ok {
			markProgressStopped(id)
		}
		return true
	})
	if count > 0 {
		log.Printf("[Spider] 已强制停止 %d 个活跃采集任务\n", count)
		go finalizeStoppedCollectTasks()
	}
}

func finalizeStoppedCollectTasks() {
	if err := collectLifecycle.flushPending(); err != nil {
		syslog.Errorf("[Spider] 终止采集后收尾刷新失败: %v", err)
	}
}

// IsTaskRunning 查询指定站点的采集任务是否仍处于活跃生命周期（含等待收尾）。
func IsTaskRunning(id string) bool {
	if _, ok := activeTasks.Load(id); ok {
		return true
	}
	if progress, ok := collectProgressSnapshot(id); ok {
		return isActiveCollectStatus(progress.Status)
	}
	return false
}

// IsAnyTaskRunning 查询系统中是否有任何采集任务正在进行（含等待收尾）。
func IsAnyTaskRunning() bool {
	found := false
	activeTasks.Range(func(key, value any) bool {
		found = true
		return false
	})
	if found {
		return true
	}
	hasActive := false
	collectProgress.Range(func(key, value any) bool {
		state := value.(*collectProgressState)
		state.mu.RLock()
		active := isActiveCollectStatus(state.data.Status)
		state.mu.RUnlock()
		if active {
			hasActive = true
			return false
		}
		return true
	})
	return hasActive
}
