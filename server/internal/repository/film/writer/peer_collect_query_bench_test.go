package writer

import (
	"context"
	"fmt"
	"strings"
	"sync"
	"sync/atomic"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/support"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

// sqlCounter 统计一次采集落库打到 sqlite 的语句数，并把 SQL 压成形状方便看是不是逐片查询。
type sqlCounter struct {
	n      atomic.Int64
	mu     sync.Mutex
	shapes map[string]int
	level  logger.LogLevel
}

func newSQLCounter() *sqlCounter {
	return &sqlCounter{shapes: map[string]int{}, level: logger.Info}
}

func (c *sqlCounter) LogMode(level logger.LogLevel) logger.Interface {
	c.level = level
	return c
}

func (c *sqlCounter) Info(context.Context, string, ...interface{})  {}
func (c *sqlCounter) Warn(context.Context, string, ...interface{})  {}
func (c *sqlCounter) Error(context.Context, string, ...interface{}) {}

func (c *sqlCounter) Trace(_ context.Context, _ time.Time, fc func() (string, int64), _ error) {
	sql, _ := fc()
	c.n.Add(1)
	shape := sqlShape(sql)
	c.mu.Lock()
	c.shapes[shape]++
	c.mu.Unlock()
}

func (c *sqlCounter) reset() {
	c.n.Store(0)
	c.mu.Lock()
	c.shapes = map[string]int{}
	c.mu.Unlock()
}

func sqlShape(sql string) string {
	sql = strings.Join(strings.Fields(sql), " ")
	if len(sql) > 180 {
		sql = sql[:180]
	}
	return sql
}

func openCountedPeerDB(t testing.TB, counter *sqlCounter) *gorm.DB {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{
		Logger: counter,
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	sqlDB, err := gdb.DB()
	if err != nil {
		t.Fatal(err)
	}
	sqlDB.SetMaxOpenConns(1)
	if err := gdb.AutoMigrate(
		&model.FilmSource{},
		&model.FilmIndex{},
		&model.MovieMatchKey{},
		&model.MovieSourceMapping{},
		&model.FilmSourcePlaylist{},
		&model.Category{},
		&model.CategoryMapping{},
		&model.SourceCategory{},
	); err != nil {
		t.Fatalf("migrate: %v", err)
	}
	_ = gdb.Create(&model.Category{Id: 1, Pid: 0, Name: "电影", Show: true}).Error
	db.Mdb = gdb
	support.RefreshCategoryCache()
	return gdb
}

func replayPage(b *testing.B, films int) {
	counter := newSQLCounter()
	gdb := openCountedPeerDB(b, counter)
	prev := db.Mdb
	db.Mdb = gdb
	b.Cleanup(func() { db.Mdb = prev })

	source := &model.FilmSource{Id: "src-bench", Name: "测站", State: true}
	page := make([]model.MovieDetail, 0, films)
	for i := 1; i <= films; i++ {
		page = append(page, testPeerDetail(int64(i), fmt.Sprintf("影片%d", i), "第1集"))
	}
	if _, err := SaveCollectedPeerDetails(context.Background(), source, 1, page); err != nil {
		b.Fatal(err)
	}
	counter.reset()
	b.ResetTimer()
	for i := 0; i < b.N; i++ {
		if _, err := SaveCollectedPeerDetails(context.Background(), source, 2, page); err != nil {
			b.Fatal(err)
		}
	}
	b.StopTimer()
	b.ReportMetric(float64(counter.n.Load())/float64(b.N), "sql/op")
}

func BenchmarkReplayUnchangedPage20(b *testing.B) { replayPage(b, 20) }
func BenchmarkReplayUnchangedPage50(b *testing.B) { replayPage(b, 50) }

func TestReplayUnchangedPageSQLShapes(t *testing.T) {
	counter := newSQLCounter()
	gdb := openCountedPeerDB(t, counter)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	const films = 40
	source := &model.FilmSource{Id: "src-shape", Name: "测站", State: true}
	page := make([]model.MovieDetail, 0, films)
	for i := 1; i <= films; i++ {
		page = append(page, testPeerDetail(int64(i), fmt.Sprintf("影片%d", i), "第1集"))
	}
	if _, err := SaveCollectedPeerDetails(context.Background(), source, 1, page); err != nil {
		t.Fatal(err)
	}
	counter.reset()
	start := time.Now()
	got, err := SaveCollectedPeerDetails(context.Background(), source, 2, page)
	if err != nil {
		t.Fatal(err)
	}
	elapsed := time.Since(start)
	if len(got.Affected) != 0 || len(got.Notify) != 0 {
		t.Fatalf("unchanged replay counted as a write: affected=%v notify=%v", got.Affected, got.Notify)
	}
	t.Logf("films=%d sql=%d elapsed=%s", films, counter.n.Load(), elapsed)
	type pair struct {
		n int
		s string
	}
	var rows []pair
	counter.mu.Lock()
	for s, n := range counter.shapes {
		rows = append(rows, pair{n, s})
	}
	counter.mu.Unlock()
	for i := 0; i < len(rows); i++ {
		for j := i + 1; j < len(rows); j++ {
			if rows[j].n > rows[i].n {
				rows[i], rows[j] = rows[j], rows[i]
			}
		}
	}
	for _, row := range rows {
		t.Logf("%4d  %s", row.n, row.s)
	}
	// 40 部无变化回放应是常数次查询。逐片查身份、vod_time、匹配键时这里会到 160 以上。
	if counter.n.Load() > 16 {
		t.Fatalf("unchanged page issued %d SQL statements, want <= 16", counter.n.Load())
	}
}
