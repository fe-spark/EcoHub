package catalog

import (
	"database/sql"
	"errors"
	"log"
	"strings"
	"time"

	"github.com/go-sql-driver/mysql"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
)

// FilmCategoryScoreIndex 排片按分类键、评分取前几部。
const FilmCategoryScoreIndex = "idx_film_index_cat_score"

// FilmSourceDeletedIndex 统计各采集站未删除影片数走覆盖索引，避免全表回表超时。
const FilmSourceDeletedIndex = "idx_film_index_source_del"

// FilmCategoryDeletedIndex 统计各分类未删除影片数走覆盖索引，避免非主站百万数据回表。
const FilmCategoryDeletedIndex = "idx_film_index_cat_del"

// CollectRecordFilmSourceIndex 让删站清理历史采集影片清单按 source_id 走索引，避免全表扫删。
const CollectRecordFilmSourceIndex = "idx_crf_source_id"

// EnsureFilmCategoryScoreIndex 让一键排片按分类键和评分取前几部。
// 不写进模型标签，避免启动时用 30 秒读超时去建这个索引。
func EnsureFilmCategoryScoreIndex() error {
	return ensureTableIndex(model.TableFilmIndex, FilmCategoryScoreIndex, "category_key, score, hits, mid")
}

// EnsureFilmSourceDeletedIndex 让按 first_source_id 统计未删除影片数的 GROUP BY 查询走覆盖索引。
// 不写进模型标签，避免启动时用 30 秒读超时去建这个索引；若表上已有同列前缀索引（如手动建的 idx_sourceid_del）则跳过。
func EnsureFilmSourceDeletedIndex() error {
	return ensureTablePrefixIndex(model.TableFilmIndex, FilmSourceDeletedIndex, "first_source_id,deleted_at", "first_source_id, deleted_at")
}

// EnsureFilmCategoryDeletedIndex 让按 category_key 统计未删除影片数的 GROUP BY 查询走覆盖索引。
// 不写进模型标签，避免启动时用 30 秒读超时去建这个索引；若表上已有同列前缀索引则跳过。
func EnsureFilmCategoryDeletedIndex() error {
	return ensureTablePrefixIndex(model.TableFilmIndex, FilmCategoryDeletedIndex, "category_key,deleted_at", "category_key, deleted_at")
}

func ensureTablePrefixIndex(tableName, name, colPrefix, columns string) error {
	if db.Mdb == nil || db.Mdb.Dialector == nil || db.Mdb.Dialector.Name() != "mysql" {
		return nil
	}
	var n int64
	err := db.Mdb.Raw(`
		SELECT COUNT(*) FROM (
			SELECT index_name
			FROM information_schema.statistics
			WHERE table_schema = DATABASE() AND table_name = ?
			GROUP BY index_name
			HAVING GROUP_CONCAT(column_name ORDER BY seq_in_index) LIKE ?
		) AS matched
	`, tableName, colPrefix+"%").Scan(&n).Error
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	return ensureTableIndex(tableName, name, columns)
}

// EnsureCollectRecordFilmSourceIndex 为 collect_record_film_items 补建 source_id 索引。
// 不写进模型标签，避免启动 AutoMigrate 时用 30 秒读超时在大表上建索引超时。
func EnsureCollectRecordFilmSourceIndex() error {
	return ensureTableIndex(model.TableCollectRecordFilm, CollectRecordFilmSourceIndex, "source_id")
}

func ensureTableIndex(tableName, name, columns string) error {
	if db.Mdb == nil || db.Mdb.Dialector.Name() != "mysql" {
		return nil
	}
	var n int64
	err := db.Mdb.Raw(`
		SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?
	`, tableName, name).Scan(&n).Error
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	log.Printf("[Init] 正在建立索引 %s.%s，数据较大时会久一些", tableName, name)
	conn, err := OpenLongReadConn(config.MysqlDsn)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.Exec("SET SESSION net_read_timeout = 1800, net_write_timeout = 1800"); err != nil {
		return err
	}
	_, err = conn.Exec("ALTER TABLE " + tableName + " ADD INDEX " + name + " (" + columns + "), ALGORITHM=INPLACE, LOCK=NONE")
	if err == nil {
		log.Printf("[Init] 索引 %s.%s 已就绪", tableName, name)
		return nil
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1061 {
		return nil
	}
	return err
}

// OpenLongReadConn 建立带长读超时的直连 MySQL 连接（30 分钟），供建索引与大表统计使用。
func OpenLongReadConn(dsn string) (*sql.DB, error) {
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
