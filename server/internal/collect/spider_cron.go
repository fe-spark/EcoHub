package collect

import (
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	filmrepo "server/internal/film"

	"github.com/robfig/cron/v3"
)

var CronCollect *cron.Cron = CreateCron()

// taskCidMap 运行时内存注册表：task.Id → cron.EntryID
// Cid 是内存值，不持久化到 DB，每次重启重新注册
var (
	taskCidMap   = make(map[string]cron.EntryID)
	taskCidLock  sync.RWMutex
	cronReloadMu sync.Mutex
)

// runningCronCounts 记录当前正在执行的定时任务并发数,用于前端展示"执行中"以及禁用编辑操作。
var (
	runningCronMu     sync.Mutex
	runningCronCounts = make(map[string]int)
)

func enterCronTaskRun(id string, exclusive bool) bool {
	if id == "" {
		return true
	}
	runningCronMu.Lock()
	defer runningCronMu.Unlock()
	if exclusive && runningCronCounts[id] > 0 {
		return false
	}
	runningCronCounts[id]++
	return true
}

func leaveCronTaskRun(id string) {
	if id == "" {
		return
	}
	runningCronMu.Lock()
	defer runningCronMu.Unlock()
	if runningCronCounts[id] > 1 {
		runningCronCounts[id]--
	} else {
		delete(runningCronCounts, id)
	}
}

// RegisterTaskCid 将 taskId 与运行时 cron.EntryID 关联
func RegisterTaskCid(taskId string, cid cron.EntryID) {
	taskCidLock.Lock()
	defer taskCidLock.Unlock()
	taskCidMap[taskId] = cid
}

// GetEntryByTaskId 通过 taskId 查找运行时 cron.Entry（含上次/下次执行时间）
func GetEntryByTaskId(taskId string) cron.Entry {
	taskCidLock.RLock()
	if cid, ok := taskCidMap[taskId]; ok {
		taskCidLock.RUnlock()
		return CronCollect.Entry(cid)
	}
	taskCidLock.RUnlock()
	return cron.Entry{}
}

// RemoveCronByTaskId 通过 taskId 删除定时任务并注销注册
func RemoveCronByTaskId(taskId string) {
	taskCidLock.Lock()
	defer taskCidLock.Unlock()
	if cid, ok := taskCidMap[taskId]; ok {
		CronCollect.Remove(cid)
		delete(taskCidMap, taskId)
	}
}

// ClearAllCrons 注销运行时注册的全部定时任务并清空触发记录。
func ClearAllCrons() {
	taskCidLock.Lock()
	for taskID, cid := range taskCidMap {
		CronCollect.Remove(cid)
		delete(taskCidMap, taskID)
	}
	taskCidLock.Unlock()
	taskRanAt.Range(func(key, _ any) bool {
		taskRanAt.Delete(key)
		return true
	})
}

// CreateCron 创建定时任务
func CreateCron() *cron.Cron {
	return cron.New(cron.WithSeconds(), cron.WithChain(cron.Recover(cron.PrintfLogger(log.Default()))))
}

func addFilmCron(id, spec string) (cron.EntryID, error) {
	if err := ValidSpec(spec); err != nil {
		return -99, fmt.Errorf("定时任务添加失败，Cron 表达式校验失败: %w", err)
	}
	return CronCollect.AddFunc(spec, func() { fireCronTask(id) })
}

// fireCronTask 定时任务到点：读取最新配置后入队。读取失败时告警，避免任务静默不跑。
func fireCronTask(id string) {
	ft, err := GetFilmTaskById(id)
	if err != nil {
		syslog.Errorf("[Cron] 定时任务到点但读取任务配置失败 id=%s err=%v", id, err)
		emitCronFailedNotify(id, id, fmt.Sprintf("读取定时任务配置失败，本轮未执行: %v", err))
		return
	}
	enqueueCronTask(ft)
}

// AddAutoUpdateCron 添加所有已启用站点的影片更新定时任务。
func AddAutoUpdateCron(id, spec string) (cron.EntryID, error) {
	return addFilmCron(id, spec)
}

// ValidSpec 校验cron表达式是否有效
func ValidSpec(spec string) error {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	_, err := parser.Parse(spec)
	return err
}

// AddOrphanCleanCron 添加片库冗余数据与孤儿清理定时任务。
func AddOrphanCleanCron(id, spec string) (cron.EntryID, error) {
	return addFilmCron(id, spec)
}

// AddCollectRecordCleanCron 定期清理超过 30 天的采集运行记录与快照。
func AddCollectRecordCleanCron(id, spec string) (cron.EntryID, error) {
	return addFilmCron(id, spec)
}

// AddLogCleanCron 添加系统运行日志清理定时任务。
func AddLogCleanCron(id, spec string) (cron.EntryID, error) {
	return addFilmCron(id, spec)
}

// ReloadCronTask 重新加载定时任务（当配置或状态发生变化时）
func ReloadCronTask(id string) error {
	cronReloadMu.Lock()
	defer cronReloadMu.Unlock()

	// 1. 获取最新配置
	ft, err := GetFilmTaskById(id)
	if err != nil {
		return err
	}

	// 2. 移除旧任务
	RemoveCronByTaskId(id)
	if !ft.State {
		return nil
	}

	// 3. 重新注册新任务
	var cid cron.EntryID
	switch ft.Model {
	case model.TaskModelAutoCollect:
		cid, err = AddAutoUpdateCron(ft.Id, ft.Spec)
	case model.TaskModelOrphanClean:
		cid, err = AddOrphanCleanCron(ft.Id, ft.Spec)
	case model.TaskModelLogClean:
		cid, err = AddLogCleanCron(ft.Id, ft.Spec)
	case model.TaskModelCollectClean:
		cid, err = AddCollectRecordCleanCron(ft.Id, ft.Spec)
	default:
		return fmt.Errorf("不支持的定时任务类型: %d", ft.Model)
	}

	if err != nil {
		return err
	}

	RegisterTaskCid(id, cid)
	return nil
}

// executeTask 执行已入队或手动触发的任务。
// 自动采集支持重叠触发：不整轮阻塞，忙碌站点由批量准入按站跳过，空闲站点立即开跑；
// 其他（维护类）任务保持同任务互斥，还在执行时跳过本轮。
// manual 表示手动“立即执行”，此时失败不发定时任务告警。
func executeTask(ft model.FilmCollectTask, manual bool) {
	if !ft.State {
		return
	}
	exclusive := ft.Model != model.TaskModelAutoCollect
	if !enterCronTaskRun(ft.Id, exclusive) {
		log.Printf("定时任务跳过: Task[%s] 已在执行中\n", ft.Id)
		return
	}
	defer leaveCronTaskRun(ft.Id)
	runTaskBody(ft, manual)
}

func runTaskBody(ft model.FilmCollectTask, manual bool) {

	log.Printf("开始执行定时任务: Task[%s] Model[%d]\n", ft.Id, ft.Model)

	switch ft.Model {
	case model.TaskModelAutoCollect:
		runAutoCollectTask(ft, manual)
		log.Println("执行一次已启用站点采集")
	case model.TaskModelOrphanClean:
		executeOrphanCleanTask(ft, manual)
		return
	case model.TaskModelLogClean:
		executeLogCleanTask(ft, manual)
		return
	case model.TaskModelCollectClean:
		executeCollectRecordCleanTask(ft, manual)
		return
	default:
		errMsg := fmt.Sprintf("定时任务[%s]类型[%d]已废弃，跳过执行", ft.Id, ft.Model)
		log.Println(errMsg)
		emitCronFailedNotify(ft.Id, ft.Remark, errMsg)
	}

	log.Printf("定时任务执行完毕: Task[%s]\n", ft.Id)
}

var runningMaintenanceTasks sync.Map

// ValidTaskModel 判断定时任务模型是否受支持。
func ValidTaskModel(taskModel int) bool {
	switch taskModel {
	case model.TaskModelAutoCollect, model.TaskModelOrphanClean, model.TaskModelLogClean, model.TaskModelCollectClean:
		return true
	default:
		return false
	}
}

// isMaintenanceModel 维护任务：除自动采集之外，其余受支持的定时任务均属于维护任务。
// 维护任务进串行队列依次执行，执行时占用维护闸门（停止采集、独占执行），同时段到点的采集跳过。
func isMaintenanceModel(taskModel int) bool {
	return ValidTaskModel(taskModel) && taskModel != model.TaskModelAutoCollect
}

func maintenanceRunningDetail() string {
	ids := make([]string, 0)
	runningMaintenanceTasks.Range(func(key, _ any) bool {
		id, _ := key.(string)
		if id != "" {
			ids = append(ids, id)
		}
		return true
	})
	sort.Strings(ids)
	current, pending := maintenanceQueueDetail()
	if len(ids) == 0 && current != "" {
		ids = append(ids, current)
	}
	parts := make([]string, 0, 2)
	if len(ids) > 0 {
		parts = append(parts, "执行中："+strings.Join(ids, "，"))
	}
	if len(pending) > 0 {
		parts = append(parts, "排队中："+strings.Join(pending, "，"))
	}
	if len(parts) == 0 {
		if collectLifecycle.isMaintenanceBusy() {
			return "维护闸门占用中"
		}
		return "无记录"
	}
	return strings.Join(parts, "；")
}

// maintenanceTaskActive 有维护任务在执行或排队（不含闸门被采集站接口探测等短暂独占的情况）。
func maintenanceTaskActive() bool {
	count := 0
	runningMaintenanceTasks.Range(func(_, _ any) bool {
		count++
		return false
	})
	return count > 0 || maintenanceQueueActive()
}

// isMaintenanceRunning 维护中：有维护在执行、在排队，或维护闸门被占用。此时采集一律跳过。
func isMaintenanceRunning() bool {
	return maintenanceTaskActive() || collectLifecycle.isMaintenanceBusy()
}

// rejectManualRun 维护中手动执行采集直接拒绝；维护任务不拒绝，改为排队。
func rejectManualRun(ft model.FilmCollectTask) error {
	if !isMaintenanceRunning() {
		return nil
	}
	if ft.Model == model.TaskModelAutoCollect {
		return fmt.Errorf("%w（%s）", errCollectDuringMaintenance, maintenanceRunningDetail())
	}
	return nil
}

func executeMaintenance(ft model.FilmCollectTask, manual bool, fallback string, action func(remark string, startedAt time.Time) error) {
	runningMaintenanceTasks.Store(ft.Id, struct{}{})
	defer runningMaintenanceTasks.Delete(ft.Id)

	startedAt := time.Now()
	remark := strings.TrimSpace(ft.Remark)
	if remark == "" {
		remark = fallback
	}
	err := collectLifecycle.runMaintenance(fmt.Sprintf("维护任务开工，采集让行：%s（%s）", remark, ft.Id), func() error {
		noteTaskRan(ft.Id, time.Now())
		log.Printf("[Maintenance] 采集已让行，开始执行：%s（%s）", remark, ft.Id)
		return action(remark, startedAt)
	})
	finishMaintenance(ft, manual, remark, err)
}

func finishMaintenance(ft model.FilmCollectTask, manual bool, remark string, err error) {
	if err == nil {
		return
	}
	syslog.Errorf("[Maintenance] %s 失败: %v", remark, err)
	if !manual {
		emitCronFailedNotify(ft.Id, remark, err.Error())
	}
}

func executeOrphanCleanTask(ft model.FilmCollectTask, manual bool) {
	executeMaintenance(ft, manual, "片库冗余数据与孤儿清理", func(remark string, startedAt time.Time) error {
		if db.Mdb == nil {
			return nil
		}
		n, k, cleanErr := cleanOrphanData()
		if cleanErr != nil {
			return cleanErr
		}
		m, p, ghostErr := cleanGhostFilms()
		if ghostErr != nil {
			return ghostErr
		}
		log.Printf("定时任务完成: %s，孤儿播放列表 %d 条，悬空映射 %d 条，无线路幽灵 %d 条，空记录 %d 条，耗时 %s",
			remark, n, k, p, m, time.Since(startedAt))
		return nil
	})
}

func cleanOrphanData() (int64, int64, error) {
	n, err := filmrepo.CleanOrphanPlaylists()
	if err != nil {
		return n, 0, fmt.Errorf("清理孤儿播放列表失败: %w", err)
	}
	k, err := filmrepo.CleanOrphanMatchKeysAndMappings()
	if err != nil {
		return n, k, err
	}
	return n, k, nil
}

func cleanGhostFilms() (int64, int64, error) {
	m, p, err := filmrepo.CleanGhostFilms(7 * 24 * time.Hour)
	if err != nil {
		return m, p, fmt.Errorf("清理无效影片失败: %w", err)
	}
	return m, p, nil
}

const (
	logRetention           = 7 * 24 * time.Hour
	failureRecordRetention = 7 * 24 * time.Hour
)

func executeLogCleanTask(ft model.FilmCollectTask, manual bool) {
	executeMaintenance(ft, manual, "清理过期运行日志", func(remark string, startedAt time.Time) error {
		n, err := syslog.PruneExpiredLogs(logRetention)
		if err != nil {
			return err
		}
		log.Printf("定时任务完成: %s，清理过期日志 %d 个，耗时 %s", remark, n, time.Since(startedAt))
		return nil
	})
}

func executeCollectRecordCleanTask(ft model.FilmCollectTask, manual bool) {
	executeMaintenance(ft, manual, "定期清理超过 30 天的采集运行记录与快照", func(remark string, startedAt time.Time) error {
		if err := CleanOldCollectRecords(30); err != nil {
			return err
		}
		log.Printf("定时任务完成: %s，耗时 %s", remark, time.Since(startedAt))
		return nil
	})
}

// 手动执行的结果提示。
const (
	RunOnceStarted = "定时任务已触发，执行结果请查看运行日志"
	RunOnceQueued  = "已有维护任务在执行，本任务已排队，前一条结束后自动执行"
)

// RunTaskOnce 立即手动执行一次任务
func RunTaskOnce(id string) error {
	_, err := RunTaskOnceWithStatus(id)
	return err
}

// RunTaskOnceWithStatus 立即手动执行一次任务，并返回给用户的提示。
// 维护任务进串行队列：前面有维护在跑时排队等待；采集在维护中直接拒绝。
func RunTaskOnceWithStatus(id string) (string, error) {
	ft, err := GetFilmTaskById(id)
	if err != nil {
		return "", err
	}
	if err := rejectManualRun(ft); err != nil {
		return "", err
	}
	if isMaintenanceModel(ft.Model) {
		if IsCronTaskRunning(ft.Id) {
			return "", fmt.Errorf("定时任务正在执行中，请等待当前执行完成")
		}
		// 只有真有维护在执行或排队才提示“已排队”；闸门被接口探测短暂占用时直接开始，开工时等闸门空闲。
		busy := maintenanceTaskActive()
		ft.State = true // 手动执行不受启用开关限制
		if !enqueueManualMaintenance(ft) {
			return "", fmt.Errorf("该维护任务已在排队或执行中")
		}
		if busy {
			return RunOnceQueued, nil
		}
		return RunOnceStarted, nil
	}
	enterCronTaskRun(ft.Id, false)
	go func() {
		defer leaveCronTaskRun(ft.Id)
		withCronRecover(ft, func() { runTaskBody(ft, true) })
	}()
	return RunOnceStarted, nil
}

func IsCronTaskRunning(id string) bool {
	runningCronMu.Lock()
	defer runningCronMu.Unlock()
	return runningCronCounts[id] > 0
}

var taskRanAt sync.Map

func noteTaskRan(id string, at time.Time) {
	if id == "" || at.IsZero() {
		return
	}
	taskRanAt.Store(id, at)
}

// TaskPrev 上次真正开工的时间。调度入队、让行和跳过不算。
func TaskPrev(taskId string) time.Time {
	if v, ok := taskRanAt.Load(taskId); ok {
		if at, ok := v.(time.Time); ok {
			return at
		}
	}
	return time.Time{}
}
