package repository

import (
	"database/sql"
	"errors"
	"log"
	"strings"

	"github.com/go-sql-driver/mysql"

	"server/internal/config"
	"server/internal/infra/db"
)

// SourceFilmListIndex 后台按所属站、更新时间取一页，非主站搜索也按它分批往后扫。
const SourceFilmListIndex = "idx_film_index_source_updated"

// FilmCategoryScoreIndex 排片按分类键、评分取前几部。
const FilmCategoryScoreIndex = "idx_film_index_cat_score"

// EnsureSourceFilmListIndex 让后台按所属站、更新时间取一页。
// 业务连接的读超时是 30 秒，百万行上建索引会超过它。这条语句单独用更长的读超时，已存在则跳过。
func EnsureSourceFilmListIndex() error {
	return ensureFilmIndex(SourceFilmListIndex, "first_source_id, update_stamp, mid")
}

// EnsureFilmCategoryScoreIndex 让一键排片按分类键和评分取前几部。
// 不写进模型标签，避免启动时用 30 秒读超时去建这个索引。
func EnsureFilmCategoryScoreIndex() error {
	return ensureFilmIndex(FilmCategoryScoreIndex, "category_key, score, hits, mid")
}

func ensureFilmIndex(name, columns string) error {
	if db.Mdb == nil || db.Mdb.Dialector.Name() != "mysql" {
		return nil
	}
	var n int64
	err := db.Mdb.Raw(`
		SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?
	`, "film_index", name).Scan(&n).Error
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	log.Printf("[Init] 正在建立索引 %s，片库较大时会久一些", name)
	conn, err := openLongReadConn(config.MysqlDsn)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.Exec("SET SESSION net_read_timeout = 1800, net_write_timeout = 1800"); err != nil {
		return err
	}
	_, err = conn.Exec("ALTER TABLE film_index ADD INDEX " + name + " (" + columns + "), ALGORITHM=INPLACE, LOCK=NONE")
	if err == nil {
		log.Printf("[Init] 索引 %s 已就绪", name)
		return nil
	}
	var mysqlErr *mysql.MySQLError
	if errors.As(err, &mysqlErr) && mysqlErr.Number == 1061 {
		return nil
	}
	return err
}

func openLongReadConn(dsn string) (*sql.DB, error) {
	if strings.Contains(dsn, "readTimeout=") {
		dsn = strings.Replace(dsn, "readTimeout=30s", "readTimeout=30m", 1)
	} else {
		dsn += "&readTimeout=30m"
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
