package repobase

import (
	"errors"
	"strings"
	"sync"
	"sync/atomic"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

var (
	collectSourceCatalog atomic.Pointer[[]model.FilmSource]
	collectCatalogMu     sync.Mutex
)

// RefreshCollectSourceCatalog 用当前库里的采集站覆盖内存目录。查询失败保留原目录。
func RefreshCollectSourceCatalog() error {
	if db.Mdb == nil {
		return errors.New("采集站目录刷新失败")
	}
	collectCatalogMu.Lock()
	defer collectCatalogMu.Unlock()
	list := make([]model.FilmSource, 0)
	if err := db.Mdb.Order("sort ASC, created_at ASC, id ASC").Find(&list).Error; err != nil {
		return err
	}
	collectSourceCatalog.Store(&list)
	InvalidatePrimarySourceCache()
	return nil
}

// forgetCollectSource 站点行已经删掉、目录重读失败时，从当前目录摘掉这一条。
func ForgetCollectSource(id string) {
	InvalidatePrimarySourceCache()
	for {
		cur := collectSourceCatalog.Load()
		if cur == nil {
			return
		}
		next := make([]model.FilmSource, 0, len(*cur))
		for _, src := range *cur {
			if src.Id != id {
				next = append(next, src)
			}
		}
		if collectSourceCatalog.CompareAndSwap(cur, &next) {
			return
		}
	}
}

// FindCollectSourceForDelete 只查内存目录。目录还没装过时先装一次。
func FindCollectSourceForDelete(id string) *model.FilmSource {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	list := collectSourceCatalog.Load()
	if list == nil {
		if err := RefreshCollectSourceCatalog(); err != nil {
			return nil
		}
		list = collectSourceCatalog.Load()
	}
	if list == nil {
		return nil
	}
	for i := range *list {
		if (*list)[i].Id == id {
			src := (*list)[i]
			return &src
		}
	}
	return nil
}

// GetCollectSourceListDB 使用指定数据库连接获取采集站 API 列表（按排序序号升序）。
func GetCollectSourceListDB(connDB *gorm.DB) ([]model.FilmSource, error) {
	if connDB == nil {
		return nil, errors.New("数据库未连接")
	}
	list := make([]model.FilmSource, 0)
	if err := connDB.Order("sort ASC, created_at ASC, id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// GetEnabledCollectSourceList 获取已启用采集站列表（按排序序号升序）。
func GetEnabledCollectSourceList() ([]model.FilmSource, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	list := make([]model.FilmSource, 0)
	if err := db.Mdb.Where("state = ?", true).Order("sort ASC, created_at ASC, id ASC").Find(&list).Error; err != nil {
		return nil, err
	}
	return list, nil
}

// DeleteCollectSourceStatsTx 在事务内删除指定采集站的运行统计行。
func DeleteCollectSourceStatsTx(tx *gorm.DB, sourceIDs ...string) error {
	ids := make([]string, 0, len(sourceIDs))
	seen := make(map[string]struct{}, len(sourceIDs))
	for _, sourceID := range sourceIDs {
		sourceID = strings.TrimSpace(sourceID)
		if sourceID == "" {
			continue
		}
		if _, ok := seen[sourceID]; ok {
			continue
		}
		seen[sourceID] = struct{}{}
		ids = append(ids, sourceID)
	}
	if len(ids) == 0 {
		return nil
	}
	return tx.Where("source_id IN ?", ids).Unscoped().Delete(&model.CollectSourceStats{}).Error
}

// FindCollectSourceById 通过 Id 标识获取对应的资源站信息。
func FindCollectSourceById(id string) *model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	var fs model.FilmSource
	if err := db.Mdb.Where("id = ?", id).First(&fs).Error; err != nil {
		return nil
	}
	return &fs
}
