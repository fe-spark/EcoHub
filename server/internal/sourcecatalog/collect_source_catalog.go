package sourcecatalog

import (
	"errors"
	"log"
	"strings"
	"sync"
	"sync/atomic"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

type collectSourceCatalogSnapshot struct {
	dbPtr *gorm.DB
	list  []model.FilmSource
}

var (
	collectSourceCatalog atomic.Pointer[collectSourceCatalogSnapshot]
	collectCatalogMu     sync.Mutex
)

// RefreshCollectSourceCatalog 用当前库里的采集站覆盖内存目录。查询失败保留原目录。
func RefreshCollectSourceCatalog() error {
	conn := db.Mdb
	if conn == nil {
		return errors.New("采集站目录刷新失败")
	}
	collectCatalogMu.Lock()
	defer collectCatalogMu.Unlock()
	return refreshCollectSourceCatalogLocked(conn)
}

func refreshCollectSourceCatalogLocked(conn *gorm.DB) error {
	if conn == nil {
		return errors.New("采集站目录刷新失败")
	}
	list := make([]model.FilmSource, 0)
	if err := conn.Order("sort ASC, created_at ASC, id ASC").Find(&list).Error; err != nil {
		return err
	}
	collectSourceCatalog.Store(&collectSourceCatalogSnapshot{
		dbPtr: conn,
		list:  list,
	})
	InvalidatePrimarySourceCache()
	return nil
}

// InvalidateCollectSourceCatalog 清空采集站内存目录与主站缓存，下次读取时重查库。
func InvalidateCollectSourceCatalog() {
	collectCatalogMu.Lock()
	collectSourceCatalog.Store(nil)
	collectCatalogMu.Unlock()
	InvalidatePrimarySourceCache()
}

func ensureCollectSourceCatalog() (*collectSourceCatalogSnapshot, error) {
	conn := db.Mdb
	if conn == nil {
		return nil, errors.New("采集站目录刷新失败")
	}
	if snap := collectSourceCatalog.Load(); snap != nil && snap.dbPtr == conn {
		return snap, nil
	}
	collectCatalogMu.Lock()
	defer collectCatalogMu.Unlock()
	if snap := collectSourceCatalog.Load(); snap != nil && snap.dbPtr == conn {
		return snap, nil
	}
	if err := refreshCollectSourceCatalogLocked(conn); err != nil {
		return nil, err
	}
	return collectSourceCatalog.Load(), nil
}

// ForgetCollectSource 站点行已经删掉、目录重读失败时，从当前目录摘掉这一条。
func ForgetCollectSource(id string) {
	collectCatalogMu.Lock()
	defer collectCatalogMu.Unlock()
	InvalidatePrimarySourceCache()
	cur := collectSourceCatalog.Load()
	if cur == nil {
		return
	}
	next := make([]model.FilmSource, 0, len(cur.list))
	for _, src := range cur.list {
		if src.Id != id {
			next = append(next, src)
		}
	}
	collectSourceCatalog.Store(&collectSourceCatalogSnapshot{
		dbPtr: cur.dbPtr,
		list:  next,
	})
}

// FindCollectSourceForDelete 只查内存目录。目录还没装过时先装一次。
func FindCollectSourceForDelete(id string) *model.FilmSource {
	id = strings.TrimSpace(id)
	if id == "" {
		return nil
	}
	snap, err := ensureCollectSourceCatalog()
	if err != nil || snap == nil {
		return nil
	}
	for i := range snap.list {
		if snap.list[i].Id == id {
			src := snap.list[i]
			return &src
		}
	}
	return nil
}

// HasCollectSource 从进程内站点目录判断采集站是否存在，避免高频只读路径逐次查库。
func HasCollectSource(id string) bool {
	return FindCollectSourceForDelete(id) != nil
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

// GetCollectSourceListTx 在事务内获取采集站 API 列表（按排序序号升序）。
func GetCollectSourceListTx(tx *gorm.DB) []model.FilmSource {
	conn := tx
	if conn == nil {
		conn = db.Mdb
	}
	if conn == nil {
		return nil
	}
	var list []model.FilmSource
	if err := conn.Order("sort ASC, created_at ASC, id ASC").Find(&list).Error; err != nil {
		log.Println("GetCollectSourceList Error:", err)
		return nil
	}
	return list
}

// GetCollectSourceList 获取采集站 API 列表（优先命中内存目录，避免每页写库重复查表）。
func GetCollectSourceList() []model.FilmSource {
	snap, err := ensureCollectSourceCatalog()
	if err != nil {
		return GetCollectSourceListTx(nil)
	}
	if snap == nil {
		return nil
	}
	out := make([]model.FilmSource, len(snap.list))
	copy(out, snap.list)
	return out
}
