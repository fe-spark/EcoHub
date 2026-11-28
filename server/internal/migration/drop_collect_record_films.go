package migration

import (
	"encoding/json"
	"slices"
	"strings"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

var (
	notifyFilmIndexSpec = filmIndexSpec{
		name: "idx_film_index_source_record_mid",
		cols: []string{"first_source_id", "last_record_id", "deleted_at", "mid"},
	}
	backfillTargetMidsChunkSize = 500
)

// migrateDropCollectRecordFilms 执行采集记录瘦身与通知单表索引迁移：
// 1. 为 film_index 在线补齐 last_record_id 列与联合索引 idx_film_index_source_record_mid；
// 2. 为 collect_records 增加 target_mids 列，并分批将存量单片更新记录回填至 target_mids；
// 3. 删除 collect_records.ledger 列与 collect_record_films 账本表。
func migrateDropCollectRecordFilms(gdb *gorm.DB) error {
	if gdb == nil {
		return nil
	}
	if err := ensureFilmIndexLastRecordSchema(gdb); err != nil {
		return err
	}
	if err := gdb.AutoMigrate(&model.CollectRecord{}); err != nil {
		return err
	}

	const legacyFilmTable = "collect_record_films"
	if gdb.Migrator().HasTable(legacyFilmTable) {
		if err := backfillSingleUpdateTargetMids(gdb, legacyFilmTable); err != nil {
			return err
		}
		if err := gdb.Migrator().DropTable(legacyFilmTable); err != nil {
			return err
		}
	}

	if gdb.Migrator().HasTable(&model.CollectRecord{}) && gdb.Migrator().HasColumn(&model.CollectRecord{}, "ledger") {
		if err := gdb.Exec("ALTER TABLE " + model.TableCollectRecord + " DROP COLUMN ledger").Error; err != nil {
			return err
		}
	}
	return nil
}

func ensureFilmIndexLastRecordSchema(gdb *gorm.DB) error {
	if !gdb.Migrator().HasTable(&model.FilmIndex{}) {
		return gdb.AutoMigrate(&model.FilmIndex{})
	}
	isSQLite := gdb.Dialector != nil && gdb.Dialector.Name() == "sqlite"
	if !gdb.Migrator().HasColumn(&model.FilmIndex{}, "last_record_id") {
		if err := addLastRecordIDColumn(gdb, isSQLite); err != nil {
			return err
		}
	}
	curCols, exists, err := currentIndexColumns(gdb, isSQLite, model.TableFilmIndex, notifyFilmIndexSpec.name)
	if err != nil {
		return err
	}
	if exists && slices.Equal(curCols, notifyFilmIndexSpec.cols) {
		return nil
	}
	return rebuildFilmIndex(gdb, isSQLite, notifyFilmIndexSpec, exists)
}

func addLastRecordIDColumn(gdb *gorm.DB, isSQLite bool) error {
	if isSQLite {
		return gdb.Exec("ALTER TABLE " + model.TableFilmIndex + " ADD COLUMN last_record_id integer NOT NULL DEFAULT 0").Error
	}
	alterSQL := "ALTER TABLE " + model.TableFilmIndex + " ADD COLUMN last_record_id bigint unsigned NOT NULL DEFAULT 0, ALGORITHM=INPLACE, LOCK=NONE"
	// 线上百万级 film_index 走长读超时连接执行在线 DDL，避免默认 30s readTimeout 中断。
	if db.Mdb != nil && gdb == db.Mdb && strings.TrimSpace(config.MysqlDsn) != "" {
		conn, err := db.OpenLongReadConn(config.MysqlDsn)
		if err != nil {
			return err
		}
		defer conn.Close()
		if _, err := conn.Exec("SET SESSION net_read_timeout = 1800, net_write_timeout = 1800"); err != nil {
			return err
		}
		_, err = conn.Exec(alterSQL)
		return err
	}
	return gdb.Exec(alterSQL).Error
}

func backfillSingleUpdateTargetMids(gdb *gorm.DB, legacyFilmTable string) error {
	if !gdb.Migrator().HasTable(&model.CollectRecord{}) || !gdb.Migrator().HasColumn(&model.CollectRecord{}, "target_mids") {
		return nil
	}
	var recIDs []uint
	if err := gdb.Table(model.TableCollectRecord).
		Where("(`trigger` = ? OR tag = ?) AND (target_mids IS NULL OR target_mids = '')",
			model.CollectTriggerSingleUpdate, model.CollectTagSingleUpdate).
		Order("id ASC").
		Pluck("id", &recIDs).Error; err != nil {
		return err
	}
	if len(recIDs) == 0 {
		return nil
	}
	chunkSize := backfillTargetMidsChunkSize
	if chunkSize <= 0 {
		chunkSize = 500
	}
	for start := 0; start < len(recIDs); start += chunkSize {
		end := start + chunkSize
		if end > len(recIDs) {
			end = len(recIDs)
		}
		if err := backfillTargetMidsChunk(gdb, legacyFilmTable, recIDs[start:end]); err != nil {
			return err
		}
	}
	return nil
}

func backfillTargetMidsChunk(gdb *gorm.DB, legacyFilmTable string, chunk []uint) error {
	var rows []struct {
		RecordID uint  `gorm:"column:record_id"`
		Mid      int64 `gorm:"column:mid"`
	}
	if err := gdb.Table(legacyFilmTable).
		Select("record_id, mid").
		Where("record_id IN ?", chunk).
		Order("record_id ASC, mid ASC").
		Scan(&rows).Error; err != nil {
		return err
	}
	byRec := make(map[uint][]int64, len(chunk))
	seen := make(map[uint]map[int64]struct{}, len(chunk))
	for _, r := range rows {
		if r.Mid <= 0 {
			continue
		}
		if seen[r.RecordID] == nil {
			seen[r.RecordID] = map[int64]struct{}{}
		}
		if _, ok := seen[r.RecordID][r.Mid]; ok {
			continue
		}
		seen[r.RecordID][r.Mid] = struct{}{}
		byRec[r.RecordID] = append(byRec[r.RecordID], r.Mid)
	}
	for id, mids := range byRec {
		raw, err := json.Marshal(mids)
		if err != nil {
			return err
		}
		if err := gdb.Table(model.TableCollectRecord).
			Where("id = ?", id).
			Update("target_mids", string(raw)).Error; err != nil {
			return err
		}
	}
	return nil
}
