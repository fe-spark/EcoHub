package migration

import (
	"encoding/json"
	"errors"
	"log"
	"slices"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"

	mysqldriver "github.com/go-sql-driver/mysql"
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
		if err := dropLegacyCollectRecordFilmsTable(gdb, legacyFilmTable); err != nil {
			return err
		}
	}

	if gdb.Migrator().HasTable(&model.CollectRecord{}) && gdb.Migrator().HasColumn(&model.CollectRecord{}, "ledger") {
		log.Printf("[Migration] 正在清理 %s.ledger 废弃列...", model.TableCollectRecord)
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
	started := time.Now()
	if err := rebuildFilmIndex(gdb, isSQLite, notifyFilmIndexSpec, exists); err != nil {
		return err
	}
	log.Printf("[Migration] %s.%s 联合索引构建完成 (耗时 %s)", model.TableFilmIndex, notifyFilmIndexSpec.name, time.Since(started).Round(time.Millisecond))
	return nil
}

func addLastRecordIDColumn(gdb *gorm.DB, isSQLite bool) error {
	if isSQLite {
		return gdb.Exec("ALTER TABLE " + model.TableFilmIndex + " ADD COLUMN last_record_id integer NOT NULL DEFAULT 0").Error
	}
	started := time.Now()
	// MySQL 8.0+ 表尾加列优先使用 ALGORITHM=INSTANT（毫秒级完成），避免强行 INPLACE 触发百万行整表复制重建。
	instantSQL := "ALTER TABLE " + model.TableFilmIndex + " ADD COLUMN last_record_id bigint unsigned NOT NULL DEFAULT 0, ALGORITHM=INSTANT"
	log.Printf("[Migration] 正在为 %s 新增 last_record_id 列（优先 ALGORITHM=INSTANT）...", model.TableFilmIndex)
	err := execMigrationDDL(gdb, instantSQL)
	if err == nil {
		log.Printf("[Migration] %s 新增 last_record_id 完成 (ALGORITHM=INSTANT, 耗时 %s)", model.TableFilmIndex, time.Since(started).Round(time.Millisecond))
		return nil
	}
	if !isMySQLInstantUnsupported(err) {
		return err
	}
	log.Printf("[Migration] 当前 MySQL 不支持 ALGORITHM=INSTANT (%v)，回退在线 INPLACE 加列...", err)
	inplaceSQL := "ALTER TABLE " + model.TableFilmIndex + " ADD COLUMN last_record_id bigint unsigned NOT NULL DEFAULT 0, ALGORITHM=INPLACE, LOCK=NONE"
	if err := execMigrationDDL(gdb, inplaceSQL); err != nil {
		return err
	}
	log.Printf("[Migration] %s 新增 last_record_id 完成 (ALGORITHM=INPLACE, 耗时 %s)", model.TableFilmIndex, time.Since(started).Round(time.Millisecond))
	return nil
}

func dropLegacyCollectRecordFilmsTable(gdb *gorm.DB, tableName string) error {
	isSQLite := gdb.Dialector != nil && gdb.Dialector.Name() == "sqlite"
	if isSQLite {
		return gdb.Migrator().DropTable(tableName)
	}
	started := time.Now()
	log.Printf("[Migration] 正在删除旧账本表 %s...", tableName)
	if err := execMigrationDDL(gdb, "DROP TABLE IF EXISTS "+tableName); err != nil {
		return err
	}
	log.Printf("[Migration] 已删除旧账本表 %s (耗时 %s)", tableName, time.Since(started).Round(time.Millisecond))
	return nil
}

// execMigrationDDL 在线执行大表 DDL；连真实主库时走长读超时直连，避免默认 30s readTimeout 中断。
func execMigrationDDL(gdb *gorm.DB, ddlSQL string) error {
	if db.Mdb != nil && gdb == db.Mdb && strings.TrimSpace(config.MysqlDsn) != "" {
		conn, err := db.OpenLongReadConn(config.MysqlDsn)
		if err != nil {
			return err
		}
		defer conn.Close()
		if _, err := conn.Exec("SET SESSION net_read_timeout = 1800, net_write_timeout = 1800"); err != nil {
			return err
		}
		_, err = conn.Exec(ddlSQL)
		return err
	}
	return gdb.Exec(ddlSQL).Error
}

func isMySQLInstantUnsupported(err error) bool {
	if err == nil {
		return false
	}
	var mysqlErr *mysqldriver.MySQLError
	if errors.As(err, &mysqlErr) {
		switch mysqlErr.Number {
		case 1845, 1846, 1064:
			return true
		}
	}
	msg := strings.ToUpper(err.Error())
	return strings.Contains(msg, "INSTANT") && (strings.Contains(msg, "NOT SUPPORTED") || strings.Contains(msg, "SYNTAX"))
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
