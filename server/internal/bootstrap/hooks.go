package bootstrap

import (
	"server/internal/access"
	"server/internal/collect"
	"server/internal/film"
	"server/internal/model"
	"server/internal/notify"
	"server/internal/portal"
)

func init() {
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
	film.OnResetCollectStats = collect.ResetCollectStatsCoalescer
	film.OnRefreshFilms = collect.SpiderSvc.CollectFilms
	film.IsFilmCollectingFn = collect.IsCollecting
	collect.OnPrimarySwitched = func(targetSourceID string) error {
		_, err := portal.IndexSvc.IndexPage(targetSourceID)
		return err
	}
	collect.OnResetDisableAccess = func() { access.SetEnabled(false) }
	collect.OnResetClearAccess = func() error {
		_, err := access.ClearAccessData(0)
		return err
	}
	collect.OnResetRecoverAccess = func() { _ = InitSvc.AnalyticsConfigInit() }
	collect.OnResetRecoverCrontab = func() { _ = InitSvc.CollectCrontabInit() }
	collect.OnResetReinitSite = InitSvc.ReinitAfterReset
}
