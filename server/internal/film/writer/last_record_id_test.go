package writer

import (
	"context"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
)

func TestSaveCollectedPeerDetails_InTxLastRecordID(t *testing.T) {
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

	// 1. 新建影片在 INSERT 时直接写入 last_record_id（无额外 UPDATE film_index）
	detail := testPeerDetail(9001, "测试大片", "第1集")
	counter.reset()
	res, err := SaveCollectedPeerDetails(WithCollectRecordID(context.Background(), 101), &primary, 1, []model.MovieDetail{detail})
	if err != nil || len(res.Affected) != 1 {
		t.Fatalf("create failed: res=%+v err=%v", res, err)
	}
	if updates := countSQL(counter, "update", "film_index"); updates != 0 {
		t.Fatalf("new film should not issue extra UPDATE film_index, got %d", updates)
	}
	var film model.FilmIndex
	if err := gdb.Where("mid = ?", res.Affected[0]).First(&film).Error; err != nil {
		t.Fatal(err)
	}
	if film.LastRecordID != 101 {
		t.Fatalf("expected last_record_id=101 after create, got %d", film.LastRecordID)
	}

	// 2. 无变更重放：不更新 last_record_id
	counter.reset()
	resUnchanged, err := SaveCollectedPeerDetails(WithCollectRecordID(context.Background(), 102), &primary, 1, []model.MovieDetail{detail})
	if err != nil || len(resUnchanged.Affected) != 0 {
		t.Fatalf("unchanged replay expected 0 affected, got %+v err=%v", resUnchanged, err)
	}
	if err := gdb.Where("mid = ?", film.Mid).First(&film).Error; err != nil {
		t.Fatal(err)
	}
	if film.LastRecordID != 101 {
		t.Fatalf("unchanged film should keep last_record_id=101, got %d", film.LastRecordID)
	}

	// 3. 仅线路追更：在 refreshRemarksAndPlaySummaryWithStampTx 的同一条 UPDATE 中同步更新 last_record_id
	lineUpdate := testPeerDetail(9001, "测试大片", "第2集")
	lineUpdate.UpdateTime = "2026-11-28 12:00:00"
	counter.reset()
	resLine, err := SaveCollectedPeerDetails(WithCollectRecordID(context.Background(), 103), &primary, 1, []model.MovieDetail{lineUpdate})
	if err != nil || len(resLine.Affected) != 1 {
		t.Fatalf("line update failed: res=%+v err=%v", resLine, err)
	}
	if updates := countSQL(counter, "update", "film_index"); updates != 1 {
		t.Fatalf("line update should issue single UPDATE film_index, got %d", updates)
	}
	if err := gdb.Where("mid = ?", film.Mid).First(&film).Error; err != nil {
		t.Fatal(err)
	}
	if film.LastRecordID != 103 {
		t.Fatalf("expected last_record_id=103 after line update, got %d", film.LastRecordID)
	}

	// 4. 仅元数据变更：在 syncOwnedFilmFieldsTx 的同一条 UPDATE 中同步更新 last_record_id
	metaUpdate := lineUpdate
	metaUpdate.Area = "中国"
	metaUpdate.UpdateTime = "2026-11-28 13:00:00"
	counter.reset()
	resMeta, err := SaveCollectedPeerDetails(WithCollectRecordID(context.Background(), 104), &primary, 1, []model.MovieDetail{metaUpdate})
	if err != nil || len(resMeta.Affected) != 1 {
		t.Fatalf("meta update failed: res=%+v err=%v", resMeta, err)
	}
	if updates := countSQL(counter, "update", "film_index"); updates != 1 {
		t.Fatalf("meta update should issue single UPDATE film_index, got %d", updates)
	}
	if err := gdb.Where("mid = ?", film.Mid).First(&film).Error; err != nil {
		t.Fatal(err)
	}
	if film.LastRecordID != 104 {
		t.Fatalf("expected last_record_id=104 after meta update, got %d", film.LastRecordID)
	}

	// 5. 未传 CollectRecordID（如人工编辑）及非主站写入（上下文 recordID=0）保持 last_record_id 不变 / 为 0
	secDetail := testPeerDetail(9002, "副站新片", "第1集")
	secRes, err := SaveCollectedPeerDetails(context.Background(), &secondary, 1, []model.MovieDetail{secDetail})
	if err != nil || len(secRes.Affected) != 1 {
		t.Fatalf("secondary create failed: %+v err=%v", secRes, err)
	}
	var secFilm model.FilmIndex
	if err := gdb.Where("mid = ?", secRes.Affected[0]).First(&secFilm).Error; err != nil {
		t.Fatal(err)
	}
	if secFilm.LastRecordID != 0 {
		t.Fatalf("secondary film without CollectRecordID should have last_record_id=0, got %d", secFilm.LastRecordID)
	}
}
