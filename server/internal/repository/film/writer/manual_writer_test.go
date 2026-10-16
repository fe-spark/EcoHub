package writer

import (
	"context"
	"sync"
	"testing"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
)

func TestSaveDetail_NewFilm_NoDeadlock(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	source := model.FilmSource{Id: "test_source", Name: "测试源", State: true}
	if err := gdb.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}

	detail := model.MovieDetail{
		Name:            "死锁测试影片",
		MovieDescriptor: model.MovieDescriptor{UpdateTime: "2026-10-16 12:00:00"},
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "第01集", Link: "http://play/1"}},
		},
	}

	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() {
		done <- SaveDetail("test_source", detail)
	}()

	select {
	case <-ctx.Done():
		t.Fatal("SaveDetail timed out (deadlock on write lock)")
	case err := <-done:
		if err != nil {
			t.Fatalf("SaveDetail failed: %v", err)
		}
	}

	var count int64
	if err := gdb.Model(&model.FilmIndex{}).Where("name = ?", "死锁测试影片").Count(&count).Error; err != nil {
		t.Fatal(err)
	}
	if count != 1 {
		t.Fatalf("expected 1 film created, got %d", count)
	}
}

func TestSaveDetail_ManualUpdate_NoDeadlock(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	source := model.FilmSource{Id: "test_source", Name: "测试源", State: true}
	if err := gdb.Create(&source).Error; err != nil {
		t.Fatalf("create source: %v", err)
	}

	fi := model.FilmIndex{
		FilmIndexIdentity: model.FilmIndexIdentity{
			Mid:           1001,
			FirstSourceId: "test_source",
		},
		FilmIndexContent: model.FilmIndexContent{
			Name: "原始影片",
		},
	}
	if err := gdb.Create(&fi).Error; err != nil {
		t.Fatalf("create film: %v", err)
	}

	detail := model.MovieDetail{
		Id:              1001,
		Name:            "更新后影片",
		MovieDescriptor: model.MovieDescriptor{UpdateTime: "2026-10-16 13:00:00"},
		PlayList: [][]model.MovieUrlInfo{
			{{Episode: "第01集", Link: "http://play/updated"}},
		},
	}

	done := make(chan error, 1)
	ctx, cancel := context.WithTimeout(context.Background(), 3*time.Second)
	defer cancel()

	go func() {
		done <- SaveDetail("test_source", detail)
	}()

	select {
	case <-ctx.Done():
		t.Fatal("SaveDetail manual update timed out")
	case err := <-done:
		if err != nil {
			t.Fatalf("SaveDetail update failed: %v", err)
		}
	}

	var updated model.FilmIndex
	if err := gdb.Where("mid = ?", 1001).First(&updated).Error; err != nil {
		t.Fatal(err)
	}
	if updated.Name != "更新后影片" {
		t.Fatalf("expected name to be '更新后影片', got %s", updated.Name)
	}
}

func TestSaveDetail_ConcurrentSaveAndCollect_NoDeadlock(t *testing.T) {
	gdb := openPeerPageDB(t)
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	sourceA := model.FilmSource{Id: "src-a", Name: "源站A", Uri: "http://src-a", State: true}
	sourceB := model.FilmSource{Id: "src-b", Name: "源站B", Uri: "http://src-b", State: true}
	if err := gdb.Create(&sourceA).Error; err != nil {
		t.Fatal(err)
	}
	if err := gdb.Create(&sourceB).Error; err != nil {
		t.Fatal(err)
	}

	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	var wg sync.WaitGroup
	errCh := make(chan error, 4)

	// 并发1：手动新增影片
	wg.Add(1)
	go func() {
		defer wg.Done()
		d := model.MovieDetail{
			Name: "并发电影1",
			PlayList: [][]model.MovieUrlInfo{
				{{Episode: "01", Link: "http://a/1"}},
			},
		}
		if err := SaveDetail("src-a", d); err != nil {
			errCh <- err
		}
	}()

	// 并发2：采集站整页入库同一名字的片
	wg.Add(1)
	go func() {
		defer wg.Done()
		d := model.MovieDetail{
			Id:   200,
			Name: "并发电影1",
			PlayList: [][]model.MovieUrlInfo{
				{{Episode: "02", Link: "http://b/2"}},
			},
		}
		if _, err := SaveCollectedPeerDetails(context.Background(), &sourceB, 1, []model.MovieDetail{d}); err != nil {
			errCh <- err
		}
	}()

	finished := make(chan struct{})
	go func() {
		wg.Wait()
		close(finished)
	}()

	select {
	case <-ctx.Done():
		t.Fatal("concurrent SaveDetail and collect timed out, deadlock detected")
	case <-finished:
	}

	close(errCh)
	for err := range errCh {
		if err != nil {
			t.Fatalf("unexpected error: %v", err)
		}
	}
}
