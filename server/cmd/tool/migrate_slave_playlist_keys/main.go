package main

import (
	"database/sql"
	"flag"
	"fmt"
	"log"
	"os"
	"strconv"
	"time"

	infradb "server/internal/infra/db"
	filmplaylist "server/internal/repository/film/playlist"

	_ "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
)

// 一次性历史数据迁移：先重写 movie_match_key，再把能唯一对上一部主站影片的附属站播放列表归并到该片主键。
// 同名跨类和真孤儿一律跳过。手动执行，不要放进启动流程，也不要每次采集收尾全表刷。
func main() {
	defaultPort := 3306
	if p := os.Getenv("MYSQL_PORT"); p != "" {
		if v, err := strconv.Atoi(p); err == nil && v > 0 {
			defaultPort = v
		}
	}
	host := flag.String("host", os.Getenv("MYSQL_HOST"), "MySQL Host")
	port := flag.Int("port", defaultPort, "MySQL Port")
	user := flag.String("user", os.Getenv("MYSQL_USER"), "MySQL User")
	pass := flag.String("pass", os.Getenv("MYSQL_PASSWORD"), "MySQL Password")
	dbname := flag.String("dbname", os.Getenv("MYSQL_DBNAME"), "MySQL Database Name")
	batchSize := flag.Int("batch-size", 2000, "Pagination batch size")
	dryRun := flag.Bool("dry-run", false, "Dry run mode (do not commit changes)")
	flag.Parse()

	if *host == "" || *user == "" || *dbname == "" {
		log.Fatalf("Missing required MySQL connection parameters. Please provide flags or set MYSQL_HOST, MYSQL_USER, MYSQL_PASSWORD, MYSQL_DBNAME.")
	}

	dsn := fmt.Sprintf("%s:%s@tcp(%s:%d)/%s?charset=utf8mb4&parseTime=True&loc=Local&timeout=15s",
		*user, *pass, *host, *port, *dbname)

	sqlDB, err := sql.Open("mysql", dsn)
	if err != nil {
		log.Fatalf("Connect failed: %v", err)
	}
	defer sqlDB.Close()

	if err := sqlDB.Ping(); err != nil {
		log.Fatalf("Ping failed: %v", err)
	}
	fmt.Println("Connected successfully to MySQL!")

	gormDB, err := gorm.Open(gormmysql.New(gormmysql.Config{Conn: sqlDB}), &gorm.Config{})
	if err != nil {
		log.Fatalf("GORM init failed: %v", err)
	}
	infradb.Mdb = gormDB

	startTime := time.Now()
	fmt.Printf("Starting slave playlist key migration (batch-size=%d, dry-run=%v)...\n", *batchSize, *dryRun)

	result, err := filmplaylist.MigrateSlavePlaylistKeys(*dryRun, *batchSize, log.Printf)
	if err != nil {
		log.Fatalf("Migration failed after scanned=%d migrated=%d skipped=%d: %v",
			result.Scanned, result.Migrated, result.Skipped, err)
	}

	if *dryRun {
		fmt.Printf("[Dry-Run] 播放列表扫描 %d 行，可归并 %d 行，跳过 %d 行；匹配键将重写 %d 部影片。未写库，去掉 --dry-run 才会执行。\n",
			result.Scanned, result.Migrated, result.Skipped, result.MatchKeys)
		fmt.Printf("Dry run finished in %s.\n", time.Since(startTime))
		return
	}
	fmt.Printf("Migration completed! 播放列表扫描 %d 行，归并 %d 行，跳过 %d 行；匹配键重写 %d 部影片，耗时 %s。\n",
		result.Scanned, result.Migrated, result.Skipped, result.MatchKeys, time.Since(startTime))
}
