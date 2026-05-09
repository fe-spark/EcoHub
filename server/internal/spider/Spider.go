package spider

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/url"
	"sort"
	"strconv"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"github.com/go-sql-driver/mysql"

	"server/internal/config"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	"server/internal/spider/conver"
	"server/internal/utils"
)

/*
	采集逻辑 v3

*/

var spiderCore = &JsonCollect{}

const (
	pageCountRetryTimes   = 2
	filmDetailRetryTimes  = 2
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

var pictureSyncRunning atomic.Bool

var asyncMasterSearchTagsMu sync.Mutex

// taskMu 保护同一站点 cancel+Store 的原子性，防止并发截停竞态
var taskMu sync.Mutex

var collectLifecycle = newCollectLifecycle()

// requestGates 按站点控制外部采集接口请求间隔：同一站点两次请求之间至少等待站点 Interval。
var requestGates sync.Map

type sourceRequestGate struct {
	mu            sync.Mutex
	nextAllowedAt time.Time
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
	state := &collectProgressState{data: model.CollectProgress{Id: sourceID, Name: name, Status: "starting"}, updated: time.Now()}
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

func isCollectProgressStopped(sourceID string) bool {
	if progress, ok := collectProgressSnapshot(sourceID); ok {
		return progress.Status == "stopped"
	}
	return false
}

func isCollectProgressStarting(sourceID string) bool {
	if progress, ok := collectProgressSnapshot(sourceID); ok {
		return progress.Status == "starting"
	}
	return false
}

func isCollectAlreadyQueuedOrRunning(sourceID string) bool {
	if _, ok := activeTasks.Load(sourceID); ok {
		return true
	}
	return isCollectProgressStarting(sourceID)
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
		state.data.Status = "starting"
		state.updated = time.Now()
		state.mu.Unlock()
	}
}

func StopTask(sourceID string) {
	updateCollectProgress(sourceID, func(progress *model.CollectProgress) {
		if progress.Status == "starting" || progress.Status == "running" {
			progress.Status = "stopped"
		}
	})
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
	for _, source := range sources {
		updateCollectProgress(source.Id, func(progress *model.CollectProgress) {
			if progress.Status == "starting" || progress.Status == "running" || progress.Status == "stopped" || progress.Status == "done" {
				progress.Status = "finalizing"
			}
		})
	}
}

func markSourcesPublished(sources map[string]model.FilmSource) {
	for _, source := range sources {
		updateCollectProgress(source.Id, func(progress *model.CollectProgress) {
			if progress.Status == "finalizing" {
				progress.Status = "done"
			}
		})
	}
}

func markSourcesFinalizeFailed(sources map[string]model.FilmSource) {
	for _, source := range sources {
		updateCollectProgress(source.Id, func(progress *model.CollectProgress) {
			if progress.Status == "finalizing" {
				progress.Status = "failed"
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
	flushing            bool
}

func newCollectLifecycle() *collectLifecycleState {
	state := &collectLifecycleState{
		activeSources:       make(map[string]struct{}),
		pendingFlushSources: make(map[string]model.FilmSource),
		affectedMIDs:        make(map[int64]struct{}),
		masterAffectedMIDs:  make(map[int64]struct{}),
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
	}
	return err
}

func (s *collectLifecycleState) runFlush(flush func([]int64, []int64) error) error {
	s.mu.Lock()
	s.waitIdleLocked()
	pending, affectedMIDs, masterMIDs := s.drainPendingLocked()
	s.mu.Unlock()
	defer s.finishFlushing()

	var err error
	if pending != nil {
		var finalMIDs, finalMasterMIDs []int64
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
			log.Printf("[Spider][Finalizer] 主站搜索标签异步刷新失败 mid_count=%d err=%v", len(mids), err)
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

func flushSourcesPending(tag string, sources []model.FilmSource) {
	if len(sources) == 0 {
		return
	}

	flushMap := make(map[string]model.FilmSource, len(sources))
	for _, source := range sources {
		source.Id = strings.TrimSpace(source.Id)
		if source.Id == "" {
			continue
		}
		flushMap[source.Id] = source
	}
	if len(flushMap) == 0 {
		return
	}

	if err := collectLifecycle.runFlush(func(affectedMIDs []int64, masterMIDs []int64) error {
		_, _, err := flushPendingSources(flushMap, affectedMIDs, masterMIDs)
		return err
	}); err != nil {
		log.Printf("[%s] 收尾刷新失败: %v", tag, err)
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
					return
				}
				if utils.IsRateLimitedErr(requestErr) {
					protectUntil := time.Now().Add(interval)
					gate.mu.Lock()
					if gate.nextAllowedAt.Before(protectUntil) {
						gate.nextAllowedAt = protectUntil
					}
					nextAllowedAt := gate.nextAllowedAt
					gate.mu.Unlock()
					log.Printf("[Spider][RateLimit] 站点 %s %s触发限流，延长保护冷却 cooldown_ms=%d next_at=%d err=%v", s.Name, tag, interval.Milliseconds(), nextAllowedAt.UnixMilli(), requestErr)
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
		if attempt < pageCountRetryTimes && utils.IsRateLimitedErr(err) {
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
		if attempt < filmDetailRetryTimes && utils.IsRateLimitedErr(lastErr) {
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
	delayIndex := attempt - 1
	if delayIndex >= len(retryBackoffs) {
		delayIndex = len(retryBackoffs) - 1
	}
	timer := time.NewTimer(retryBackoffs[delayIndex])
	defer timer.Stop()

	select {
	case <-ctx.Done():
		return ctx.Err()
	case <-timer.C:
		return nil
	}
}

func getSourcePageConcurrency(s *model.FilmSource) int {
	limit := config.MAXGoroutine
	if limit <= 0 {
		limit = 1
	}
	return limit
}

func runSourcesWithLimit(sources []model.FilmSource, h int, tag string) {
	if len(sources) == 0 {
		return
	}
	sources = filterCollectableSources(sources, tag)
	if len(sources) == 0 {
		return
	}
	markSourcesCollectStarting(sources)
	runVersion := stopAllVersion.Load()
	log.Printf("[%s] 主站/附属站并发采集，站点数=%d，站点并发不限制", tag, len(sources))
	runSourcesGroupWithLimit(sources, h, tag, 0, runVersion)
	if err := collectLifecycle.flushPending(); err != nil {
		log.Printf("[%s] 批量采集收尾刷新失败: %v", tag, err)
	}
}

func isDispatchStopped(runVersion uint64) bool {
	return stopAllVersion.Load() != runVersion
}

func runSourcesGroupWithLimit(sources []model.FilmSource, h int, tag string, limit int, runVersion uint64) {
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
			if err := handleCollectWithStopVersion(fs.Id, h, &runVersion, false, false); err != nil {
				log.Printf("[%s] 采集站点 %s 失败: %v", tag, fs.Name, err)
			}
		}(src)
	}
	wg.Wait()
}

// ======================================================= 通用采集方法  =======================================================

// HandleCollect 影视采集  id-采集站ID h-时长/h
func HandleCollect(id string, h int) error {
	return handleCollectWithStopVersion(id, h, nil, true, false)
}

func HandlePreparedCollect(id string, h int) error {
	return handleCollectWithStopVersion(id, h, nil, true, true)
}

func handleCollectWithStopVersion(id string, h int, runVersion *uint64, flushAtEnd bool, allowPreparedStart bool) (retErr error) {
	hadWrites := false
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
	defer func() {
		originalErr := retErr
		if originalErr != nil && !hadWrites {
			collectLifecycle.endSource(s.Id)
			return
		}
		if flushAtEnd {
			updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
				if progress.Status == "starting" || progress.Status == "running" || progress.Status == "stopped" || progress.Status == "done" {
					progress.Status = "finalizing"
				}
			})
			flushErr := collectLifecycle.finishSourceAndFlush(*s)
			if originalErr == nil && flushErr != nil {
				retErr = flushErr
			}
			updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
				if flushErr != nil && progress.Status == "finalizing" {
					progress.Status = "failed"
					return
				}
				if progress.Status == "finalizing" {
					progress.Status = "done"
				}
			})
			return
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
						progress.Status = "failed"
						return
					}
				})
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
	// pageCount = 0 说明该站点在当前时间段内无新数据，任务无需执行
	if pageCount <= 0 {
		updateCollectProgress(id, func(progress *model.CollectProgress) {
			progress.Total = 0
			progress.Current = 0
			progress.Success = 0
			progress.Failed = 0
			progress.Status = "done"
		})
		log.Printf("[Spider] 站点 %s 无需采集 (pageCount=%d，可能该时间段内无新内容)\n", s.Name, pageCount)
		return nil
	}
	updateCollectProgress(id, func(progress *model.CollectProgress) {
		progress.Total = pageCount
		progress.Current = 0
		progress.Success = 0
		progress.Failed = 0
		progress.Status = "running"
	})
	log.Printf("[Spider] 站点 %s 共 %d 页，开始采集...\n", s.Name, pageCount)

	pageWorkerLimit := getSourcePageConcurrency(s)
	hadWrites, err = collectFilmPages(ctx, pageCount, pageWorkerLimit, s, h)
	if err != nil {
		return err
	}
	if ctx.Err() != nil {
		log.Printf("[Spider] 站点 %s 已停止接收新分页，等待收尾刷新\n", s.Name)
	}
	if s.Grade == model.MasterCollect && s.SyncPictures {
		triggerPictureSync()
	}

	return nil
}

func triggerPictureSync() {
	if !pictureSyncRunning.CompareAndSwap(false, true) {
		return
	}
	go func() {
		defer pictureSyncRunning.Store(false)
		repository.SyncFilmPicture()
	}()
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

// saveCollectedFilm 将已采集的 list 按站点类型写入存储，消除 collectFilm/collectFilmById 中的重复 switch 块。
// saveMaster 由调用方注入，区分批量(SaveDetails)与单条(SaveDetail)两种写入策略。
func saveCollectedFilm(s *model.FilmSource, list []model.MovieDetail, saveMaster func(string, []model.MovieDetail) error) error {
	var err error
	switch s.Grade {
	case model.MasterCollect:
		lock := getSourceWriteLock(s.Id)
		lock.Lock()
		defer lock.Unlock()
		err = saveMaster(s.Id, list)
		if err != nil {
			return fmt.Errorf("save master details failed: %w", err)
		}
		if s.SyncPictures {
			if err = repository.SaveVirtualPic(conver.ConvertVirtualPicture(list)); err != nil {
				return fmt.Errorf("save virtual pictures failed: %w", err)
			}
		}
	case model.SlaveCollect:
		lock := getSourceWriteLock(s.Id)
		lock.Lock()
		defer lock.Unlock()
		err = runCollectDBWriteWithRetry(context.Background(), s.Name, 0, func() error {
			return filmrepo.SaveSitePlayList(s.Id, list)
		})
		if err != nil {
			return fmt.Errorf("save slave playlists failed: %w", err)
		}
	}
	return nil
}

func saveCollectedFilmForCollect(ctx context.Context, s *model.FilmSource, page int, list []model.MovieDetail) ([]int64, error) {
	if s.Grade != model.MasterCollect {
		return nil, saveCollectedFilm(s, list, filmrepo.SaveDetails)
	}

	var affectedPids []int64
	lock := getSourceWriteLock(s.Id)
	lock.Lock()
	defer lock.Unlock()
	err := runCollectDBWriteWithRetry(ctx, s.Name, page, func() error {
		pids, err := filmrepo.SaveDetailsForCollect(s.Id, list)
		if err != nil {
			return err
		}
		affectedPids = pids
		return nil
	})
	if err != nil {
		return nil, fmt.Errorf("save master details failed: %w", err)
	}
	if s.SyncPictures {
		if err = repository.SaveVirtualPic(conver.ConvertVirtualPicture(list)); err != nil {
			return nil, fmt.Errorf("save virtual pictures failed: %w", err)
		}
	}
	return affectedPids, nil
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
		log.Printf("[Spider][Failure] 失败页记录保存失败 source_id=%s source=%s page=%d hour=%d phase=%s err=%v record_err=%v", s.Id, s.Name, pg, h, phase, err, recordErr)
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
func collectFilmPages(parentCtx context.Context, pageCount int, requestWorkerLimit int, s *model.FilmSource, h int) (bool, error) {
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
			log.Printf("[Spider] 站点 %s 连续失败达到阈值，终止采集 page=%d stage=%s err=%v", s.Name, page, stage, err)
			updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
				progress.Status = "failed"
			})
			cancel()
		})
	}
	recordSuccess := func() {
		consecutiveFailuresMu.Lock()
		consecutiveFailures = 0
		consecutiveFailuresMu.Unlock()
	}
	markStopped := func() {
		updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
			progress.Status = "stopped"
		})
	}
	recordPageFailure := func(page int, stage string, err error) {
		snapshot := recordPageFinished(page, false)
		updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
			progress.Failed = snapshot.failed
			if page > progress.Current {
				progress.Current = page
			}
		})
		saveFilmPageFailure(s, h, page, stage, err)
		if shouldLogCollectFailure(snapshot.failed) {
			log.Printf("[Spider] 站点 %s 采集失败累计=%d，最近失败 page=%d stage=%s err=%v", s.Name, snapshot.failed, page, stage, err)
		}
		recordFailure(page, stage, err)
		logProgress(false)
	}
	recordPageSuccess := func(page int, mids []int64) {
		snapshot := recordPageFinished(page, true)
		recordSuccess()
		if s.Grade == model.MasterCollect {
			collectLifecycle.addMasterAffectedMIDs(mids)
		} else {
			collectLifecycle.addAffectedMIDs(mids)
		}
		updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
			progress.Success = snapshot.success
			progress.Failed = snapshot.failed
			if page > progress.Current {
				progress.Current = page
			}
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
					updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
						progress.Status = "stopped"
					})
					return
				case pg, ok := <-pages:
					if !ok {
						return
					}
					updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
						if pg > progress.Current {
							progress.Current = pg
						}
						progress.Status = "running"
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
					submitErr := collectWrites.submit(context.Background(), collectWriteJob{
						sourceID:   s.Id,
						sourceName: s.Name,
						grade:      s.Grade,
						page:       page,
						write: func() ([]int64, error) {
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
							updateCollectProgress(s.Id, func(progress *model.CollectProgress) {
								progress.Status = "stopped"
							})
							return
						}
						recordPageFailure(page, "enqueue", submitErr)
						continue
					}
					recordSuccess()
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
		recordPageSuccess(completion.page, completion.mids)
	}
	logProgress(true)
	if ctx.Err() != nil {
		log.Printf("[Spider] 站点 %s 并发采集任务已中断，worker 已全部退出\n", s.Name)
	}
	if stopErr != nil {
		return stats.success > 0, stopErr
	}
	return stats.success > 0, nil
}

// collectFilmById 采集指定ID的影片信息
func collectFilmById(ids string, s *model.FilmSource, flushAtEnd bool) (retErr error) {
	if s == nil {
		return errors.New("采集站信息不存在")
	}
	if err := collectLifecycle.beginSource(s.Id); err != nil {
		log.Printf("[Spider] 站点 %s 无法启动单片采集: %v\n", s.Id, err)
		return err
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
		return err
	}
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("pg", "1")
	r.Params.Set("ids", ids)
	list, err := spiderCore.GetFilmDetail(r)
	if err != nil {
		release(err)
		return fmt.Errorf("get movie detail failed: %w", err)
	}
	if len(list) <= 0 {
		release(errors.New("response list is empty"))
		return errors.New("get movie detail failed: response list is empty")
	}
	release(nil)
	if err := saveCollectedFilm(s, list, func(id string, l []model.MovieDetail) error {
		return filmrepo.SaveDetail(id, l[0])
	}); err != nil {
		return err
	}
	return nil
}

// BatchCollect 批量采集, 采集指定的所有站点最近x小时内更新的数据
func BatchCollect(h int, ids ...string) {
	sources := make([]model.FilmSource, 0)
	for _, id := range ids {
		if fs := repository.FindCollectSourceById(id); fs != nil && fs.State {
			sources = append(sources, *fs)
		}
	}

	if len(sources) == 0 {
		return
	}

	runSourcesWithLimit(sources, h, "Batch-Collect")
}

func getEnabledSourcesByGrade(grade model.SourceGrade) []model.FilmSource {
	sources := repository.GetCollectSourceListByGrade(grade)
	enabled := make([]model.FilmSource, 0, len(sources))
	for _, s := range sources {
		if s.State {
			enabled = append(enabled, s)
		}
	}
	return enabled
}

// AutoCollect 自动进行对所有已启用站点的采集任务
func AutoCollect(h int) {
	sources := repository.GetCollectSourceList()
	enabled := make([]model.FilmSource, 0, len(sources))
	for _, source := range sources {
		if source.State {
			enabled = append(enabled, source)
		}
	}
	if len(enabled) == 0 {
		log.Println("[Spider] 自动采集：未找到任何启用的站点")
		return
	}

	runSourcesWithLimit(enabled, h, "Auto-Collect")
}

// ClearSpider 删除所有已采集的影片信息，并与采集写库互斥。
func ClearSpider() error {
	StopAllTasks()
	if err := collectLifecycle.waitIdle(time.Second * 30); err != nil {
		return err
	}
	return collectLifecycle.runExclusive(func() error {
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

	all := repository.GetCollectSourceList()
	enabled := make([]model.FilmSource, 0, len(all))
	for _, source := range all {
		if source.State {
			enabled = append(enabled, source)
		}
	}
	if len(enabled) == 0 {
		log.Println("[Spider] CollectSingleFilm: 未找到任何启用的站点")
		return
	}

	var wg sync.WaitGroup
	for _, source := range enabled {
		requestID := resolveSingleCollectSourceMid(globalMid, source)
		if requestID == "" {
			continue
		}

		wg.Add(1)
		go func(src model.FilmSource, sourceMid string) {
			defer wg.Done()
			if err := collectFilmById(sourceMid, &src, false); err != nil {
				log.Printf("[Spider] CollectSingleFilm 站点 %s 更新失败: %v", src.Name, err)
			}
		}(source, requestID)
	}
	wg.Wait()
	flushSourcesPending("CollectSingleFilm", enabled)
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
func recoverFilmPage(ctx context.Context, s *model.FilmSource, fr *model.FailureRecord) {
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

	mids, err := saveCollectedFilmForCollect(ctx, s, fr.PageNumber, list)
	if err != nil {
		markRecoverFailure(s, fr, "recover_save", err)
		log.Println("Recover saveCollectedFilm Error: ", err)
		return
	}
	if s.Grade == model.MasterCollect {
		collectLifecycle.addMasterAffectedMIDs(mids)
	} else {
		collectLifecycle.addAffectedMIDs(mids)
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
		log.Printf("[Spider][Recover] 失败记录更新失败 source_id=%s source=%s page=%d hour=%d err=%v", s.Id, s.Name, fr.PageNumber, fr.Hour, updateErr)
		return
	}
	if final {
		log.Printf("[Spider][Recover] 失败页达到最大自动重试次数 source_id=%s source=%s page=%d hour=%d retry_count=%d", s.Id, s.Name, fr.PageNumber, fr.Hour, retryCount)
	}
}

// ======================================================= 采集拓展内容  =======================================================

// SingleRecoverSpider 二次采集
func SingleRecoverSpider(fr *model.FailureRecord) {
	// 仅对当前失败记录所属站点+失败页进行重试，不干扰正在运行的采集任务
	s := repository.FindCollectSourceById(fr.OriginId)
	if s == nil {
		log.Printf("[Spider] 重试失败: 站点 %s 不存在\n", fr.OriginId)
		return
	}
	if err := collectLifecycle.waitAndBeginSource(s.Id); err != nil {
		log.Printf("[Spider] 站点 %s 无法启动失败页重试: %v\n", s.Id, err)
		return
	}
	defer func() {
		if err := collectLifecycle.finishSourceAndFlush(*s); err != nil {
			log.Printf("[Spider] 站点 %s 失败页重试收尾刷新失败: %v\n", s.Id, err)
		}
	}()
	recoverFilmPage(context.Background(), s, fr)
}

// FullRecoverSpider 扫描记录表中的失败记录, 并发重试各失败页
func FullRecoverSpider() {
	list := repository.PendingRecord()
	sourcesToFlush := make([]model.FilmSource, 0, len(list))
	seen := make(map[string]struct{}, len(list))
	recordsBySource := make(map[string][]model.FailureRecord, len(list))
	sourceByID := make(map[string]model.FilmSource, len(list))
	limit := config.MAXGoroutine
	if limit <= 0 {
		limit = 1
	}
	sem := make(chan struct{}, limit)
	var wg sync.WaitGroup
	for i := range list {
		fr := list[i]
		s := repository.FindCollectSourceById(fr.OriginId)
		if s == nil {
			log.Printf("[Spider] 重试失败: 站点 %s 不存在\n", fr.OriginId)
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
				log.Printf("[Spider] 站点 %s 无法启动失败页重试: %v\n", source.Id, err)
				return
			}
			defer collectLifecycle.endSource(source.Id)
			for i := range pending {
				record := pending[i]
				recoverFilmPage(context.Background(), &source, &record)
			}
		}(src, recordsCopy)
	}
	wg.Wait()
	flushSourcesPending("FullRecoverSpider", sourcesToFlush)
}

// ======================================================= 公共方法  =======================================================

// CollectApiTest 测试采集接口是否可用
func CollectApiTest(s model.FilmSource) error {
	// 使用 ac=list 测试：获取分类列表，所有标准 Mac CMS 站均支持，
	// 且不需要额外过滤参数（ac=detail 在无 h/t 参数时部分站点会返回 400）
	r := utils.RequestInfo{Uri: s.Uri, Params: url.Values{}}
	r.Params.Set("ac", "list")
	r.Params.Set("pg", "1")
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
	list := make([]model.CollectProgress, 0)
	seen := make(map[string]struct{})
	activeTasks.Range(func(key, value any) bool {
		id := key.(string)
		seen[id] = struct{}{}
		if progress, ok := collectProgressSnapshot(id); ok {
			list = append(list, progress)
			return true
		}
		list = append(list, model.CollectProgress{Id: id, Status: "running"})
		return true
	})
	collectProgress.Range(func(key, value any) bool {
		id := key.(string)
		if _, ok := seen[id]; ok {
			return true
		}
		state := value.(*collectProgressState)
		state.mu.RLock()
		progress := state.data
		state.mu.RUnlock()
		if progress.Status == "starting" || progress.Status == "running" || progress.Status == "finalizing" {
			list = append(list, progress)
		}
		return true
	})
	return list
}

// StopAllTasks 强制停止当前系统中所有正在进行的采集任务
func StopAllTasks() {
	stopAllVersion.Add(1)
	count := 0
	collectProgress.Range(func(key, value any) bool {
		state := value.(*collectProgressState)
		state.mu.Lock()
		if state.data.Status == "starting" || state.data.Status == "running" {
			state.data.Status = "stopped"
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
			updateCollectProgress(id, func(progress *model.CollectProgress) {
				progress.Status = "stopped"
			})
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
		log.Printf("[Spider] 终止采集后收尾刷新失败: %v", err)
	}
}

// IsTaskRunning 查询指定站点的采集任务是否正在运行
func IsTaskRunning(id string) bool {
	_, ok := activeTasks.Load(id)
	return ok
}

// IsAnyTaskRunning 查询系统中是否有任何采集任务正在进行
func IsAnyTaskRunning() bool {
	running := false
	activeTasks.Range(func(key, value any) bool {
		running = true
		return false // 找到一个就退出循环
	})
	return running
}
