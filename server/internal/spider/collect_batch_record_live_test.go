package spider

import (
	"testing"
	"time"

	"server/internal/model"
	"server/internal/spider/progress"
)

func TestBatchSourceLiveDetailShowsFailedSource(t *testing.T) {
	src := model.FilmSource{Id: "live-fail-src", Name: "樱花"}
	progress.MarkSourcesCollectStarting([]model.FilmSource{src}, "queue-live-fail")
	progress.Update(src.Id, func(cur *model.CollectProgress) {
		cur.Status = progress.StatusFailed
		cur.Success = 0
		cur.Failed = 1
		cur.FailedPages = []int{1}
	})
	batch := newCollectBatchContext(model.CollectTriggerManual, "", []model.FilmSource{src}, time.Now())
	t.Cleanup(func() { unregisterActiveBatch(batch) })
	batch.recordSourceSnapshot(model.CollectRecordSourceDetail{
		SourceID:   src.Id,
		SourceName: src.Name,
		Status:     progress.StatusRunning,
		FilmsTotal: 4,
	})
	batch.RecordSourceError(src.Id, "连接失败")

	detail, ok := BatchSourceLiveDetail(batch.batchID, src.Id)
	if !ok {
		t.Fatal("本批进行中应返回该站明细")
	}
	if detail.Status != progress.StatusFailed {
		t.Fatalf("status=%s，采集中心已是失败", detail.Status)
	}
	if detail.Error != "连接失败" {
		t.Fatalf("error=%q", detail.Error)
	}
	if detail.FilmsTotal != 4 {
		t.Fatalf("films=%d，实时状态不能清掉本批已记下的部数", detail.FilmsTotal)
	}
	if detail.FailedCnt != 1 || len(detail.FailedPages) != 1 {
		t.Fatalf("failed=%d pages=%v", detail.FailedCnt, detail.FailedPages)
	}

	batch.recordSourceSnapshot(model.CollectRecordSourceDetail{
		SourceID:   src.Id,
		SourceName: src.Name,
		Status:     progress.StatusFailed,
		FilmsTotal: 4,
		Error:      "连接失败",
	})
	progress.MarkSourcesCollectStarting([]model.FilmSource{src}, "queue-live-next")
	next, ok := BatchSourceLiveDetail(batch.batchID, src.Id)
	if !ok || next.Status != progress.StatusFailed {
		t.Fatalf("下一轮进度不应盖住本批，ok=%v status=%s", ok, next.Status)
	}
}
