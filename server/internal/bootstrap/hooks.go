package bootstrap

import (
	"sync"

	"server/internal/analytics"
	"server/internal/collect"
	"server/internal/collectctl"
	"server/internal/film"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/portal"
	"server/internal/recommend"
	"server/internal/siteops"
)

var wireOnce sync.Once

// Wire 装配跨业务域的运行时回调钩子，幂等执行。
func Wire() {
	wireOnce.Do(func() {
		collect.RegisterNotifyDispatcher(collect.NotifyDispatcher{
			OnBatchSummary: func(payload model.CollectBatchNotifyPayload) {
				notify.NotifySvc.PublishBatchSummary(payload)
			},
			OnSourceFailed: func(sourceID, sourceName, reason string) {
				notify.NotifySvc.PublishSourceFailed(sourceID, sourceName, reason)
			},
			OnCronFailed: func(taskID, remark, reason string) {
				notify.NotifySvc.PublishCronFailed(taskID, remark, reason)
			},
		})
		film.OnRefreshFilms = collect.SpiderSvc.CollectFilms
		film.IsFilmCollectingFn = collect.IsCollecting
		film.SetInventoryStatsCollectingChecker(collect.IsCollecting)
		collect.RegisterCollectDoneHook(func() {
			film.RefreshInventoryStatsAsync()
			// 等采集批次与检索索引封口全部结束后再同步自动榜单分组，确保刚入库的新片可立即命中。
			if !collect.IsCollecting() {
				recommend.HomeRecommendSvc.SyncAllAutoGroups()
			}
		})
		collectctl.OnPrimarySwitched = func(targetSourceID string) error {
			_, err := portal.IndexSvc.IndexPage(targetSourceID)
			return err
		}
		siteops.OnResetRecoverAccess = func() {
			analytics.Start()
			_ = InitSvc.AnalyticsConfigInit()
		}
		siteops.OnResetRecoverCrontab = func() { _ = InitSvc.CollectCrontabInit() }
		siteops.OnResetReinitSite = InitSvc.ReinitAfterReset
	})
}

func init() {
	Wire()
}
