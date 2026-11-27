package sourcecatalog

import (
	"strings"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"
)

var (
	sourceDeletingCheckerMu sync.RWMutex
	sourceDeletingChecker   func(string) bool
)

// RegisterSourceDeletingChecker 注册站点删除运行态检查器，供选主站跳过正在删除的站点。
func RegisterSourceDeletingChecker(fn func(string) bool) {
	sourceDeletingCheckerMu.Lock()
	sourceDeletingChecker = fn
	sourceDeletingCheckerMu.Unlock()
}

func isSourceDeleting(id string) bool {
	id = strings.TrimSpace(id)
	if id == "" {
		return false
	}
	sourceDeletingCheckerMu.RLock()
	fn := sourceDeletingChecker
	sourceDeletingCheckerMu.RUnlock()
	return fn != nil && fn(id)
}

// IsSourceDeleting 查询站点是否正处于异步删除队列中。
func IsSourceDeleting(id string) bool {
	return isSourceDeleting(id)
}

// pickEnabledCollectSource 返回排序最前且未在删除中的启用站（主站为空时的懒初始化来源）。
func pickEnabledCollectSource() *model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var list []model.FilmSource
	if err := db.Mdb.Where("state = ?", true).Order("sort ASC, created_at ASC, id ASC").Find(&list).Error; err != nil {
		return nil
	}
	for i := range list {
		if list[i].Id == "" || isSourceDeleting(list[i].Id) {
			continue
		}
		return &list[i]
	}
	return nil
}
