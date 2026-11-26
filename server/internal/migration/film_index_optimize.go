package migration

import (
	"fmt"
	"log"
	"slices"
	"strings"

	"gorm.io/gorm"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
)

// unusedFilmIndexes 已不再用于 SQL 过滤或造成明显写放大的单列及超宽复合索引。
var unusedFilmIndexes = []string{
	"idx_film_index_db_id",
	"idx_film_index_original_category",
	"idx_film_index_area",
	"idx_film_index_language",
	"idx_film_index_year",
	"idx_film_index_collect_stamp",
	"idx_film_index_category_version",
	"idx_film_index_rule_version",
	"idx_filter_score",
	"idx_filter_update",
	"idx_filter_hits",
}

type filmIndexSpec struct {
	name string
	cols []string
}

var cidCompositeSpecs = []filmIndexSpec{
	{name: "idx_cid_update", cols: []string{"cid", "update_stamp", "mid"}},
	{name: "idx_cid_hits", cols: []string{"cid", "hits", "mid"}},
}

// migrateFilmIndexOptimize 先删除废弃索引以降低写放大与空间占用，再在线校正复合索引列序。
func migrateFilmIndexOptimize(gdb *gorm.DB) error {
	if gdb == nil || !gdb.Migrator().HasTable(&model.FilmIndex{}) {
		return nil
	}
	if err := dropFilmIndexes(gdb, unusedFilmIndexes); err != nil {
		return err
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

	// 线上主库数据量较大，复用长读超时直连避免 30s 读超时中断在线 DDL。
	if db.Mdb != nil && gdb == db.Mdb && strings.TrimSpace(config.MysqlDsn) != "" {
		log.Printf("[Migration] 正在在线重建索引 %s.%s (%s)", model.TableFilmIndex, spec.name, colList)
		conn, err := db.OpenLongReadConn(config.MysqlDsn)
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
