package spider

import (
	"errors"
	"fmt"
	"log"
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
var orphanCleanTaskLock sync.Mutex

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
	return cron.New(cron.WithSeconds())
}

// AddAutoUpdateCron 添加 所有已启用站点的影片更新定时任务
func AddAutoUpdateCron(id, spec string) (cron.EntryID, error) {
	// 校验 spec 表达式的有效性
	if err := ValidSpec(spec); err != nil {
		return -99, errors.New(fmt.Sprint("定时任务添加失败,Cron表达式校验失败: ", err.Error()))
	}
	return CronCollect.AddFunc(spec, func() {
		// 通过 Id 获取任务相关数据
		ft, err := repository.GetFilmTaskById(id)
		if err != nil {
			log.Println("FilmCollectCron Exec Failed: ", err)
			return
		}
		executeTask(ft)
	})
}



// ValidSpec 校验cron表达式是否有效
func ValidSpec(spec string) error {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	_, err := parser.Parse(spec)
	return err
}

// AddOrphanCleanCron 添加片库冗余数据与孤儿清理定时任务。
func AddOrphanCleanCron(id, spec string) (cron.EntryID, error) {
	if err := ValidSpec(spec); err != nil {
		return -99, errors.New(fmt.Sprint("定时任务添加失败，Cron 表达式校验失败: ", err.Error()))
	}
	return CronCollect.AddFunc(spec, func() {
		ft, err := repository.GetFilmTaskById(id)
		if err != nil {
			log.Println("OrphanCleanCron Exec Failed: ", err)
			return
		}
		executeTask(ft)
	})
}

// AddCollectRecordCleanCron 定期清理超过 30 天的采集运行记录与快照。
func AddCollectRecordCleanCron(id, spec string) (cron.EntryID, error) {
	if err := ValidSpec(spec); err != nil {
		return -99, errors.New(fmt.Sprint("定时任务添加失败,Cron表达式校验失败: ", err.Error()))
	}
	return CronCollect.AddFunc(spec, func() {
		ft, err := repository.GetFilmTaskById(id)
		if err != nil {
			log.Println("CollectRecordCleanCron Exec Failed: ", err)
			return
		}
		executeTask(ft)
	})
}

// AddLogCleanCron 添加系统运行日志清理定时任务
func AddLogCleanCron(id, spec string) (cron.EntryID, error) {
	if err := ValidSpec(spec); err != nil {
		return -99, errors.New(fmt.Sprint("定时任务添加失败,Cron表达式校验失败: ", err.Error()))
	}
	return CronCollect.AddFunc(spec, func() {
		ft, err := repository.GetFilmTaskById(id)
		if err != nil {
			log.Println("LogCleanCron Exec Failed: ", err)
			return
		}
		executeTask(ft)
	})
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

// executeTask 执行特定的定时任务逻辑（cron 调度入口）。
// 通过 runningCronTasks 的原子 LoadOrStore 保证同一任务同一时刻只跑一份;
// 重叠的触发(慢任务跨调度周期、cron 与手动同时来)直接 skip。
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
		if isMaintenanceRunning() {
			log.Printf("[Spider] 定时自动采集避让：检测到系统正在执行低频维护任务，自动跳过本次采集调度 (Task: %s)", ft.Id)
			return
		}
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

var (
	runningMaintenanceTasks sync.Map
	orphanCleanWaitTimeout  = 10 * time.Minute
	maintenanceWaitTimeout  = 10 * time.Minute
)

func isMaintenanceRunning() bool {
	count := 0
	runningMaintenanceTasks.Range(func(_, _ any) bool {
		count++
		return false
	})
	return count > 0 || collectLifecycle.isMaintenanceBusy()
}

func executeOrphanCleanTask(ft model.FilmCollectTask) {
	runningMaintenanceTasks.Store(ft.Id, struct{}{})
	defer runningMaintenanceTasks.Delete(ft.Id)

	orphanCleanTaskLock.Lock()
	defer orphanCleanTaskLock.Unlock()
	startedAt := time.Now()
	remark := ft.Remark
	if strings.TrimSpace(remark) == "" {
		remark = "片库冗余数据与孤儿清理"
	}
	err := collectLifecycle.reserveMaintenance(orphanCleanWaitTimeout, func() error {
		noteTaskRan(ft.Id, time.Now())
		log.Printf("[CleanOrphan] 低频清理优先：已阻断新采集，开始独占清理，waitCost=%s", time.Since(startedAt))
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
	finishOrphanClean(ft, remark, startedAt, err)
}

func finishOrphanClean(ft model.FilmCollectTask, remark string, startedAt time.Time, err error) {
	if err == nil {
		return
	}
	if errors.Is(err, errDrainTimeout) {
		log.Printf("定时任务跳过: %s，%v，耗时 %s", remark, err, time.Since(startedAt))
		return
	}
	syslog.Errorf("[CleanOrphan] 片库清理已执行但失败: %v", err)
	emitCronFailedNotify(ft.Id, remark, err.Error())
}

func cleanOrphanData() (int64, int64, error) {
	var n int64
	if res := db.Mdb.Where("NOT EXISTS (SELECT 1 FROM film_index WHERE film_index.mid = film_source_playlists.mid)").Delete(&model.FilmSourcePlaylist{}); res.Error != nil {
		return 0, 0, res.Error
	} else {
		n = res.RowsAffected
	}
	k := filmrepo.CleanOrphanMatchKeysAndMappings()
	return n, k, nil
}

func cleanGhostFilms() (int64, int64, error) {
	m := filmrepo.CleanEmptyFilms()
	p := filmrepo.CleanPlaylessFilms(7 * 24 * time.Hour)
	if m > 0 || p > 0 {
		if err := filmsnapshot.ActivateRebuiltFilmListSnapshot(""); err != nil {
			return m, p, err
		}
	}
	return m, p, nil
}

const (
	logRetention           = 7 * 24 * time.Hour
	failureRecordRetention = 7 * 24 * time.Hour
)

func executeLogCleanTask(ft model.FilmCollectTask) {
	runningMaintenanceTasks.Store(ft.Id, struct{}{})
	defer runningMaintenanceTasks.Delete(ft.Id)

	noteTaskRan(ft.Id, time.Now())
	startedAt := time.Now()
	remark := ft.Remark
	if strings.TrimSpace(remark) == "" {
		remark = "清理过期运行日志"
	}
	err := collectLifecycle.reserveMaintenance(maintenanceWaitTimeout, func() error {
		n, err := syslog.PruneExpiredLogs(logRetention)
		if err != nil {
			return err
		}
		log.Printf("定时任务完成: %s，清理过期日志 %d 个，耗时 %s", remark, n, time.Since(startedAt))
		return nil
	})
	if err != nil {
		if errors.Is(err, errDrainTimeout) {
			log.Printf("定时任务跳过: %s，%v，耗时 %s", remark, err, time.Since(startedAt))
			return
		}
		syslog.Errorf("[LogClean] 清理过期运行日志失败: %v", err)
		emitCronFailedNotify(ft.Id, remark, err.Error())
	}
}

func executeCollectRecordCleanTask(ft model.FilmCollectTask) {
	runningMaintenanceTasks.Store(ft.Id, struct{}{})
	defer runningMaintenanceTasks.Delete(ft.Id)

	noteTaskRan(ft.Id, time.Now())
	startedAt := time.Now()
	remark := ft.Remark
	if strings.TrimSpace(remark) == "" {
		remark = "定期清理超过 30 天的采集运行记录与快照"
	}
	err := collectLifecycle.reserveMaintenance(maintenanceWaitTimeout, func() error {
		return repository.CleanOldCollectRecords(30)
	})
	if err != nil {
		if errors.Is(err, errDrainTimeout) {
			log.Printf("定时任务跳过: %s，%v，耗时 %s", remark, err, time.Since(startedAt))
			return
		}
		log.Printf("定时任务失败: %s，err=%v，耗时 %s", remark, err, time.Since(startedAt))
		emitCronFailedNotify(ft.Id, remark, err.Error())
	} else {
		log.Printf("定时任务完成: %s，耗时 %s", remark, time.Since(startedAt))
	}
}

// RunTaskOnce 立即手动执行一次任务
func RunTaskOnce(id string) error {
	ft, err := repository.GetFilmTaskById(id)
	if err != nil {
		return err
	}
	if _, alreadyRunning := runningCronTasks.LoadOrStore(ft.Id, struct{}{}); alreadyRunning {
		return fmt.Errorf("定时任务正在执行中，请等待当前执行完成")
	}
	go func() {
		defer runningCronTasks.Delete(ft.Id)
		defer func() {
			if r := recover(); r != nil {
				log.Printf("[Spider] Cron task panic recovered: task=%s err=%v", ft.Id, r)
			}
		}()
		runTaskBody(ft)
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

// TaskPrev 上次真正开工的时间。排队和等待让路不算，调度触发和手动执行都算。
func TaskPrev(taskId string) time.Time {
	prev := GetEntryByTaskId(taskId).Prev
	if v, ok := taskRanAt.Load(taskId); ok {
		if at, ok := v.(time.Time); ok && at.After(prev) {
			return at
		}
	}
	return prev
}
