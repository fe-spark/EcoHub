package spider

import (
	"errors"
	"fmt"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"
	filmsnapshot "server/internal/repository/film/snapshot"

	"github.com/robfig/cron/v3"
)

var CronCollect *cron.Cron = CreateCron()

// taskCidMap 运行时内存注册表：task.Id → cron.EntryID
// Cid 是内存值，不持久化到 DB，每次重启重新注册
var taskCidMap = make(map[string]cron.EntryID)
var taskCidLock sync.RWMutex

// runningCronTasks 记录当前正在执行的定时任务 ID,用于前端展示"执行中"以及禁用编辑操作。
var runningCronTasks sync.Map

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

// CreateCron 创建定时任务
func CreateCron() *cron.Cron {
	return cron.New(cron.WithSeconds(), cron.WithChain(cron.Recover(cron.PrintfLogger(log.Default()))))
}

func addFilmCron(id, spec string) (cron.EntryID, error) {
	if err := ValidSpec(spec); err != nil {
		return -99, fmt.Errorf("定时任务添加失败，Cron 表达式校验失败: %w", err)
	}
	return CronCollect.AddFunc(spec, func() {
		ft, err := repository.GetFilmTaskById(id)
		if err != nil {
			log.Printf("定时任务执行失败 id=%s err=%v", id, err)
			return
		}
		enqueueCronTask(ft)
	})
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
	// 1. 获取最新配置
	ft, err := repository.GetFilmTaskById(id)
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
// runningCronTasks 保证同一任务同一时刻只跑一份，重叠触发直接跳过。
func executeTask(ft model.FilmCollectTask) {
	if !ft.State {
		return
	}
	if _, alreadyRunning := runningCronTasks.LoadOrStore(ft.Id, struct{}{}); alreadyRunning {
		log.Printf("定时任务跳过: Task[%s] 已在执行中\n", ft.Id)
		return
	}
	defer runningCronTasks.Delete(ft.Id)
	runTaskBody(ft)
}

func runTaskBody(ft model.FilmCollectTask) {

	log.Printf("开始执行定时任务: Task[%s] Model[%d]\n", ft.Id, ft.Model)

	switch ft.Model {
	case model.TaskModelAutoCollect:
		AutoCollectTriggered(model.CollectTriggerCron, ft.Time)
		log.Println("执行一次已启用站点采集")
	case model.TaskModelOrphanClean:
		executeOrphanCleanTask(ft)
		return
	case model.TaskModelLogClean:
		executeLogCleanTask(ft)
		return
	case model.TaskModelCollectClean:
		executeCollectRecordCleanTask(ft)
		return
	default:
		errMsg := fmt.Sprintf("定时任务[%s]类型[%d]已废弃，跳过执行", ft.Id, ft.Model)
		log.Println(errMsg)
		emitCronFailedNotify(ft.Id, ft.Remark, errMsg)
	}

	log.Printf("定时任务执行完毕: Task[%s]\n", ft.Id)
}

var runningMaintenanceTasks sync.Map

// isMaintenanceModel 需要占用维护闸门（停止采集、独占执行）的任务。目前只有片库清理。
func isMaintenanceModel(taskModel int) bool {
	return taskModel == model.TaskModelOrphanClean
}

// isLightCronModel 不碰片库、无需停采集的轻量清理任务：日志清理、采集记录清理。
// 它们只受 runningCronTasks 防重入保护，不占维护闸门，也不让采集让行。
func isLightCronModel(taskModel int) bool {
	switch taskModel {
	case model.TaskModelLogClean, model.TaskModelCollectClean:
		return true
	default:
		return false
	}
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
	if len(ids) == 0 {
		if collectLifecycle.isMaintenanceBusy() {
			return "维护闸门占用中"
		}
		return "无记录"
	}
	sort.Strings(ids)
	return strings.Join(ids, "，")
}

func isMaintenanceRunning() bool {
	count := 0
	runningMaintenanceTasks.Range(func(_, _ any) bool {
		count++
		return false
	})
	return count > 0 || collectLifecycle.isMaintenanceBusy()
}

func rejectManualRun(ft model.FilmCollectTask) error {
	if !isMaintenanceRunning() {
		return nil
	}
	if isMaintenanceModel(ft.Model) {
		return fmt.Errorf("%w（%s）", errMaintenanceBusy, maintenanceRunningDetail())
	}
	if ft.Model == model.TaskModelAutoCollect {
		return fmt.Errorf("%w（%s）", errCollectDuringMaintenance, maintenanceRunningDetail())
	}
	return nil
}

func executeMaintenance(ft model.FilmCollectTask, fallback string, action func(remark string, startedAt time.Time) error) {
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
	finishMaintenance(ft, remark, err)
}

func finishMaintenance(ft model.FilmCollectTask, remark string, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, errMaintenanceBusy) {
		log.Printf("定时任务跳过: %s（%s），已有维护任务在执行（%s）", remark, ft.Id, maintenanceRunningDetail())
		return
	}
	syslog.Errorf("[Maintenance] %s 失败: %v", remark, err)
	emitCronFailedNotify(ft.Id, remark, err.Error())
}

func executeOrphanCleanTask(ft model.FilmCollectTask) {
	executeMaintenance(ft, "片库冗余数据与孤儿清理", func(remark string, startedAt time.Time) error {
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
	m, emptyErr := filmrepo.CleanEmptyFilms()
	if emptyErr != nil {
		emptyErr = fmt.Errorf("清理空记录失败: %w", emptyErr)
	}
	// 与原逻辑一致：空记录清理失败也继续尝试清理无线路幽灵影片，错误最后合并上报。
	p, playlessErr := filmrepo.CleanPlaylessFilms(7 * 24 * time.Hour)
	if playlessErr != nil {
		playlessErr = fmt.Errorf("清理无线路幽灵影片失败: %w", playlessErr)
	}
	cleanErr := errors.Join(emptyErr, playlessErr)
	// 只要删过影片（即使中途出错），都要重建快照，避免读模型继续展示已删除的影片。
	if m > 0 || p > 0 {
		if err := filmsnapshot.ActivateRebuiltFilmListSnapshot(""); err != nil {
			return m, p, errors.Join(cleanErr, fmt.Errorf("重建影片快照失败: %w", err))
		}
	}
	return m, p, cleanErr
}

const (
	logRetention           = 7 * 24 * time.Hour
	failureRecordRetention = 7 * 24 * time.Hour
)

// executeLightTask 轻量任务的执行流程：不停采集、不占闸门，失败时告警。
func executeLightTask(ft model.FilmCollectTask, fallback string, action func(remark string, startedAt time.Time) error) {
	startedAt := time.Now()
	remark := strings.TrimSpace(ft.Remark)
	if remark == "" {
		remark = fallback
	}
	noteTaskRan(ft.Id, startedAt)
	if err := action(remark, startedAt); err != nil {
		syslog.Errorf("[Cron] %s 失败: %v", remark, err)
		emitCronFailedNotify(ft.Id, remark, err.Error())
	}
}

// executeLogCleanTask 只删除磁盘上的历史轮转日志，与采集互不影响，不走维护闸门。
func executeLogCleanTask(ft model.FilmCollectTask) {
	executeLightTask(ft, "清理过期运行日志", func(remark string, startedAt time.Time) error {
		n, err := syslog.PruneExpiredLogs(logRetention)
		if err != nil {
			return err
		}
		log.Printf("定时任务完成: %s，清理过期日志 %d 个，耗时 %s", remark, n, time.Since(startedAt))
		return nil
	})
}

// executeCollectRecordCleanTask 不走维护闸门：清理只碰 started_at 超过 30 天且非 running 的记录，
// 以及早于 30 天、批次记录已不存在的清单行。采集只写进行中批次（记录与清单都是新时间），两者不会交叉；
// 也不涉及快照发布，因此无需 publishMu。
func executeCollectRecordCleanTask(ft model.FilmCollectTask) {
	executeLightTask(ft, "定期清理超过 30 天的采集运行记录与快照", func(remark string, startedAt time.Time) error {
		if err := repository.CleanOldCollectRecords(30); err != nil {
			return err
		}
		log.Printf("定时任务完成: %s，耗时 %s", remark, time.Since(startedAt))
		return nil
	})
}

// RunTaskOnce 立即手动执行一次任务
func RunTaskOnce(id string) error {
	ft, err := repository.GetFilmTaskById(id)
	if err != nil {
		return err
	}
	if err := rejectManualRun(ft); err != nil {
		return err
	}
	if _, alreadyRunning := runningCronTasks.LoadOrStore(ft.Id, struct{}{}); alreadyRunning {
		return fmt.Errorf("定时任务正在执行中，请等待当前执行完成")
	}
	go func() {
		defer runningCronTasks.Delete(ft.Id)
		withCronRecover(ft, func() { runTaskBody(ft) })
	}()
	return nil
}

func IsCronTaskRunning(id string) bool {
	_, ok := runningCronTasks.Load(id)
	return ok
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
