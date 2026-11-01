package service

import (
	"context"
	"errors"
	"testing"
	"time"

	filmrepo "server/internal/repository/film"
)

func TestFollowSiteReset_IdleCloses(t *testing.T) {
	filmrepo.FinishResetProgress(nil)
	var got []filmrepo.ResetProgress
	err := SpiderSvc.FollowSiteReset(context.Background(), func(v any) error {
		got = append(got, v.(filmrepo.ResetProgress))
		return nil
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(got) != 1 || got[0].Running {
		t.Fatalf("idle follow: %+v", got)
	}
}

func TestFollowSiteReset_ReplaysThenFollowsUntilDone(t *testing.T) {
	t.Cleanup(func() {
		if filmrepo.GetResetProgress().Running {
			filmrepo.FinishResetProgress(nil)
		}
	})
	filmrepo.StartResetProgress()
	filmrepo.ReportResetProgress(20, "正在清空影视库存")

	got := make(chan filmrepo.ResetProgress, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := SpiderSvc.FollowSiteReset(context.Background(), func(v any) error {
			got <- v.(filmrepo.ResetProgress)
			return nil
		})
		if err != nil {
			t.Errorf("follow: %v", err)
		}
	}()

	select {
	case p := <-got:
		if !p.Running || p.Percent != 20 || p.Stage != "正在清空影视库存" {
			t.Fatalf("snapshot: %+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("snapshot missing")
	}

	filmrepo.ReportResetProgress(45, "正在清空派生数据")
	select {
	case p := <-got:
		if !p.Running || p.Percent != 45 || p.Stage != "正在清空派生数据" {
			t.Fatalf("update: %+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("update missing")
	}

	filmrepo.FinishResetProgress(errors.New("分类同步失败"))
	select {
	case p := <-got:
		if p.Running || p.Error != "分类同步失败" {
			t.Fatalf("finish: %+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("finish missing")
	}

	select {
	case <-done:
	case <-time.After(2 * time.Second):
		t.Fatal("follow did not return")
	}
}
