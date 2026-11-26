package migration

import (
	"database/sql"
	"fmt"
	"log"
	"slices"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"
	"gorm.io/gorm"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
)

type filmIndexSpec struct {
	name string
	cols []string
}

var cidCompositeSpecs = []filmIndexSpec{
	{name: "idx_cid_update", cols: []string{"cid", "update_stamp", "mid"}},
	{name: "idx_cid_hits", cols: []string{"cid", "hits", "mid"}},
}

func migrateFilmIndexCidComposite(gdb *gorm.DB) error {
	if gdb == nil || !gdb.Migrator().HasTable(&model.FilmIndex{}) {
		return nil
	}
	isSQLite := gdb.Dialector != nil && gdb.Dialector.Name() == "sqlite"
	for _, spec := range cidCompositeSpecs {
		curCols, exists, err := currentIndexColumns(gdb, isSQLite, model.TableFilmIndex, spec.name)
		if err != nil {
			return err
		}
		if exists && slices.Equal(curCols, spec.cols) {
			continue
		}
		if err := rebuildFilmIndex(gdb, isSQLite, spec, exists); err != nil {
			return err
		}
	}
	return nil
}

func currentIndexColumns(gdb *gorm.DB, isSQLite bool, tableName, indexName string) ([]string, bool, error) {
	if isSQLite {
		type sqliteIndexCol struct {
			Seqno int    `gorm:"column:seqno"`
			Name  string `gorm:"column:name"`
		}
		var rows []sqliteIndexCol
		// PRAGMA 不支持占位符，indexName 仅来自内部常量 cidCompositeSpecs。
		if err := gdb.Raw(fmt.Sprintf("PRAGMA index_info('%s')", indexName)).Scan(&rows).Error; err != nil {
			return nil, false, err
		}
		if len(rows) == 0 {
			return nil, false, nil
		}
		cols := make([]string, 0, len(rows))
		for _, r := range rows {
			if col := strings.ToLower(strings.TrimSpace(r.Name)); col != "" {
				cols = append(cols, col)
			}
		}
		return cols, len(cols) > 0, nil
	}

	var rawCols string
	if err := gdb.Raw(`
		SELECT COALESCE(GROUP_CONCAT(LOWER(column_name) ORDER BY seq_in_index SEPARATOR ','), '')
		FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?
	`, tableName, indexName).Scan(&rawCols).Error; err != nil {
		return nil, false, err
	}
	rawCols = strings.TrimSpace(rawCols)
	if rawCols == "" {
		return nil, false, nil
	}
	parts := strings.Split(rawCols, ",")
	cols := make([]string, 0, len(parts))
	for _, p := range parts {
		if col := strings.TrimSpace(p); col != "" {
			cols = append(cols, col)
		}
	}
	return cols, len(cols) > 0, nil
}

func rebuildFilmIndex(gdb *gorm.DB, isSQLite bool, spec filmIndexSpec, exists bool) error {
	colList := strings.Join(spec.cols, ", ")
	if isSQLite {
		if exists {
			if err := gdb.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", spec.name)).Error; err != nil {
				return err
			}
		}
		return gdb.Exec(fmt.Sprintf("CREATE INDEX %s ON %s (%s)", spec.name, model.TableFilmIndex, colList)).Error
	}

	var alterSQL string
	if exists {
		alterSQL = fmt.Sprintf(
			"ALTER TABLE %s DROP INDEX %s, ADD INDEX %s (%s), ALGORITHM=INPLACE, LOCK=NONE",
			model.TableFilmIndex, spec.name, spec.name, colList,
		)
	} else {
		alterSQL = fmt.Sprintf(
			"ALTER TABLE %s ADD INDEX %s (%s), ALGORITHM=INPLACE, LOCK=NONE",
			model.TableFilmIndex, spec.name, colList,
		)
	}

	// 线上主库可能数据量大，优先走长读超时直连避免 30s 读超时中断在线 DDL。
	if db.Mdb != nil && gdb == db.Mdb && strings.TrimSpace(config.MysqlDsn) != "" {
		log.Printf("[Migration] 正在在线重建索引 %s.%s (%s)", model.TableFilmIndex, spec.name, colList)
		conn, err := openMigrationLongReadConn(config.MysqlDsn)
		if err != nil {
			return err
		}
		defer conn.Close()
		if _, err := conn.Exec("SET SESSION net_read_timeout = 1800, net_write_timeout = 1800"); err != nil {
			return err
		}
		if _, err := conn.Exec(alterSQL); err != nil {
			return err
		}
		return nil
	}
	return gdb.Exec(alterSQL).Error
}

func openMigrationLongReadConn(dsn string) (*sql.DB, error) {
	if parsed, err := mysql.ParseDSN(dsn); err == nil {
		parsed.ReadTimeout = 30 * time.Minute
		parsed.WriteTimeout = 30 * time.Minute
		dsn = parsed.FormatDSN()
	} else if strings.Contains(dsn, "readTimeout=") {
		dsn = strings.Replace(dsn, "readTimeout=30s", "readTimeout=30m", 1)
	} else if strings.Contains(dsn, "?") {
		dsn += "&readTimeout=30m"
	} else {
		dsn += "?readTimeout=30m"
	}
	conn, err := sql.Open("mysql", dsn)
	if err != nil {
		return nil, err
	}
	conn.SetMaxOpenConns(1)
	if err = conn.Ping(); err != nil {
		conn.Close()
		return nil, err
	}
	return conn, nil
}
