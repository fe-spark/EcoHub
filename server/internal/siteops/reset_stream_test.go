package siteops

import (
	"context"
	"errors"
	"testing"
	"time"
)

func TestFollowSiteReset_IdleCloses(t *testing.T) {
	FinishResetProgress(nil)
	var got []ResetProgress
	err := ResetSvc.FollowSiteReset(context.Background(), func(v any) error {
		got = append(got, v.(ResetProgress))
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
		if GetResetProgress().Running {
			FinishResetProgress(nil)
		}
	})
	StartResetProgress()
	ReportResetProgress(20, "正在清空影视库存")

	got := make(chan ResetProgress, 8)
	done := make(chan struct{})
	go func() {
		defer close(done)
		err := ResetSvc.FollowSiteReset(context.Background(), func(v any) error {
			got <- v.(ResetProgress)
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

	ReportResetProgress(45, "正在清空派生数据")
	select {
	case p := <-got:
		if !p.Running || p.Percent != 45 || p.Stage != "正在清空派生数据" {
			t.Fatalf("update: %+v", p)
		}
	case <-time.After(2 * time.Second):
		t.Fatal("update missing")
	}

	FinishResetProgress(errors.New("分类同步失败"))
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
