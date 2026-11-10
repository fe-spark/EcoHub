package spider

import (
	"errors"
	"log"
	"strings"
	"sync"

	"server/internal/model"
	"server/internal/repository"

	"gorm.io/gorm"
)

// 维护串行队列：所有维护任务（片库清理、日志清理、采集记录清理）都进同一个 FIFO 队列，
// 由单个工作协程依次执行，前一条结束再开始下一条，不跳过、不丢弃。
// 同一任务已在排队或正在执行时去重。队列非空或正在执行期间视为“维护中”，到点的自动采集跳过。
var maintenanceQueue = newMaintenanceQueueState()

// queuedMaintenance 排队中的维护。manual 表示手动执行：不受启用开关限制。
type queuedMaintenance struct {
	ft     model.FilmCollectTask
	manual bool
}

type maintenanceQueueState struct {
	mu      sync.Mutex
	pending []queuedMaintenance
	queued  map[string]struct{}
	current string
	working bool
	idle    *sync.Cond
}

func newMaintenanceQueueState() *maintenanceQueueState {
	q := &maintenanceQueueState{queued: make(map[string]struct{})}
	q.idle = sync.NewCond(&q.mu)
	return q
}

// enqueueMaintenance 把定时到点的维护任务加入队尾。返回 false 表示该任务已在排队或正在执行（去重）。
func enqueueMaintenance(ft model.FilmCollectTask) bool {
	return maintenanceQueue.push(queuedMaintenance{ft: ft})
}

// enqueueManualMaintenance 手动执行的维护任务入队，不受启用开关限制。
func enqueueManualMaintenance(ft model.FilmCollectTask) bool {
	return maintenanceQueue.push(queuedMaintenance{ft: ft, manual: true})
}

func (q *maintenanceQueueState) push(item queuedMaintenance) bool {
	ft := item.ft
	if ft.Id == "" || !isMaintenanceModel(ft.Model) {
		return false
	}
	q.mu.Lock()
	if _, ok := q.queued[ft.Id]; ok || q.current == ft.Id {
		q.mu.Unlock()
		log.Printf("[Maintenance] 维护任务 %s 已在排队或执行中，本次不重复入队", ft.Id)
		return false
	}
	q.queued[ft.Id] = struct{}{}
	q.pending = append(q.pending, item)
	ahead := len(q.pending) - 1
	if q.current != "" {
		ahead++
	}
	startWorker := !q.working
	q.working = true
	q.mu.Unlock()
	if ahead > 0 {
		log.Printf("[Maintenance] 维护任务 %s 已排队，前面还有 %d 条", ft.Id, ahead)
	}
	if startWorker {
		go q.run()
	}
	return true
}

func (q *maintenanceQueueState) run() {
	for {
		q.mu.Lock()
		if len(q.pending) == 0 {
			q.working = false
			q.current = ""
			q.idle.Broadcast()
			q.mu.Unlock()
			return
		}
		item := q.pending[0]
		q.pending = q.pending[1:]
		delete(q.queued, item.ft.Id)
		q.current = item.ft.Id
		q.mu.Unlock()

		// 读配置等前置步骤也可能 panic：兜住后告警，工作协程继续处理下一条。
		withCronRecover(item.ft, func() { runQueuedMaintenance(item) })

		q.mu.Lock()
		q.current = ""
		q.mu.Unlock()
	}
}

// loadQueuedTask 开跑前读取任务最新配置。包级变量，测试可替换。
var loadQueuedTask = repository.GetFilmTaskById

// runQueuedMaintenance 开跑前读取最新配置；任务已删除、定时触发的任务已停用或类型变更则跳过。
func runQueuedMaintenance(item queuedMaintenance) {
	ft := item.ft
	latest, err := loadQueuedTask(ft.Id)
	switch {
	case err == nil:
		ft = latest
	case errors.Is(err, gorm.ErrRecordNotFound):
		log.Printf("[Maintenance] 维护任务 %s 已删除，跳过本次排队", ft.Id)
		return
	}
	if item.manual {
		ft.State = true
	}
	if !ft.State || !isMaintenanceModel(ft.Model) {
		log.Printf("[Maintenance] 维护任务 %s 已停用或类型变更，跳过本次排队", ft.Id)
		return
	}
	if maintenanceRunHook != nil {
		withCronRecover(ft, func() { maintenanceRunHook(ft) })
		return
	}
	runCronTaskSafely(ft, item.manual)
}

// maintenanceRunHook 仅供测试：替换队列里每条维护的实际执行。
var maintenanceRunHook func(ft model.FilmCollectTask)

// maintenanceQueueActive 队列里有任务或正在执行。
func maintenanceQueueActive() bool {
	q := maintenanceQueue
	q.mu.Lock()
	defer q.mu.Unlock()
	return q.working
}

// maintenanceQueueDetail 正在执行与排队中的维护任务，供日志与提示使用。
func maintenanceQueueDetail() (current string, pending []string) {
	q := maintenanceQueue
	q.mu.Lock()
	defer q.mu.Unlock()
	pending = make([]string, 0, len(q.pending))
	for _, item := range q.pending {
		pending = append(pending, item.ft.Id)
	}
	return q.current, pending
}

// IsCronTaskQueued 任务是否在维护队列中等待执行。
func IsCronTaskQueued(id string) bool {
	q := maintenanceQueue
	q.mu.Lock()
	defer q.mu.Unlock()
	_, ok := q.queued[strings.TrimSpace(id)]
	return ok
}

// waitMaintenanceQueueIdle 测试用：等待队列清空。
func waitMaintenanceQueueIdle() {
	q := maintenanceQueue
	q.mu.Lock()
	defer q.mu.Unlock()
	for q.working {
		q.idle.Wait()
	}
}
