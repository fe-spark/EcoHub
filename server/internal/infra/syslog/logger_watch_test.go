package syslog

import (
	"context"
	"testing"
	"time"
)

func TestSubscribeWakesAndCloses(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	ch := Subscribe(ctx)

	Infof("subscribe-%s", t.Name())
	select {
	case <-ch:
	case <-time.After(time.Second):
		t.Fatal("missing wake")
	}

	cancel()
	timeout := time.After(time.Second)
	for {
		select {
		case _, ok := <-ch:
			if !ok {
				return
			}
		case <-timeout:
			t.Fatal("subscribe channel not closed")
		}
	}
}

func TestSubscribeCoalesceDoesNotBlockWriter(t *testing.T) {
	ctx, cancel := context.WithCancel(context.Background())
	defer cancel()
	_ = Subscribe(ctx)

	Infof("coalesce-1-%s", t.Name())
	Infof("coalesce-2-%s", t.Name())
	done := make(chan struct{})
	go func() {
		Infof("coalesce-3-%s", t.Name())
		close(done)
	}()
	select {
	case <-done:
	case <-time.After(time.Second):
		t.Fatal("log write blocked on subscriber")
	}
}
