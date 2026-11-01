package repository

import (
	"errors"
	"strings"
	"sync/atomic"

	"server/internal/infra/db"
	"server/internal/model"
)

var collectSourceCatalog atomic.Pointer[[]model.FilmSource]

// RefreshCollectSourceCatalog 用当前库里的采集站覆盖内存目录。查询失败保留原目录。
func RefreshCollectSourceCatalog() error {
	if db.Mdb == nil {
		return errors.New("采集站目录刷新失败")
	}
	list := make([]model.FilmSource, 0)
	if err := db.Mdb.Order("sort ASC, created_at ASC, id ASC").Find(&list).Error; err != nil {
		return err
	}
	collectSourceCatalog.Store(&list)
	return nil
}

// forgetCollectSource 站点行已经删掉、目录重读失败时，从当前目录摘掉这一条。
func forgetCollectSource(id string) {
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
	collectSourceCatalog.CompareAndSwap(cur, &next)
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
