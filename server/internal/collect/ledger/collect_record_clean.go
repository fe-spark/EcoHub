package ledger

import (
	"errors"
	"fmt"
	"time"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

// 清理与容量治理参数（包级变量，便于测试覆盖多轮与上限淘汰）。
var (
	collectCleanRecordChunk      = 200
	collectCleanFilmChunk        = 5000
	collectLedgerRetentionDays   = 7
	collectLedgerMaxRows         int64 = 10_000_000
	collectCleanBeforeDeleteHook func(ids []uint)
)

// CleanOldCollectRecords 清理过期采集记录，并按独立留存周期与总行数上限滚动淘汰影片账本。
// 主记录保留 days 天；影片账本保留 collectLedgerRetentionDays 天且总行数不超过 collectLedgerMaxRows，
// 超限时从最旧的已结束批次回收账本行并将主记录 ledger 置为 false（主记录统计数保留）。进行中批次一律不动。
func CleanOldCollectRecords(days int) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
	if days <= 0 {
		days = 30
	}
	now := time.Now()
	recordThreshold := now.AddDate(0, 0, -days)
	session := db.Mdb.Session(&gorm.Session{SkipDefaultTransaction: true})
	recordChunk := collectCleanRecordChunk
	if recordChunk <= 0 {
		recordChunk = 200
	}

	var cursor uint
	for {
		var ids []uint
		if err := session.Model(&model.CollectRecord{}).
			Where("started_at < ? AND status != ? AND id > ?", recordThreshold, model.CollectRecordStatusRunning, cursor).
			Order("id ASC").
			Limit(recordChunk).
			Pluck("id", &ids).Error; err != nil {
			return fmt.Errorf("查询过期采集记录失败: %w", err)
		}
		if len(ids) == 0 {
			break
		}
		cursor = ids[len(ids)-1]
		if collectCleanBeforeDeleteHook != nil {
			collectCleanBeforeDeleteHook(ids)
		}
		if err := session.Where("id IN ? AND status != ?", ids, model.CollectRecordStatusRunning).
			Delete(&model.CollectRecord{}).Error; err != nil {
			return fmt.Errorf("删除过期采集记录失败: %w", err)
		}
		gone, err := recordIDsWithoutRecord(session, ids)
		if err != nil {
			return err
		}
		if err := deleteCollectFilmsByRecordIDs(session, gone); err != nil {
			return err
		}
		if len(ids) < recordChunk {
			break
		}
	}

	ledgerDays := collectLedgerRetentionDays
	if ledgerDays > 0 && ledgerDays < days {
		if err := pruneExpiredCollectLedgers(session, now.AddDate(0, 0, -ledgerDays), recordChunk); err != nil {
			return err
		}
	}
	if collectLedgerMaxRows > 0 {
		if err := pruneExcessCollectLedgers(session, collectLedgerMaxRows, recordChunk); err != nil {
			return err
		}
	}
	return sweepOrphanCollectFilms(session, recordChunk)
}

func pruneExpiredCollectLedgers(session *gorm.DB, threshold time.Time, recordChunk int) error {
	var cursor uint
	for {
		var ids []uint
		if err := session.Model(&model.CollectRecord{}).
			Where("started_at < ? AND ledger = ? AND status != ? AND id > ?", threshold, true, model.CollectRecordStatusRunning, cursor).
			Order("id ASC").
			Limit(recordChunk).
			Pluck("id", &ids).Error; err != nil {
			return fmt.Errorf("查询过期账本批次失败: %w", err)
		}
		if len(ids) == 0 {
			return nil
		}
		cursor = ids[len(ids)-1]
		if err := retireLedgerRecords(session, ids); err != nil {
			return err
		}
		if len(ids) < recordChunk {
			return nil
		}
	}
}

func pruneExcessCollectLedgers(session *gorm.DB, maxRows int64, recordChunk int) error {
	var total int64
	if err := session.Model(&model.CollectRecord{}).
		Select("COALESCE(SUM(film_count), 0)").
		Where("ledger = ? AND status != ?", true, model.CollectRecordStatusRunning).
		Scan(&total).Error; err != nil {
		return fmt.Errorf("统计账本总行数失败: %w", err)
	}
	if total <= maxRows {
		return nil
	}
	var cursor uint
	for total > maxRows {
		var rows []struct {
			ID        uint
			FilmCount int
		}
		if err := session.Model(&model.CollectRecord{}).
			Select("id", "film_count").
			Where("ledger = ? AND status != ? AND id > ?", true, model.CollectRecordStatusRunning, cursor).
			Order("id ASC").
			Limit(recordChunk).
			Scan(&rows).Error; err != nil {
			return fmt.Errorf("查询超限账本批次失败: %w", err)
		}
		if len(rows) == 0 {
			return nil
		}
		cursor = rows[len(rows)-1].ID
		evict := make([]uint, 0, len(rows))
		for _, r := range rows {
			if total <= maxRows {
				break
			}
			evict = append(evict, r.ID)
			total -= int64(r.FilmCount)
		}
		if err := retireLedgerRecords(session, evict); err != nil {
			return err
		}
		if len(rows) < recordChunk {
			return nil
		}
	}
	return nil
}

// retireLedgerRecords 回收已结束批次的影片账本并将主记录 ledger 置为 false（主记录统计保留）。
func retireLedgerRecords(session *gorm.DB, ids []uint) error {
	if len(ids) == 0 {
		return nil
	}
	if err := session.Model(&model.CollectRecord{}).
		Where("id IN ? AND status != ?", ids, model.CollectRecordStatusRunning).
		Update("ledger", false).Error; err != nil {
		return fmt.Errorf("关闭过期账本标记失败: %w", err)
	}
	var retired []uint
	if err := session.Model(&model.CollectRecord{}).
		Where("id IN ? AND ledger = ? AND status != ?", ids, false, model.CollectRecordStatusRunning).
		Pluck("id", &retired).Error; err != nil {
		return fmt.Errorf("复核回收账本批次失败: %w", err)
	}
	return deleteCollectFilmsByRecordIDs(session, retired)
}

func recordIDsWithoutRecord(session *gorm.DB, ids []uint) ([]uint, error) {
	if len(ids) == 0 {
		return nil, nil
	}
	var alive []uint
	if err := session.Model(&model.CollectRecord{}).
		Where("id IN ?", ids).
		Pluck("id", &alive).Error; err != nil {
		return nil, fmt.Errorf("查询采集记录失败: %w", err)
	}
	aliveSet := make(map[uint]struct{}, len(alive))
	for _, id := range alive {
		aliveSet[id] = struct{}{}
	}
	gone := make([]uint, 0, len(ids))
	for _, id := range ids {
		if _, ok := aliveSet[id]; !ok {
			gone = append(gone, id)
		}
	}
	return gone, nil
}

// deleteCollectFilmsByRecordIDs 按聚簇主键分批删除指定记录的账本行，兼容 MySQL 与 SQLite。
func deleteCollectFilmsByRecordIDs(session *gorm.DB, recordIDs []uint) error {
	if session == nil || len(recordIDs) == 0 {
		return nil
	}
	chunk := collectCleanFilmChunk
	if chunk <= 0 {
		chunk = 5000
	}
	isMySQL := session.Dialector != nil && session.Dialector.Name() == "mysql"
	for {
		var res *gorm.DB
		if isMySQL {
			res = session.Exec("DELETE FROM "+model.TableCollectRecordFilm+" WHERE record_id IN ? LIMIT ?", recordIDs, chunk)
		} else {
			res = session.Exec("DELETE FROM "+model.TableCollectRecordFilm+" WHERE rowid IN (SELECT rowid FROM "+model.TableCollectRecordFilm+" WHERE record_id IN ? LIMIT ?)", recordIDs, chunk)
		}
		if res.Error != nil {
			return fmt.Errorf("删除影片账本失败: %w", res.Error)
		}
		if res.RowsAffected < int64(chunk) {
			return nil
		}
	}
}

// sweepOrphanCollectFilms 按 record_id 游标清理主记录已不存在的孤儿账本行。
func sweepOrphanCollectFilms(session *gorm.DB, recordChunk int) error {
	var cursor uint
	for {
		var recordIDs []uint
		if err := session.Model(&model.CollectRecordFilmItem{}).
			Distinct("record_id").
			Where("record_id > ?", cursor).
			Order("record_id ASC").
			Limit(recordChunk).
			Pluck("record_id", &recordIDs).Error; err != nil {
			return fmt.Errorf("查询孤儿账本记录号失败: %w", err)
		}
		if len(recordIDs) == 0 {
			return nil
		}
		cursor = recordIDs[len(recordIDs)-1]
		gone, err := recordIDsWithoutRecord(session, recordIDs)
		if err != nil {
			return err
		}
		if err := deleteCollectFilmsByRecordIDs(session, gone); err != nil {
			return err
		}
		if len(recordIDs) < recordChunk {
			return nil
		}
	}
}
