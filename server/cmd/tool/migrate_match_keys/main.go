package main

import (
	"database/sql"
	"flag"
	"fmt"
	_ "github.com/go-sql-driver/mysql"
	gormmysql "gorm.io/driver/mysql"
	"gorm.io/gorm"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	infradb "server/internal/infra/db"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"
)

func resolveMasterRootPid(pid, cid int64, cName string) int64 {
	rootId := support.GetRootId(pid)
	if rootId <= 0 && cid > 0 {
		rootId = support.GetRootId(cid)
	}
	if rootId <= 0 && strings.TrimSpace(cName) != "" {
		rootId = support.ResolveRootCategoryIDByCName(cName)
	}
	return rootId
}

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
	pageSize := flag.Int("batch-size", 500, "Pagination batch size")
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
		log.Printf("GORM init warning: %v", err)
	} else {
		infradb.Mdb = gormDB
		support.RefreshCategoryCache()
	}

	type filmItem struct {
		ID    int64
		Mid   int64
		Name  string
		Pid   int64
		Cid   int64
		DbId  int64
		CName string
	}

	lastID := int64(0)
	totalUpdated := 0
	startTime := time.Now()

	fmt.Printf("Starting match key migration (batch-size=%d, dry-run=%v)...\n", *pageSize, *dryRun)

	for {
		// 基于主键游标流式分页，内存占用恒定 O(1)，杜绝全量拉取引发的 OOM
		rows, err := sqlDB.Query(`
			SELECT id, mid, name, pid, cid, db_id, c_name 
			FROM film_index 
			WHERE id > ? AND deleted_at IS NULL 
			ORDER BY id ASC 
			LIMIT ?
		`, lastID, *pageSize)
		if err != nil {
			log.Fatalf("Query film_index failed at lastID=%d: %v", lastID, err)
		}

		var chunk []filmItem
		for rows.Next() {
			var f filmItem
			if err := rows.Scan(&f.ID, &f.Mid, &f.Name, &f.Pid, &f.Cid, &f.DbId, &f.CName); err != nil {
				rows.Close()
				log.Fatalf("Scan failed at lastID=%d: %v", lastID, err)
			}
			chunk = append(chunk, f)
			lastID = f.ID
		}
		rows.Close()

		if len(chunk) == 0 {
			break
		}

		if *dryRun {
			fmt.Printf("[Dry-Run] 预览批次匹配键生成 (当前游标 ID=%d, 批数据量=%d):\n", lastID, len(chunk))
			for i, f := range chunk {
				if i >= 10 {
					break
				}
				pid := resolveMasterRootPid(f.Pid, f.Cid, f.CName)
				keys := shared.BuildMovieMatchKeysWithCategory(f.DbId, f.Name, pid)
				fmt.Printf("MID=%d Name='%s' Pid=%d Keys=%v\n", f.Mid, f.Name, pid, keys)
			}
			return
		}

		mids := make([]int64, 0, len(chunk))
		type matchRecord struct {
			Mid      int64
			MatchKey string
		}
		var newRecords []matchRecord

		for _, f := range chunk {
			mids = append(mids, f.Mid)
			pid := resolveMasterRootPid(f.Pid, f.Cid, f.CName)
			keys := shared.BuildMovieMatchKeysWithCategory(f.DbId, f.Name, pid)
			for _, k := range keys {
				newRecords = append(newRecords, matchRecord{Mid: f.Mid, MatchKey: k})
			}
		}

		// 局部小事务批处理更新，控制锁持有时间
		tx, err := sqlDB.Begin()
		if err != nil {
			log.Fatalf("Begin tx failed at lastID=%d: %v", lastID, err)
		}

		placeholders := strings.Repeat("?,", len(mids))
		placeholders = placeholders[:len(placeholders)-1]
		midArgs := make([]any, len(mids))
		for idx, m := range mids {
			midArgs[idx] = m
		}

		deleteQuery := fmt.Sprintf("DELETE FROM movie_match_key WHERE mid IN (%s)", placeholders)
		if _, err := tx.Exec(deleteQuery, midArgs...); err != nil {
			tx.Rollback()
			log.Fatalf("Delete old match keys failed: %v", err)
		}

		if len(newRecords) > 0 {
			insertQuery := "INSERT IGNORE INTO movie_match_key (mid, match_key, created_at, updated_at) VALUES "
			var insertArgs []any
			for rIdx, r := range newRecords {
				if rIdx > 0 {
					insertQuery += ","
				}
				insertQuery += "(?, ?, NOW(), NOW())"
				insertArgs = append(insertArgs, r.Mid, r.MatchKey)
			}
			if _, err := tx.Exec(insertQuery, insertArgs...); err != nil {
				tx.Rollback()
				log.Fatalf("Insert new match keys failed: %v", err)
			}
		}

		if err := tx.Commit(); err != nil {
			log.Fatalf("Commit failed at lastID=%d: %v", lastID, err)
		}

		totalUpdated += len(chunk)
		if totalUpdated%1000 == 0 {
			fmt.Printf("Progress: %d films migrated (lastID=%d, %s elapsed)\n", totalUpdated, lastID, time.Since(startTime))
		}
	}

	fmt.Printf("Migration completed successfully! Processed %d films in %s.\n", totalUpdated, time.Since(startTime))
}
