package spider

import (
	"errors"
	"fmt"
	"log"
	"sync"

	"server/internal/model"
	"server/internal/repository"
	filmrepo "server/internal/repository/film"

	"github.com/robfig/cron/v3"
)

var CronCollect *cron.Cron = CreateCron()

// taskCidMap 运行时内存注册表：task.Id → cron.EntryID
// Cid 是内存值，不持久化到 DB，每次重启重新注册
var taskCidMap = make(map[string]cron.EntryID)
var taskCidLock sync.RWMutex

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

// AddFilmUpdateCron 添加 指定站点的影片更新定时任务
func AddFilmUpdateCron(id, spec string) (cron.EntryID, error) {
	// 校验 spec 表达式的有效性
	if err := ValidSpec(spec); err != nil {
		return -99, errors.New(fmt.Sprint("定时任务添加失败,Cron表达式校验失败: ", err.Error()))
	}
	return CronCollect.AddFunc(spec, func() {
		// 通过创建任务时生成的 Id 获取任务相关数据
		ft, err := repository.GetFilmTaskById(id)
		if err != nil {
			log.Println("FilmCollectCron Exec Failed: ", err)
			return
		}
		executeTask(ft)
	})
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

// AddFilmRecoverCron 失败采集记录处理
func AddFilmRecoverCron(id, spec string) (cron.EntryID, error) {
	// 校验 spec 表达式的有效性
	if err := ValidSpec(spec); err != nil {
		return -99, errors.New(fmt.Sprint("定时任务添加失败,Cron表达式校验失败: ", err.Error()))
	}
	return CronCollect.AddFunc(spec, func() {
		// 通过 Id 获取任务相关数据
		ft, err := repository.GetFilmTaskById(id)
		if err != nil {
			log.Println("FilmRecoverCron Exec Failed: ", err)
			return
		}
		executeTask(ft)
	})
}

// RemoveCron 删除定时任务
func RemoveCron(id cron.EntryID) {
	// 通过定时任务EntryID移出对应的定时任务
	CronCollect.Remove(id)
}

// GetEntryById 返回定时任务的相关时间信息
func GetEntryById(id cron.EntryID) cron.Entry {
	return CronCollect.Entry(id)
}

// ValidSpec 校验cron表达式是否有效
func ValidSpec(spec string) error {
	parser := cron.NewParser(cron.Second | cron.Minute | cron.Hour | cron.Dom | cron.Month | cron.Dow | cron.Descriptor)
	_, err := parser.Parse(spec)
	return err
}

// AddOrphanCleanCron 添加附属站播放列表孤儿清理定时任务。
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
	case 0:
		cid, err = AddAutoUpdateCron(ft.Id, ft.Spec)
	case 1:
		cid, err = AddFilmUpdateCron(ft.Id, ft.Spec)
	case 2:
		cid, err = AddFilmRecoverCron(ft.Id, ft.Spec)
	case 3:
		cid, err = AddOrphanCleanCron(ft.Id, ft.Spec)
	default:
		return fmt.Errorf("不支持的定时任务类型: %d", ft.Model)
	}

	if err != nil {
		return err
	}

	RegisterTaskCid(id, cid)
	return nil
}

// executeTask 执行特定的定时任务逻辑（内部统一调用）
func executeTask(ft model.FilmCollectTask) {
	if !ft.State {
		return
	}

	log.Printf("开始执行定时任务: Task[%s] Model[%d]\n", ft.Id, ft.Model)

	switch ft.Model {
	case 0: // 自动更新已启用站点
		AutoCollect(ft.Time)
		log.Println("执行一次自动更新任务")
	case 1: // 更新指定资源站
		if len(ft.Ids) == 0 {
			log.Printf("定时任务[%s]未配置资源站，跳过执行\n", ft.Id)
			return
		}
		BatchCollect(ft.Time, ft.Ids...)
	case 2: // 失败采集恢复
		FullRecoverSpider()
		log.Println("执行一次失败采集恢复任务")
	case 3: // 附属站播放列表孤儿清理
		activeIDs := GetActiveTasks()
		if len(activeIDs) > 0 {
			masters := repository.GetCollectSourceListByGrade(model.MasterCollect)
			masterIDSet := make(map[string]struct{}, len(masters))
			for _, master := range masters {
				masterIDSet[master.Id] = struct{}{}
			}
			for _, activeID := range activeIDs {
				if _, isMaster := masterIDSet[activeID]; isMaster {
					log.Println("[CleanOrphan] 主站正在采集中，跳过本次孤儿清理，等待下次执行")
					return
				}
			}
		}
		n := filmrepo.CleanOrphanPlaylists()
		m := filmrepo.CleanEmptyFilms()
		log.Printf("执行一次数据清理任务，删除了 %d 条孤儿记录和 %d 条空记录\n", n, m)
	default:
		log.Printf("定时任务[%s]类型[%d]已废弃，跳过执行\n", ft.Id, ft.Model)
	}

	log.Printf("定时任务执行完毕: Task[%s]\n", ft.Id)
}

// RunTaskOnce 立即手动执行一次任务
func RunTaskOnce(id string) {
	go func() {
		ft, err := repository.GetFilmTaskById(id)
		if err != nil {
			log.Println("RunTaskOnce Failed: ", err)
			return
		}
		// 手动触发不需要再次检查 State，因为通常是在开启时主动调用
		// 但为了安全，底层的 executeTask 依然会检查
		executeTask(ft)
	}()
}
