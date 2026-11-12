package service

import "testing"

func TestStartCollectMatchesSingleSiteBatch(t *testing.T) {
	id := "missing-site"
	batch := SpiderSvc.BatchCollect(-1, []string{id})
	err := SpiderSvc.StartCollect(id, -1)
	if err == nil {
		t.Fatal("missing site should fail")
	}
	if len(batch.Started) != 0 || len(batch.Skipped) != 1 || batch.Skipped[0].Reason != err.Error() {
		t.Fatalf("batch=%+v err=%v", batch, err)
	}

	many := SpiderSvc.BatchCollect(-1, []string{"missing-a", "missing-b"})
	if len(many.Started) != 0 || len(many.Skipped) != 2 {
		t.Fatalf("batch=%+v", many)
	}
	if many.Skipped[0].Reason != batch.Skipped[0].Reason || many.Skipped[1].Reason != batch.Skipped[0].Reason {
		t.Fatalf("single %q batch %q %q", batch.Skipped[0].Reason, many.Skipped[0].Reason, many.Skipped[1].Reason)
	}
}
