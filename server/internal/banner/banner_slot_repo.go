package banner

import (
	"context"
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"

	"gorm.io/gorm"
	"gorm.io/gorm/clause"
)

var bannerWriteMu sync.Mutex

// ErrBannerRevisionConflict 提交时带的版本号和该站当前轮播不一致。
var ErrBannerRevisionConflict = errors.New("轮播已被修改，请刷新后再试")

// ErrBannerSlotNotFound 轮播行不存在。
var ErrBannerSlotNotFound = errors.New("轮播不存在")

// ErrBannerSourceNotFound 轮播所属的采集站不存在。
var ErrBannerSourceNotFound = errors.New("采集站不存在")

// ErrBannerDuplicateFilm 同一站点重复添加同一部影片。
var ErrBannerDuplicateFilm = errors.New("该影片已在本站轮播中")

func bannerDB() (*gorm.DB, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	return db.Mdb, nil
}

func listBannerSlotsTx(tx *gorm.DB, sourceID string) ([]model.BannerSlot, error) {
	list := make([]model.BannerSlot, 0)
	err := tx.Where("source_id = ?", strings.TrimSpace(sourceID)).Order("sort ASC, id ASC").Find(&list).Error
	return list, err
}

// ListBannerSlots 读取一个站点的轮播，按排序返回。
func ListBannerSlots(sourceID string) ([]model.BannerSlot, error) {
	return ListBannerSlotsCtx(context.Background(), sourceID)
}

// ListBannerSlotsCtx 同 ListBannerSlots，查询受 ctx 的超时和取消控制。
func ListBannerSlotsCtx(ctx context.Context, sourceID string) ([]model.BannerSlot, error) {
	gdb, err := bannerDB()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return listBannerSlotsTx(gdb.WithContext(ctx), sourceID)
}

// ListAllBannerSlots 读取全部站点的轮播，用于备份。
func ListAllBannerSlots() ([]model.BannerSlot, error) {
	gdb, err := bannerDB()
	if err != nil {
		return nil, err
	}
	list := make([]model.BannerSlot, 0)
	err = gdb.Order("source_id ASC, sort ASC, id ASC").Find(&list).Error
	return list, err
}

// BannerRevision 按排好序的轮播行计算版本号。任何一行增删、改动或换序都会改变它。
func BannerRevision(slots []model.BannerSlot) string {
	h := sha1.New()
	for _, s := range slots {
		fmt.Fprintf(h, "%s|%d|%d|%s|%s|%s|%s|%g|%d|%d\n",
			s.Id, s.Mid, s.Sort, s.ImageMode, s.PosterOverride, s.BackdropOverride,
			s.BlurbOverride, s.ScoreOverride, s.YearOverride, s.TmdbID)
	}
	return hex.EncodeToString(h.Sum(nil))[:16]
}

// GetBannerSlot 按 ID 读一行。
func GetBannerSlot(id string) (model.BannerSlot, error) {
	gdb, err := bannerDB()
	if err != nil {
		return model.BannerSlot{}, err
	}
	return getBannerSlotTx(gdb, id)
}

func bannerFilmTaken(tx *gorm.DB, sourceID string, mid int64, exceptID string) (bool, error) {
	var count int64
	q := tx.Model(&model.BannerSlot{}).Where("source_id = ? AND mid = ?", sourceID, mid)
	if exceptID != "" {
		q = q.Where("id <> ?", exceptID)
	}
	err := q.Count(&count).Error
	return count > 0, err
}

// lockBannerSource 在事务里锁住采集站这一行，同一站点的轮播写入因此串行执行，
// 版本号校验和随后的写入之间不会被别的请求插进来。sqlite 不支持行锁，驱动会忽略 FOR UPDATE。
func lockBannerSource(tx *gorm.DB, sourceID string) error {
	var src model.FilmSource
	err := tx.Clauses(clause.Locking{Strength: "UPDATE"}).
		Select("id").Where("id = ?", strings.TrimSpace(sourceID)).First(&src).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return ErrBannerSourceNotFound
	}
	return err
}

// isDuplicateKeyErr 是否撞了唯一索引（MySQL 1062 / sqlite UNIQUE）。
func isDuplicateKeyErr(err error) bool {
	return db.IsDuplicateKeyError(err)
}

// getBannerSlotTx 在事务里按 ID 读一行。
func getBannerSlotTx(tx *gorm.DB, id string) (model.BannerSlot, error) {
	var slot model.BannerSlot
	if err := tx.Where("id = ?", strings.TrimSpace(id)).First(&slot).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.BannerSlot{}, ErrBannerSlotNotFound
		}
		return model.BannerSlot{}, err
	}
	return slot, nil
}

// lockBannerSlot 读出一行并锁住它所在站点，返回锁定后重新读到的行。
func lockBannerSlot(tx *gorm.DB, id string) (model.BannerSlot, error) {
	slot, err := getBannerSlotTx(tx, id)
	if err != nil {
		return model.BannerSlot{}, err
	}
	if err := lockBannerSource(tx, slot.SourceId); err != nil {
		return model.BannerSlot{}, err
	}
	// 拿到站点锁之前这一行可能已被删除或修改，锁定后重读一次。
	return getBannerSlotTx(tx, id)
}

// CreateBannerSlot 新增一行，排在该站末尾。
func CreateBannerSlot(slot *model.BannerSlot) error {
	bannerWriteMu.Lock()
	defer bannerWriteMu.Unlock()
	gdb, err := bannerDB()
	if err != nil {
		return err
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
		if err := lockBannerSource(tx, slot.SourceId); err != nil {
			return err
		}
		taken, err := bannerFilmTaken(tx, slot.SourceId, slot.Mid, "")
		if err != nil {
			return err
		}
		if taken {
			return ErrBannerDuplicateFilm
		}
		var maxSort int64
		if err := tx.Model(&model.BannerSlot{}).Where("source_id = ?", slot.SourceId).
			Select("COALESCE(MAX(sort), 0)").Scan(&maxSort).Error; err != nil {
			return err
		}
		slot.Sort = maxSort + 1
		if err := tx.Create(slot).Error; err != nil {
			if isDuplicateKeyErr(err) {
				return ErrBannerDuplicateFilm
			}
			return err
		}
		return nil
	})
	if err == nil {
		repobase.ClearIndexPageCache()
	}
	return err
}

// UpdateBannerSlot 在事务里读出一行、交给 apply 合并字段，再整行保存。读-改-写在同一把站点锁里完成。
func UpdateBannerSlot(id string, apply func(slot *model.BannerSlot) error) (model.BannerSlot, error) {
	return UpdateBannerSlotWithTx(id, func(_ *gorm.DB, slot *model.BannerSlot) error {
		return apply(slot)
	})
}

// UpdateBannerSlotWithTx 同 UpdateBannerSlot，同时向回调提供当前事务句柄以便复用事务连接做校验。
func UpdateBannerSlotWithTx(id string, apply func(tx *gorm.DB, slot *model.BannerSlot) error) (model.BannerSlot, error) {
	bannerWriteMu.Lock()
	defer bannerWriteMu.Unlock()
	gdb, err := bannerDB()
	if err != nil {
		return model.BannerSlot{}, err
	}
	var saved model.BannerSlot
	err = gdb.Transaction(func(tx *gorm.DB) error {
		slot, err := lockBannerSlot(tx, id)
		if err != nil {
			return err
		}
		if err := apply(tx, &slot); err != nil {
			return err
		}
		taken, err := bannerFilmTaken(tx, slot.SourceId, slot.Mid, slot.Id)
		if err != nil {
			return err
		}
		if taken {
			return ErrBannerDuplicateFilm
		}
		res := tx.Model(&model.BannerSlot{}).Where("id = ?", slot.Id).
			Select("mid", "image_mode", "poster_override", "backdrop_override", "blurb_override", "score_override", "year_override", "tmdb_id", "updated_at").
			Updates(&slot)
		if res.Error != nil {
			if isDuplicateKeyErr(res.Error) {
				return ErrBannerDuplicateFilm
			}
			return res.Error
		}
		saved = slot
		return nil
	})
	if err != nil {
		return model.BannerSlot{}, err
	}
	repobase.ClearIndexPageCache()
	return saved, nil
}

// DeleteBannerSlot 删除一行。
func DeleteBannerSlot(id string) error {
	bannerWriteMu.Lock()
	defer bannerWriteMu.Unlock()
	gdb, err := bannerDB()
	if err != nil {
		return err
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
		slot, err := lockBannerSlot(tx, id)
		if err != nil {
			return err
		}
		res := tx.Where("id = ?", slot.Id).Delete(&model.BannerSlot{})
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrBannerSlotNotFound
		}
		return nil
	})
	if err != nil {
		return err
	}
	repobase.ClearIndexPageCache()
	return nil
}

// ReorderBannerSlots 按 ids 的顺序重排该站轮播。ids 必须正好是该站现有的全部行（含后台标成不可用的行）。
func ReorderBannerSlots(sourceID, revision string, ids []string) error {
	bannerWriteMu.Lock()
	defer bannerWriteMu.Unlock()
	gdb, err := bannerDB()
	if err != nil {
		return err
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
		if err := lockBannerSource(tx, sourceID); err != nil {
			return err
		}
		current, err := listBannerSlotsTx(tx, sourceID)
		if err != nil {
			return err
		}
		if BannerRevision(current) != strings.TrimSpace(revision) {
			return ErrBannerRevisionConflict
		}
		if len(ids) != len(current) {
			return ErrBannerRevisionConflict
		}
		sortByID := make(map[string]int64, len(current))
		for _, s := range current {
			sortByID[s.Id] = s.Sort
		}
		for i, id := range ids {
			oldSort, ok := sortByID[id]
			if !ok {
				return ErrBannerRevisionConflict
			}
			delete(sortByID, id)
			next := int64(i + 1)
			if oldSort == next {
				continue
			}
			// 只更新排序真的变化的行：MySQL 对值没变的行 RowsAffected 为 0，不能拿来判断行是否还在。
			res := tx.Model(&model.BannerSlot{}).Where("id = ? AND source_id = ?", id, sourceID).Update("sort", next)
			if res.Error != nil {
				return res.Error
			}
			if res.RowsAffected != 1 {
				return ErrBannerRevisionConflict
			}
		}
		return nil
	})
	if err == nil {
		repobase.ClearIndexPageCache()
	}
	return err
}

// ReplaceBannerSlots 整组替换该站轮播（一键排片发布）。
func ReplaceBannerSlots(sourceID, revision string, slots []model.BannerSlot) error {
	bannerWriteMu.Lock()
	defer bannerWriteMu.Unlock()
	gdb, err := bannerDB()
	if err != nil {
		return err
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
		if err := lockBannerSource(tx, sourceID); err != nil {
			return err
		}
		current, err := listBannerSlotsTx(tx, sourceID)
		if err != nil {
			return err
		}
		if BannerRevision(current) != strings.TrimSpace(revision) {
			return ErrBannerRevisionConflict
		}
		if err := tx.Where("source_id = ?", sourceID).Delete(&model.BannerSlot{}).Error; err != nil {
			return err
		}
		if len(slots) == 0 {
			return nil
		}
		if err := tx.CreateInBatches(&slots, 200).Error; err != nil {
			if isDuplicateKeyErr(err) {
				return ErrBannerDuplicateFilm
			}
			return err
		}
		return nil
	})
	if err == nil {
		repobase.ClearIndexPageCache()
	}
	return err
}

// ReplaceAllBannerSlots 用备份整体替换全部站点的轮播。
func ReplaceAllBannerSlots(slots []model.BannerSlot) error {
	bannerWriteMu.Lock()
	defer bannerWriteMu.Unlock()
	gdb, err := bannerDB()
	if err != nil {
		return err
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
		if err := tx.Session(&gorm.Session{AllowGlobalUpdate: true}).Delete(&model.BannerSlot{}).Error; err != nil {
			return err
		}
		if len(slots) == 0 {
			return nil
		}
		return tx.CreateInBatches(&slots, 200).Error
	})
	if err == nil {
		repobase.ClearIndexPageCache()
	}
	return err
}

// FilmsOnSource 批量判断：影片档案存在，且在该站有播放线路。返回命中的 mid 集合。
func FilmsOnSource(ctx context.Context, sourceID string, mids []int64) (map[int64]bool, error) {
	gdb, err := bannerDB()
	if err != nil {
		return nil, err
	}
	if ctx == nil {
		ctx = context.Background()
	}
	return FilmsOnSourceTx(gdb.WithContext(ctx), sourceID, mids)
}

// FilmsOnSourceTx 在指定连接/事务上批量判断影片是否存在且在该站有播放线路，避免事务内二次占用连接池。
func FilmsOnSourceTx(tx *gorm.DB, sourceID string, mids []int64) (map[int64]bool, error) {
	out := make(map[int64]bool, len(mids))
	if len(mids) == 0 {
		return out, nil
	}
	if tx == nil {
		return nil, errors.New("数据库未连接")
	}
	var hit []int64
	err := tx.Model(&model.FilmIndex{}).
		Where("mid IN ?", mids).
		Where(model.FilmHasPlaySourceSQL(), strings.TrimSpace(sourceID), model.LineKindPlay).
		Pluck("mid", &hit).Error
	if err != nil {
		return nil, err
	}
	for _, mid := range hit {
		out[mid] = true
	}
	return out, nil
}

// ExistingFilmMids 片库里还存在的 mid。
func ExistingFilmMids(mids []int64) (map[int64]bool, error) {
	out := make(map[int64]bool, len(mids))
	if len(mids) == 0 {
		return out, nil
	}
	gdb, err := bannerDB()
	if err != nil {
		return nil, err
	}
	for start := 0; start < len(mids); start += 500 {
		end := start + 500
		if end > len(mids) {
			end = len(mids)
		}
		var hit []int64
		if err := gdb.Model(&model.FilmIndex{}).Where("mid IN ?", mids[start:end]).Pluck("mid", &hit).Error; err != nil {
			return nil, err
		}
		for _, mid := range hit {
			out[mid] = true
		}
	}
	return out, nil
}
