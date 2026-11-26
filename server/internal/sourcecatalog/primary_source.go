package sourcecatalog

import (
	"errors"
	"strings"
	"sync/atomic"
	"time"

	"golang.org/x/sync/singleflight"
	"gorm.io/gorm"

	"server/internal/infra/db"
	"server/internal/infra/syslog"
	"server/internal/model"
)

// 主站是服务端唯一的全局状态（见《主站全局切换方案.md》2.1）：
//   - 存储为空且存在启用站 → 取排序首位启用站并持久化（升级初始化、D5）；
//   - 存储有效（存在、启用、未在删除中）→ 返回存储站；
//   - 存储指向停用/删除中的站 → 异常，告警并返回 nil，不自动换站（D6）。

type primaryCacheHolder struct {
	dbConn *gorm.DB
	source *model.FilmSource
}

type primaryLoadResult struct {
	source *model.FilmSource
	stale  bool
}

var (
	primaryCache    atomic.Pointer[primaryCacheHolder]
	primaryCacheGen atomic.Uint64
	primarySF       singleflight.Group

	// ErrPrimaryMismatch 写操作携带或归属的站点与当前主站不一致（D3）。
	ErrPrimaryMismatch = errors.New("主站已变更，请刷新后重试")
)

// InvalidatePrimarySourceCache 使主站内存缓存失效，下次读取时重新加载。
func InvalidatePrimarySourceCache() {
	primaryCacheGen.Add(1)
	primaryCache.Store(nil)
	primarySF.Forget("current_primary")
}

// StoredPrimarySourceID 读取持久化的主站 ID（不经过内存缓存，不校验删除态）。
func StoredPrimarySourceID() string {
	return storedPrimarySourceID()
}

func cloneFilmSource(src *model.FilmSource) *model.FilmSource {
	if src == nil {
		return nil
	}
	cpy := *src
	return &cpy
}

// CurrentPrimarySource 当前主站。
// 正常状态直接从原子快照读取；未命中或数据库连接变更时通过 singleflight 合并并发请求探活并懒加载。
func CurrentPrimarySource() *model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	if p := primaryCache.Load(); p != nil && p.dbConn == db.Mdb {
		if p.source != nil && isSourceDeleting(p.source.Id) {
			return nil
		}
		return cloneFilmSource(p.source)
	}
	for attempt := 0; attempt < 3; attempt++ {
		res, err, _ := primarySF.Do("current_primary", func() (any, error) {
			// 双检：并发排队请求不重复查库
			if p := primaryCache.Load(); p != nil && p.dbConn == db.Mdb {
				if p.source != nil && isSourceDeleting(p.source.Id) {
					return primaryLoadResult{source: nil}, nil
				}
				return primaryLoadResult{source: p.source}, nil
			}
			dbConn := db.Mdb
			src, gen := resolveCurrentPrimarySourceDB()
			if primaryCacheGen.Load() != gen {
				// 查询期间发生并发写操作导致缓存失效，禁止将可能陈旧的快照回填进缓存
				return primaryLoadResult{source: src, stale: true}, nil
			}
			if src != nil && !isSourceDeleting(src.Id) && dbConn == db.Mdb {
				primaryCache.Store(&primaryCacheHolder{
					dbConn: dbConn,
					source: src,
				})
			}
			return primaryLoadResult{source: src}, nil
		})
		if err != nil || res == nil {
			return nil
		}
		loaded := res.(primaryLoadResult)
		if loaded.stale && attempt < 2 {
			primarySF.Forget("current_primary")
			continue
		}
		if loaded.source != nil && isSourceDeleting(loaded.source.Id) {
			return nil
		}
		return cloneFilmSource(loaded.source)
	}
	return nil
}

func resolveCurrentPrimarySourceDB() (*model.FilmSource, uint64) {
	gen := primaryCacheGen.Load()
	if id := storedPrimarySourceID(); id != "" {
		return primarySourceByID(id), gen
	}
	first := pickEnabledCollectSource()
	if first == nil {
		return nil, gen
	}
	if err := SetPrimarySourceID(first.Id); err != nil {
		// 写库失败（如测试库未建该表）时仍返回正确主站，下次调用再试
		syslog.Errorf("[Primary] 主站状态持久化失败: %v", err)
		return first, gen
	}
	// SetPrimarySourceID 内部会递增 primaryCacheGen，记录写库后的基准版本再回读校验
	genAfterSet := primaryCacheGen.Load()
	return primarySourceByID(first.Id), genAfterSet
}

// CurrentPrimaryID 当前主站 ID。没有主站或主站处于不可用状态时返回空。
func CurrentPrimaryID() string {
	if s := CurrentPrimarySource(); s != nil {
		return s.Id
	}
	return ""
}

// SetPrimarySourceID 写入主站状态。全站唯一的写入口。
func SetPrimarySourceID(id string) error {
	id = strings.TrimSpace(id)
	if id == "" {
		return errors.New("主站 ID 不能为空")
	}
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	err := db.Mdb.Transaction(func(tx *gorm.DB) error {
		var rows int64
		if err := tx.Model(&model.PrimaryState{}).Count(&rows).Error; err != nil {
			return err
		}
		if rows == 0 {
			if err := tx.Create(&model.PrimaryState{Id: 1, PrimarySourceId: id, UpdatedAt: time.Now()}).Error; err != nil {
				return err
			}
		} else {
			if err := tx.Model(&model.PrimaryState{}).Where("id = ?", 1).
				Updates(map[string]any{"primary_source_id": id, "updated_at": time.Now()}).Error; err != nil {
				return err
			}
		}
		return moveCollectSourceToFirstTx(tx, id)
	})
	if err == nil {
		// 同步刷新采集站内存目录，保证 sort 顺序与主站状态一致；刷新失败时仍清理主站缓存避免脏读。
		if refreshErr := RefreshCollectSourceCatalog(); refreshErr != nil {
			InvalidatePrimarySourceCache()
		}
	}
	return err
}

// moveCollectSourceToFirstTx 在事务内将新主站排序移至首位 (sort=0)，其余站点保持原有相对顺序依次顺延。
func moveCollectSourceToFirstTx(tx *gorm.DB, primaryID string) error {
	primaryID = strings.TrimSpace(primaryID)
	if primaryID == "" {
		return nil
	}
	var list []model.FilmSource
	if err := tx.Order("sort ASC, created_at ASC, id ASC").Find(&list).Error; err != nil {
		return err
	}
	if len(list) == 0 {
		return nil
	}
	targetIdx := -1
	for i := range list {
		if list[i].Id == primaryID {
			targetIdx = i
			break
		}
	}
	if targetIdx == -1 {
		return nil
	}

	reordered := make([]*model.FilmSource, 0, len(list))
	reordered = append(reordered, &list[targetIdx])
	for i := range list {
		if i != targetIdx {
			reordered = append(reordered, &list[i])
		}
	}

	for newSort, s := range reordered {
		if s.Sort != newSort {
			if err := tx.Model(&model.FilmSource{}).Where("id = ?", s.Id).Update("sort", newSort).Error; err != nil {
				return err
			}
		}
	}
	return nil
}

// ClearPrimarySourceState 置空主站状态（D6 最后一个启用站的例外路径）。
func ClearPrimarySourceState() error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	err := db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.PrimaryState{}).Error
	if err == nil {
		InvalidatePrimarySourceCache()
	}
	return err
}

// FindOtherEnabledCollectSourcesExcluding 返回除 excludeIDs 外排序靠前且未在删除中的启用站列表。
func FindOtherEnabledCollectSourcesExcluding(excludeIDs map[string]struct{}) []*model.FilmSource {
	if db.Mdb == nil {
		return nil
	}
	var list []model.FilmSource
	if err := db.Mdb.Where("state = ?", true).Order("sort ASC, created_at ASC, id ASC").Find(&list).Error; err != nil {
		return nil
	}
	result := make([]*model.FilmSource, 0, len(list))
	for i := range list {
		id := strings.TrimSpace(list[i].Id)
		if id == "" || isSourceDeleting(id) {
			continue
		}
		if _, ok := excludeIDs[id]; ok {
			continue
		}
		result = append(result, &list[i])
	}
	return result
}

// ReconcilePrimaryAfterSourceReplace 备份导入整体替换站点后调用：
// 存储的主站仍在替换列表中则保留，否则清空，由懒初始化按新列表重选。
// 状态表未建（如测试库）时跳过。
func ReconcilePrimaryAfterSourceReplace(list []model.FilmSource) {
	if db.Mdb == nil {
		return
	}
	id := storedPrimarySourceID()
	if id == "" {
		InvalidatePrimarySourceCache()
		return
	}
	for i := range list {
		if strings.TrimSpace(list[i].Id) == id && list[i].State {
			// 主站 ID 未变但站点名称、启用状态或域名规则可能已替换，必须失效缓存强制重载
			InvalidatePrimarySourceCache()
			return
		}
	}
	if err := db.Mdb.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.PrimaryState{}).Error; err != nil {
		syslog.Errorf("[Primary] 备份导入后清理失效主站状态失败: %v", err)
	} else {
		InvalidatePrimarySourceCache()
	}
}

// storedPrimarySourceID 读取持久化的主站 ID。未设置（或表未建，如测试库）返回空。
func storedPrimarySourceID() string {
	if db.Mdb == nil {
		return ""
	}
	var rec model.PrimaryState
	if err := db.Mdb.Where("id = ?", 1).First(&rec).Error; err != nil {
		return ""
	}
	return strings.TrimSpace(rec.PrimarySourceId)
}

// primarySourceByID 校验并返回存储的主站。失效属异常状态，返回 nil，不自动换站。
func primarySourceByID(id string) *model.FilmSource {
	var fs model.FilmSource
	if err := db.Mdb.Where("id = ?", id).First(&fs).Error; err != nil {
		syslog.Warnf("[Primary] 存储的主站 %s 已不存在，等待人工切换", id)
		return nil
	}
	if !fs.State {
		syslog.Warnf("[Primary] 存储的主站 %s(%s) 已停用，不自动换站", fs.Name, fs.Id)
		return nil
	}
	if isSourceDeleting(fs.Id) {
		syslog.Warnf("[Primary] 存储的主站 %s(%s) 正在删除，不自动换站", fs.Name, fs.Id)
		return nil
	}
	return &fs
}
