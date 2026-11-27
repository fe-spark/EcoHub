package collectctl

import (
	"errors"
	"server/internal/film/catalog"
	"server/internal/repobase"
	"server/internal/sourcecatalog"
	"strings"
	"time"

	"server/internal/collect"
	"server/internal/collect/progress"
	filmrepo "server/internal/film"
	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/syslog"
	"server/internal/model"
)

// deleteIndexWait 删除前等待已经在进行的检索索引、封口和主站预热。
const deleteIndexWait = 30 * time.Minute

func deleteIdleReason() string {
	reason := collect.SearchWorkBusyReason()
	if !filmsnapshot.IsPrimaryWarming() {
		return reason
	}
	if reason == "" {
		return "主站预热"
	}
	return reason + "，主站预热"
}

func waitDeleteIdle(id string, timeout time.Duration) error {
	reason := deleteIdleReason()
	if reason == "" {
		syslog.Infof("[Collect] 删除站点检索空闲 id=%s", id)
		return nil
	}
	if timeout <= 0 {
		syslog.Errorf("[Collect] 删除站点检索仍在忙且无等待上限 id=%s 原因=%s", id, reason)
		return errors.New("检索索引更新超时，未删除")
	}
	syslog.Infof("[Collect] 删除站点等待检索结束 id=%s 原因=%s 上限=%s", id, reason, timeout)
	started := time.Now()
	deadline := time.Now().Add(timeout)
	for {
		reason = deleteIdleReason()
		if reason == "" {
			syslog.Infof("[Collect] 删除站点检索已结束 id=%s 耗时=%s", id, time.Since(started).Round(time.Millisecond))
			return nil
		}
		if !time.Now().Before(deadline) {
			syslog.Errorf("[Collect] 删除站点等待检索超时 id=%s 仍在=%s 耗时=%s", id, reason, time.Since(started).Round(time.Millisecond))
			return errors.New("检索索引更新超时，未删除")
		}
		time.Sleep(100 * time.Millisecond)
	}
}

func validateSourceForDelete(id string) (*model.FilmSource, error) {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil, errors.New("采集站ID不能为空")
	}
	src := sourcecatalog.FindCollectSourceForDelete(id)
	if src == nil {
		return nil, errors.New("当前资源站信息不存在, 请勿重复操作")
	}
	if progress.IsTaskRunning(id) {
		status := ""
		if snap, ok := progress.Snapshot(id); ok {
			status = snap.Status
		}
		syslog.Infof("[Collect] 拒绝删除，站点采集中 id=%s name=%s status=%s", id, src.Name, status)
		return nil, errors.New("采集中，暂不可删除")
	}
	if progress.IsSourceDeleting(id) {
		return nil, errors.New("该站正在删除")
	}
	if progress.IsCategorySyncing(id) {
		return nil, errors.New("分类采集中，暂不可删除")
	}
	return src, nil
}

// TriggerDelFilmSource 异步触发采集站删除：广播 deleting 状态并毫秒级返回，后台串行队列安全停工与级联清理。
// 删除当前主站必须携带 newPrimaryID，或无其他启用站时 confirmClearPrimary 置空（D6）。
func (s *CollectService) TriggerDelFilmSource(id, newPrimaryID string, confirmClearPrimary bool) error {
	return s.TriggerDelFilmSourceWithAuth(id, newPrimaryID, confirmClearPrimary, true)
}

// TriggerDelFilmSourceWithAuth 异步触发单个采集站删除，并按 allowPrimaryHandover 校验删除主站的权限（D4）。
func (s *CollectService) TriggerDelFilmSourceWithAuth(id, newPrimaryID string, confirmClearPrimary, allowPrimaryHandover bool) error {
	if !allowPrimaryHandover && (strings.TrimSpace(newPrimaryID) != "" || confirmClearPrimary) {
		return ErrPrimarySwitchForbidden
	}
	// D6：删除当前主站，在准入锁内完成校验与主站交接，消除并发切主与删除竞态
	return filmsnapshot.WithPrimaryMutation(func(searchBusy bool) error {
		src, err := validateSourceForDelete(id)
		if err != nil {
			return err
		}

		// 必须在 reserveDelFilmSources 置位 deleting 之前读取当前主站，避免缓存冷态时被误判为 nil
		oldPrimary := sourcecatalog.CurrentPrimarySource()
		isPrimaryTarget := oldPrimary != nil && oldPrimary.Id == src.Id
		targetNewPrimary := strings.TrimSpace(newPrimaryID)
		if isPrimaryTarget {
			if !allowPrimaryHandover {
				return ErrPrimarySwitchForbidden
			}
			if err := planPrimaryHandover(searchBusy, src.Id, targetNewPrimary, confirmClearPrimary); err != nil {
				return err
			}
		}

		res, err := reserveDelFilmSources(deleteSourceQueue, []*model.FilmSource{src})
		if err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				res.rollback()
			}
		}()

		clearPrimarySourceID := ""
		if isPrimaryTarget {
			if targetNewPrimary != "" {
				if err := applyPrimaryHandover(oldPrimary, src.Id, targetNewPrimary, false); err != nil {
					return err
				}
			} else if confirmClearPrimary {
				// 无其他启用站需置空主站时，推迟到真正出队删除前再清空，防止排队被撤回后导致索引与主站状态撕裂
				clearPrimarySourceID = src.Id
			}
		}
		res.commit(nil, clearPrimarySourceID)
		committed = true
		return nil
	})
}

// TriggerDelFilmSourceBatch 异步批量触发采集站删除。任一站校验不通过时整批拒绝，不触发部分删除。
// 批量包含当前主站时：将主站平滑交接并排在队列最后一位删除。
func (s *CollectService) TriggerDelFilmSourceBatch(ids []string, newPrimaryID string, confirmClearPrimary bool) error {
	return s.TriggerDelFilmSourceBatchWithAuth(ids, newPrimaryID, confirmClearPrimary, true)
}

// TriggerDelFilmSourceBatchWithAuth 异步批量触发采集站删除，并按 allowPrimaryHandover 校验删除主站的权限（D4）。
func (s *CollectService) TriggerDelFilmSourceBatchWithAuth(ids []string, newPrimaryID string, confirmClearPrimary, allowPrimaryHandover bool) error {
	if len(ids) == 1 {
		return s.TriggerDelFilmSourceWithAuth(ids[0], newPrimaryID, confirmClearPrimary, allowPrimaryHandover)
	}
	if !allowPrimaryHandover && (strings.TrimSpace(newPrimaryID) != "" || confirmClearPrimary) {
		return ErrPrimarySwitchForbidden
	}
	return filmsnapshot.WithPrimaryMutation(func(searchBusy bool) error {
		targets := make([]*model.FilmSource, 0, len(ids))
		seen := make(map[string]struct{}, len(ids))
		var failed []string
		for _, raw := range ids {
			id := strings.TrimSpace(raw)
			if id == "" {
				continue
			}
			if _, ok := seen[id]; ok {
				continue
			}
			seen[id] = struct{}{}
			src, err := validateSourceForDelete(id)
			if err != nil {
				failed = append(failed, err.Error())
				continue
			}
			targets = append(targets, src)
		}
		if len(failed) > 0 {
			return errors.New(strings.Join(failed, "；"))
		}
		if len(targets) == 0 {
			return errors.New("采集站ID不能为空")
		}

		// 必须在 reserveDelFilmSources 置位 deleting 之前读取当前主站，避免缓存冷态时被误判为 nil
		oldPrimary := sourcecatalog.CurrentPrimarySource()
		var primaryTarget *model.FilmSource
		if oldPrimary != nil {
			for _, target := range targets {
				if target.Id == oldPrimary.Id {
					primaryTarget = target
					break
				}
			}
		}

		targetNewPrimary := strings.TrimSpace(newPrimaryID)
		if primaryTarget != nil {
			if !allowPrimaryHandover {
				return ErrPrimarySwitchForbidden
			}
			otherEnabled := sourcecatalog.FindOtherEnabledCollectSourcesExcluding(seen)
			if targetNewPrimary != "" {
				if err := planPrimaryHandoverExcluding(searchBusy, seen, primaryTarget.Id, targetNewPrimary, false); err != nil {
					return err
				}
			} else if len(otherEnabled) > 0 {
				return errors.New("仍有其他启用站，请指定新主站")
			} else {
				if err := planPrimaryHandoverExcluding(searchBusy, seen, primaryTarget.Id, "", confirmClearPrimary); err != nil {
					return err
				}
			}
		}

		res, err := reserveDelFilmSources(deleteSourceQueue, targets)
		if err != nil {
			return err
		}
		committed := false
		defer func() {
			if !committed {
				res.rollback()
			}
		}()

		clearPrimarySourceID := ""
		if primaryTarget != nil {
			if targetNewPrimary != "" {
				if err := applyPrimaryHandover(oldPrimary, primaryTarget.Id, targetNewPrimary, false); err != nil {
					return err
				}
			} else if confirmClearPrimary {
				// 推迟到队尾主站出队开始清理时再清空主站与丢弃检索索引，避免撤回队列时撕裂主站状态
				clearPrimarySourceID = primaryTarget.Id
			}

			// 核心：非主站先删，当前主站排在队列最后一位删除
			nonPrimaryTargets := make([]*model.FilmSource, 0, len(targets))
			for _, target := range targets {
				if target.Id != primaryTarget.Id {
					nonPrimaryTargets = append(nonPrimaryTargets, target)
				}
			}
			targets = append(nonPrimaryTargets, primaryTarget)
		}

		res.commit(targets, clearPrimarySourceID)
		committed = true
		return nil
	})
}

func (s *CollectService) DelFilmSource(id string) (int, error) {
	return s.delFilmSource(id)
}

func (s *CollectService) delFilmSource(id string) (int, error) {
	if !progress.IsSourceDeleting(id) {
		progress.SetSourceDeleting(id, true)
		defer progress.SetSourceDeleting(id, false)
	}
	if err := waitDeleteIdle(id, deleteIndexWait); err != nil {
		return 0, err
	}
	deletedCount := 0
	err := collect.WithSourceExclusive(id, func() error {
		src := sourcecatalog.FindCollectSourceById(id)
		if src == nil {
			return errors.New("当前资源站信息不存在, 请勿重复操作")
		}
		if progress.IsTaskRunning(id) {
			status := ""
			if snap, ok := progress.Snapshot(id); ok {
				status = snap.Status
			}
			syslog.Infof("[Collect] 删除执行时站点已在采集中，放弃 id=%s name=%s status=%s", id, src.Name, status)
			return errors.New("采集中，暂不可删除")
		}
		release, err := collect.StopSourceForDelete(id)
		if err != nil {
			return err
		}
		defer release()
		orphanMids, err := collect.DelCollectResource(id)
		if err != nil {
			if len(orphanMids) > 0 {
				filmsnapshot.InvalidateCachesAfterSourceDelete(id)
			}
			return err
		}
		deletedCount = len(orphanMids)
		filmsnapshot.InvalidateCachesAfterSourceDelete(id)
		if len(orphanMids) > 0 {
			syslog.Infof("[Collect] 删除采集源 %s(%s)，已删除该站档案 %d 部", src.Name, id, len(orphanMids))
		}
		collect.ResetSourceThrottle(id)
		repobase.ClearProvideNetworkConfigCache()
		catalog.MarkCategoryChanged()
		progress.BroadcastCategoryStatus(id, "idle")
		return nil
	})
	if err != nil {
		return 0, err
	}
	collect.DropSourceRuntime(id)
	filmrepo.RefreshInventoryStatsAsync()
	return deletedCount, nil
}
