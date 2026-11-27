package writer

import (
	"context"
	"errors"
	"sync/atomic"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"gorm.io/gorm"
)

func TestSavePeerFilmsIsolated_RespectsCanceledContext(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	source := &model.FilmSource{Id: "iso-src", Name: "逐片站", State: true}
	ctx, cancel := context.WithCancel(context.Background())
	cancel()

	mids, err := savePeerFilmsIsolated(ctx, source, 1, []model.MovieDetail{
		testPeerDetail(301, "取消片", "第1集"),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("期望返回 context.Canceled，实际 err=%v mids=%+v", err, mids)
	}
	if countRows(t, gdb, &model.FilmIndex{}) != 0 {
		t.Fatal("已取消的 context 不应写入任何影片")
	}
}

func TestSavePeerFilmsIsolated_MidLoopCancelStopsSecondFilm(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	source := &model.FilmSource{Id: "iso-mid-cancel", Name: "半途取消站", State: true}
	ctx, cancel := context.WithCancel(context.Background())
	origSave := savePeerFilm
	savePeerFilm = func(s *model.FilmSource, d model.MovieDetail) (int64, bool, bool, bool, error) {
		mid, isNew, playChanged, stored, err := saveSinglePeerDetailTx(s, d)
		cancel()
		return mid, isNew, playChanged, stored, err
	}
	t.Cleanup(func() { savePeerFilm = origSave })

	_, err := savePeerFilmsIsolated(ctx, source, 1, []model.MovieDetail{
		testPeerDetail(311, "第一片", "第1集"),
		testPeerDetail(312, "第二片", "第1集"),
	})
	if !errors.Is(err, context.Canceled) {
		t.Fatalf("期望第 2 片前因取消中止并返回 context.Canceled，实际=%v", err)
	}
	if countRows(t, gdb, &model.FilmIndex{}) != 1 {
		t.Fatalf("仅第 1 片应落库，实际影片数=%d", countRows(t, gdb, &model.FilmIndex{}))
	}
}

func TestSaveSinglePeerDetailTxCtx_RetriesLockTimeoutAndResetsState(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	var attempts atomic.Int32
	if err := gdb.Callback().Create().Before("gorm:create").Register("test:inject_lock_timeout_once", func(tx *gorm.DB) {
		if tx.Statement != nil && tx.Statement.Table == model.TableFilmIndex {
			if attempts.Add(1) == 1 {
				_ = tx.AddError(errors.New("Error 1205 (HY000): Lock wait timeout exceeded"))
			}
		}
	}); err != nil {
		t.Fatal(err)
	}

	source := &model.FilmSource{Id: "iso-retry", Name: "重试站", State: true}
	mid, isNew, playChanged, stored, err := saveSinglePeerDetailTxCtx(
		context.Background(),
		source,
		testPeerDetail(321, "锁重试片", "第1集"),
	)
	if err != nil {
		t.Fatalf("首次锁超时后重试应成功，实际 err=%v", err)
	}
	if attempts.Load() != 2 {
		t.Fatalf("应重试 1 次共尝试 2 次，实际 attempts=%d", attempts.Load())
	}
	if mid <= 0 || !isNew || !playChanged || !stored {
		t.Fatalf("期望正常落库标志位，actual mid=%d isNew=%t playChanged=%t stored=%t", mid, isNew, playChanged, stored)
	}
}
