package writer

import (
	"context"
	"errors"
	"strings"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/utils"

	mysqldriver "github.com/go-sql-driver/mysql"
)

func TestIsRetryableMySQLLockErr(t *testing.T) {
	if isRetryableMySQLLockErr(nil) {
		t.Fatal("nil error is not a lock retry")
	}
	if !isRetryableMySQLLockErr(&mysqldriver.MySQLError{Number: 1213, Message: "Deadlock found when trying to get lock"}) {
		t.Fatal("1213 should retry")
	}
	if !isRetryableMySQLLockErr(&mysqldriver.MySQLError{Number: 1205, Message: "Lock wait timeout exceeded"}) {
		t.Fatal("1205 should retry")
	}
	if isRetryableMySQLLockErr(&mysqldriver.MySQLError{Number: 1406, Message: "Data too long for column 'name'"}) {
		t.Fatal("1406 stays on the isolated per-film path")
	}
	if !isRetryableMySQLLockErr(errors.New("Error 1213 (40001): Deadlock found when trying to get lock")) {
		t.Fatal("deadlock text should retry")
	}
	if !isRetryableMySQLLockErr(errors.New("Error 1205 (HY000): Lock wait timeout exceeded; try restarting transaction")) {
		t.Fatal("lock wait text should retry")
	}
	if isRetryableMySQLLockErr(errors.New("Error 1406 (22001): Data too long for column")) {
		t.Fatal("data too long text should not retry the page")
	}
}

func TestNonPrimaryNewPageUsesLightCreate(t *testing.T) {
	counter := newSQLCounter()
	gdb := openCountedPeerDB(t, counter)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	primary := model.FilmSource{Id: "src-primary", Name: "主站", Uri: "http://primary.example/api", State: true, Sort: 1}
	secondary := model.FilmSource{Id: "src-secondary", Name: "副站", Uri: "http://secondary.example/api", State: true, Sort: 2}
	if err := gdb.Create(&[]model.FilmSource{primary, secondary}).Error; err != nil {
		t.Fatal(err)
	}

	detail := testPeerDetail(8801, "副站新片", "第1集")
	wantUnix, err := utils.ParseCollectUpdateTime(detail.UpdateTime)
	if err != nil {
		t.Fatal(err)
	}

	counter.reset()
	res, err := SaveCollectedPeerDetails(context.Background(), &secondary, 1, []model.MovieDetail{detail})
	if err != nil {
		t.Fatal(err)
	}
	if len(res.Affected) != 1 {
		t.Fatalf("affected=%v", res.Affected)
	}
	if updates := countSQL(counter, "update", "film_index"); updates != 0 {
		t.Fatalf("light create updated film_index %d times", updates)
	}
	if selects := countSQL(counter, "select", "film_source_playlists"); selects != 0 {
		t.Fatalf("light create selected playlists %d times", selects)
	}

	var film model.FilmIndex
	if err := gdb.Where("mid = ?", res.Affected[0]).First(&film).Error; err != nil {
		t.Fatal(err)
	}
	if film.FirstSourceId != secondary.Id {
		t.Fatalf("first source=%s", film.FirstSourceId)
	}
	if film.Remarks != "第1集" {
		t.Fatalf("remarks=%q", film.Remarks)
	}
	if film.PlayFromSummary == "" {
		t.Fatal("play summary empty")
	}
	if film.UpdateStamp != wantUnix {
		t.Fatalf("update stamp=%d want vod time %d", film.UpdateStamp, wantUnix)
	}

	var mapping model.MovieSourceMapping
	if err := gdb.Where("source_id = ? AND source_mid = ?", secondary.Id, detail.Id).First(&mapping).Error; err != nil {
		t.Fatal(err)
	}
	if mapping.GlobalMid != film.Mid || mapping.SourceVodUnix != wantUnix {
		t.Fatalf("mapping mid=%d unix=%d", mapping.GlobalMid, mapping.SourceVodUnix)
	}
	var playlists int64
	if err := gdb.Model(&model.FilmSourcePlaylist{}).Where("mid = ? AND source_id = ?", film.Mid, secondary.Id).Count(&playlists).Error; err != nil {
		t.Fatal(err)
	}
	if playlists == 0 {
		t.Fatal("playlist missing")
	}
	var keys int64
	if err := gdb.Model(&model.MovieMatchKey{}).Where("mid = ?", film.Mid).Count(&keys).Error; err != nil {
		t.Fatal(err)
	}
	if keys == 0 {
		t.Fatal("match key missing")
	}

	counter.reset()
	if _, err := SaveCollectedPeerDetails(context.Background(), &secondary, 2, []model.MovieDetail{detail}); err != nil {
		t.Fatal(err)
	}
	if selects := countSQL(counter, "select", "film_source_playlists"); selects == 0 {
		t.Fatal("mapped replay should use the full path and read playlists")
	}

	counter.reset()
	primaryDetail := testPeerDetail(8802, "主站新片", "第1集")
	if _, err := SaveCollectedPeerDetails(context.Background(), &primary, 1, []model.MovieDetail{primaryDetail}); err != nil {
		t.Fatal(err)
	}
	if selects := countSQL(counter, "select", "film_source_playlists"); selects == 0 {
		t.Fatal("primary first page should read playlists")
	}
}

func countSQL(counter *sqlCounter, verb, table string) int {
	counter.mu.Lock()
	defer counter.mu.Unlock()
	total := 0
	for shape, n := range counter.shapes {
		text := strings.ToLower(shape)
		fields := strings.Fields(text)
		if len(fields) == 0 || fields[0] != verb || !strings.Contains(text, table) {
			continue
		}
		total += n
	}
	return total
}
