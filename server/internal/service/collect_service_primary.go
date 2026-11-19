package service

import (
	"errors"
	"strings"

	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/spider/progress"
)

// ErrPrimaryMismatch 写操作携带或归属的站点与当前主站不一致（D3）。
var ErrPrimaryMismatch = errors.New("主站已变更，请刷新后重试")

// CurrentPrimary 当前主站。没有主站时返回 nil。
func (s *CollectService) CurrentPrimary() *model.FilmSource {
	return repository.PickPrimarySourceForCategory()
}

// SwitchPrimary 切换主站（全站唯一入口；路由挂 AdminAccess，仅超管可达，D4）。
func (s *CollectService) SwitchPrimary(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("主站 ID 不能为空")
	}
	return withPrimaryMutation(func(searchBusy bool) error {
		oldPrimary := repository.PickPrimarySourceForCategory()
		if oldPrimary != nil && oldPrimary.Id == id {
			return nil // 已是当前主站，幂等返回
		}
		if searchBusy {
			return filmsnapshot.ErrSearchIndexBusy
		}
		if err := validateNewPrimary(id, ""); err != nil {
			return err
		}
		if err := repository.SetPrimarySourceID(id); err != nil {
			return err
		}
		applyPrimaryChanged(oldPrimary, repository.PickPrimarySourceForCategory())
		return nil
	})
}

// validateNewPrimary 校验切换目标：存在、启用、未删除，且不是正在停用/删除的站。
func validateNewPrimary(newPrimaryID, leavingID string) error {
	id := strings.TrimSpace(newPrimaryID)
	if id == "" {
		return errors.New("请指定新主站")
	}
	if id == leavingID {
		return errors.New("新主站不能是正在停用或删除的站点")
	}
	src := repository.FindCollectSourceById(id)
	if src == nil {
		return errors.New("新主站不存在")
	}
	if !src.State {
		return errors.New("新主站必须处于启用状态")
	}
	if progress.IsSourceDeleting(id) {
		return errors.New("新主站正在删除")
	}
	return nil
}

// planPrimaryHandover 校验停用/删除当前主站时的主站交接（D6）。
// 交接会触发索引重建，检索忙碌时直接拒绝。
func planPrimaryHandover(searchBusy bool, leavingID, newPrimaryID string, confirmClearPrimary bool) error {
	leavingID = strings.TrimSpace(leavingID)
	excluded := make(map[string]struct{}, 1)
	if leavingID != "" {
		excluded[leavingID] = struct{}{}
	}
	return planPrimaryHandoverExcluding(searchBusy, excluded, leavingID, newPrimaryID, confirmClearPrimary)
}

// planPrimaryHandoverExcluding 校验停用/删除当前主站时的主站交接，排除一组正在操作的站点。
func planPrimaryHandoverExcluding(searchBusy bool, excludeIDs map[string]struct{}, leavingID, newPrimaryID string, confirmClearPrimary bool) error {
	if searchBusy {
		return filmsnapshot.ErrSearchIndexBusy
	}
	if strings.TrimSpace(newPrimaryID) != "" {
		if _, excluded := excludeIDs[newPrimaryID]; excluded {
			return errors.New("新主站不能是正在停用或删除的站点")
		}
		return validateNewPrimary(newPrimaryID, leavingID)
	}
	if !confirmClearPrimary {
		return errors.New("停用或删除主站前需指定新主站，或确认置空")
	}
	// 只有排除本次操作的所有站点后，没有其他启用站才允许置空主站
	others := repository.FindOtherEnabledCollectSourcesExcluding(excludeIDs)
	if len(others) > 0 {
		return errors.New("仍有其他启用站，请指定新主站")
	}
	return nil
}

// applyPrimaryHandover 站点状态已变更后写入主站交接并触发主站变化处理。
func applyPrimaryHandover(oldPrimary *model.FilmSource, leavingID, newPrimaryID string, confirmClearPrimary bool) error {
	if strings.TrimSpace(newPrimaryID) != "" {
		if err := repository.SetPrimarySourceID(newPrimaryID); err != nil {
			return err
		}
		applyPrimaryChanged(oldPrimary, repository.PickPrimarySourceForCategory())
		return nil
	}
	if err := repository.ClearPrimarySourceState(); err != nil {
		return err
	}
	applyPrimaryChanged(oldPrimary, nil)
	return nil
}
