package repository

import (
	"crypto/sha1"
	"encoding/hex"
	"errors"
	"fmt"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// ErrBannerRevisionConflict 提交时带的版本号和该站当前轮播不一致。
var ErrBannerRevisionConflict = errors.New("轮播已被修改，请刷新后再试")

// ErrBannerSlotNotFound 轮播行不存在。
var ErrBannerSlotNotFound = errors.New("轮播不存在")

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
	gdb, err := bannerDB()
	if err != nil {
		return nil, err
	}
	return listBannerSlotsTx(gdb, sourceID)
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
	var slot model.BannerSlot
	if err := gdb.Where("id = ?", strings.TrimSpace(id)).First(&slot).Error; err != nil {
		if errors.Is(err, gorm.ErrRecordNotFound) {
			return model.BannerSlot{}, ErrBannerSlotNotFound
		}
		return model.BannerSlot{}, err
	}
	return slot, nil
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

// CreateBannerSlot 新增一行，排在该站末尾。
func CreateBannerSlot(slot *model.BannerSlot) error {
	gdb, err := bannerDB()
	if err != nil {
		return err
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
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
		return tx.Create(slot).Error
	})
	if err == nil {
		ClearIndexPageCache()
	}
	return err
}

// UpdateBannerSlot 保存一行的全部字段（调用方已合并好）。
func UpdateBannerSlot(slot *model.BannerSlot) error {
	gdb, err := bannerDB()
	if err != nil {
		return err
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
		taken, err := bannerFilmTaken(tx, slot.SourceId, slot.Mid, slot.Id)
		if err != nil {
			return err
		}
		if taken {
			return ErrBannerDuplicateFilm
		}
		res := tx.Model(&model.BannerSlot{}).Where("id = ?", slot.Id).
			Select("mid", "image_mode", "poster_override", "backdrop_override", "blurb_override", "score_override", "year_override", "tmdb_id", "updated_at").
			Updates(slot)
		if res.Error != nil {
			return res.Error
		}
		if res.RowsAffected == 0 {
			return ErrBannerSlotNotFound
		}
		return nil
	})
	if err == nil {
		ClearIndexPageCache()
	}
	return err
}

// DeleteBannerSlot 删除一行。
func DeleteBannerSlot(id string) error {
	gdb, err := bannerDB()
	if err != nil {
		return err
	}
	res := gdb.Where("id = ?", strings.TrimSpace(id)).Delete(&model.BannerSlot{})
	if res.Error != nil {
		return res.Error
	}
	if res.RowsAffected == 0 {
		return ErrBannerSlotNotFound
	}
	ClearIndexPageCache()
	return nil
}

// ReorderBannerSlots 按 ids 的顺序重排该站轮播。ids 必须正好是该站现有的全部行。
func ReorderBannerSlots(sourceID, revision string, ids []string) error {
	gdb, err := bannerDB()
	if err != nil {
		return err
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
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
		have := make(map[string]bool, len(current))
		for _, s := range current {
			have[s.Id] = true
		}
		for i, id := range ids {
			if !have[id] {
				return ErrBannerRevisionConflict
			}
			delete(have, id)
			if err := tx.Model(&model.BannerSlot{}).Where("id = ?", id).Update("sort", int64(i+1)).Error; err != nil {
				return err
			}
		}
		return nil
	})
	if err == nil {
		ClearIndexPageCache()
	}
	return err
}

// ReplaceBannerSlots 整组替换该站轮播（一键排片发布）。
func ReplaceBannerSlots(sourceID, revision string, slots []model.BannerSlot) error {
	gdb, err := bannerDB()
	if err != nil {
		return err
	}
	err = gdb.Transaction(func(tx *gorm.DB) error {
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
		return tx.Create(&slots).Error
	})
	if err == nil {
		ClearIndexPageCache()
	}
	return err
}

// ReplaceAllBannerSlots 用备份整体替换全部站点的轮播。
func ReplaceAllBannerSlots(slots []model.BannerSlot) error {
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
		return tx.Create(&slots).Error
	})
	if err == nil {
		ClearIndexPageCache()
	}
	return err
}

// DeleteBannerSlotsByMids 影片档案删除时在同一事务里删掉它们的轮播。
func DeleteBannerSlotsByMids(tx *gorm.DB, mids []int64) error {
	if tx == nil || len(mids) == 0 {
		return nil
	}
	return tx.Where("mid IN ?", mids).Delete(&model.BannerSlot{}).Error
}

// DeleteBannerSlotsBySource 删除一个站点的全部轮播。
func DeleteBannerSlotsBySource(tx *gorm.DB, sourceID string) error {
	if tx == nil || strings.TrimSpace(sourceID) == "" {
		return nil
	}
	return tx.Where("source_id = ?", strings.TrimSpace(sourceID)).Delete(&model.BannerSlot{}).Error
}

// FilmOnSource 影片档案存在，且在该站有播放线路。
func FilmOnSource(mid int64, sourceID string) (bool, error) {
	gdb, err := bannerDB()
	if err != nil {
		return false, err
	}
	var count int64
	err = gdb.Model(&model.FilmIndex{}).
		Where("mid = ?", mid).
		Where(model.FilmHasPlaySourceSQL(), strings.TrimSpace(sourceID), model.LineKindPlay).
		Count(&count).Error
	return count > 0, err
}
