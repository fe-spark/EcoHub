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

const sourceFilmListIndex = "idx_film_index_source_updated"

// EnsureSourceFilmListIndex 让后台按所属站、更新时间取一页。
// 业务连接的读超时是 30 秒，百万行上建索引会超过它。这条语句单独用更长的读超时，已存在则跳过。
func EnsureSourceFilmListIndex() error {
	if db.Mdb == nil || db.Mdb.Dialector.Name() != "mysql" {
		return nil
	}
	var n int64
	err := db.Mdb.Raw(`
		SELECT COUNT(*) FROM information_schema.statistics
		WHERE table_schema = DATABASE() AND table_name = ? AND index_name = ?
	`, "film_index", sourceFilmListIndex).Scan(&n).Error
	if err != nil {
		return err
	}
	if n > 0 {
		return nil
	}
	log.Printf("[Init] 正在建立影片列表索引 %s，片库较大时会久一些", sourceFilmListIndex)
	conn, err := openLongReadConn(config.MysqlDsn)
	if err != nil {
		return err
	}
	defer conn.Close()
	if _, err = conn.Exec("SET SESSION net_read_timeout = 1800, net_write_timeout = 1800"); err != nil {
		return err
	}
	_, err = conn.Exec("ALTER TABLE film_index ADD INDEX " + sourceFilmListIndex + " (first_source_id, update_stamp, mid), ALGORITHM=INPLACE, LOCK=NONE")
	if err == nil {
		log.Printf("[Init] 影片列表索引 %s 已就绪", sourceFilmListIndex)
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
