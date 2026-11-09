package spider

import (
	"fmt"
	"log"
	"strings"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/model"
	"server/internal/repository"
)

// 维护任务因闸门被占用而跳过时，不再直接丢掉本轮，而是排队等闸门空出来后补跑一次。
// 同一任务只排一份（去重），最长等待 maintenanceRetryMaxWait，超时发送告警。
var (
	maintenanceRetryPoll    = 5 * time.Second
	maintenanceRetryMaxWait = 2 * time.Hour
	deferredMaintenance     sync.Map // task id -> *deferredMaintenanceState

	// startDeferredMaintenanceHook 仅供测试替换补跑协程的启动方式；为 nil 时正常启动。
	startDeferredMaintenanceHook func(ft model.FilmCollectTask, st *deferredMaintenanceState)
)

type deferredMaintenanceState struct {
	busyAgain atomic.Bool
}

// deferMaintenance 登记一次待补跑的维护。已在排队时只标记“又被跳过一次”，不重复排队。
func deferMaintenance(ft model.FilmCollectTask) {
	if ft.Id == "" || !isMaintenanceModel(ft.Model) {
		return
	}
	st := &deferredMaintenanceState{}
	if v, loaded := deferredMaintenance.LoadOrStore(ft.Id, st); loaded {
		v.(*deferredMaintenanceState).busyAgain.Store(true)
		return
	}
	log.Printf("[Cron] 维护任务 %s 因已有维护在执行（%s）延后，闸门空闲后补跑", ft.Id, maintenanceRunningDetail())
	if startDeferredMaintenanceHook != nil {
		startDeferredMaintenanceHook(ft, st)
		return
	}
	go runDeferredMaintenance(ft, st)
}

func isMaintenanceDeferred(id string) bool {
	_, ok := deferredMaintenance.Load(id)
	return ok
}

func runDeferredMaintenance(ft model.FilmCollectTask, st *deferredMaintenanceState) {
	defer deferredMaintenance.Delete(ft.Id)
	deadline := time.Now().Add(maintenanceRetryMaxWait)
	ticker := time.NewTicker(maintenanceRetryPoll)
	defer ticker.Stop()
	for range ticker.C {
		if isMaintenanceRunning() || IsCronTaskRunning(ft.Id) {
			if !time.Now().Before(deadline) {
				notifyDeferredTimeout(ft)
				return
			}
			continue
		}
		if latest, err := repository.GetFilmTaskById(ft.Id); err == nil {
			ft = latest
		}
		if !ft.State || !isMaintenanceModel(ft.Model) {
			log.Printf("[Cron] 维护任务 %s 已停用或类型变更，取消补跑", ft.Id)
			return
		}
		st.busyAgain.Store(false)
		log.Printf("[Cron] 维护闸门已空闲，补跑维护任务 %s", ft.Id)
		runCronTaskSafely(ft)
		if !st.busyAgain.Load() {
			return
		}
		if !time.Now().Before(deadline) {
			notifyDeferredTimeout(ft)
			return
		}
	}
}

func notifyDeferredTimeout(ft model.FilmCollectTask) {
	remark := strings.TrimSpace(ft.Remark)
	if remark == "" {
		remark = ft.Id
	}
	reason := fmt.Sprintf("等待其他维护任务结束超过 %s，本轮未执行（%s）", maintenanceRetryMaxWait, maintenanceRunningDetail())
	log.Printf("[Cron] %s: %s", remark, reason)
	emitCronFailedNotify(ft.Id, remark, reason)
}
