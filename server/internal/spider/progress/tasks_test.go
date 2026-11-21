package progress

import (
	"context"
	"testing"
	"time"

	"server/internal/model"
)

func TestWithStopCancel_ImmediateOnRequestStopAndMarkAllRunningStopped(t *testing.T) {
	const (
		idSingle  = "probe-stop-single"
		idAll     = "probe-stop-all"
		idStopped = "probe-already-stopped"
	)
	for _, id := range []string{idSingle, idAll, idStopped} {
		store.Delete(id)
		id := id
		t.Cleanup(func() { store.Delete(id) })
	}

	ensure(idSingle, "Single")
	ctxSingle, stopSingle := WithStopCancel(context.Background(), idSingle)
	defer stopSingle()
	RequestStop(idSingle)
	select {
	case <-ctxSingle.Done():
	case <-time.After(50 * time.Millisecond):
		t.Fatal("RequestStop should cancel probe ctx immediately")
	}

	ensure(idAll, "All")
	ctxAll, stopAll := WithStopCancel(context.Background(), idAll)
	defer stopAll()
	MarkAllRunningStopped()
	select {
	case <-ctxAll.Done():
	case <-time.After(50 * time.Millisecond):
		t.Fatal("MarkAllRunningStopped should cancel probe ctx immediately")
	}

	ensure(idStopped, "Stopped")
	Update(idStopped, func(p *model.CollectProgress) {
		p.Status = StatusStopped
	})
	ctxAlready, stopAlready := WithStopCancel(context.Background(), idStopped)
	defer stopAlready()
	if ctxAlready.Err() == nil {
		t.Fatal("WithStopCancel should return canceled ctx when source already stopped")
	}
}
