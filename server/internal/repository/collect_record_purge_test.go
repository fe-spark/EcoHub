package repository

import (
	"strings"
	"testing"
	"time"

	"server/internal/model"

	"gorm.io/gorm"
)

func TestPurgeKeepsRunningAndInterruptedStatus(t *testing.T) {
	gdb := setupTestDB(t)
	if err := gdb.AutoMigrate(&model.FilmSource{}, &model.CollectSourceStats{}); err != nil {
		t.Fatal(err)
	}
	for _, src := range []model.FilmSource{
		{Id: "src-a", Name: "A", Uri: "http://a.example"},
		{Id: "src-b", Name: "B", Uri: "http://b.example"},
	} {
		if err := gdb.Create(&src).Error; err != nil {
			t.Fatal(err)
		}
	}
	now := time.Now()
	running := &model.CollectRecord{
		BatchID:    "batch-running",
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusRunning,
		StartedAt:  now,
		DetailJson: `[{"sourceId":"src-a","status":"done","pageTotal":1,"successCnt":1},{"sourceId":"src-b","status":"running","pageTotal":4,"successCnt":2}]`,
	}
	interrupted := &model.CollectRecord{
		BatchID:    "batch-interrupted",
		Trigger:    model.CollectTriggerManual,
		Status:     model.CollectRecordStatusInterrupted,
		ErrorMsg:   "服务重启，任务异常中断",
		StartedAt:  now,
		DetailJson: `[{"sourceId":"src-a","status":"interrupted","pageTotal":3,"successCnt":1},{"sourceId":"src-b","status":"interrupted","pageTotal":5,"successCnt":4}]`,
	}
	if err := CreateCollectRecord(running); err != nil {
		t.Fatal(err)
	}
	if err := CreateCollectRecord(interrupted); err != nil {
		t.Fatal(err)
	}
	if err := BatchSaveCollectRecordFilms("batch-running", map[string][]int64{"src-a": {1}, "src-b": {2}}); err != nil {
		t.Fatal(err)
	}
	if err := BatchSaveCollectRecordFilms("batch-interrupted", map[string][]int64{"src-a": {3}, "src-b": {4}}); err != nil {
		t.Fatal(err)
	}

	if err := gdb.Transaction(func(tx *gorm.DB) error {
		return purgeSourceCollectHistoryTx(tx, "src-a")
	}); err != nil {
		t.Fatal(err)
	}

	runningLeft, err := GetCollectRecordByBatchID("batch-running")
	if err != nil {
		t.Fatal(err)
	}
	if runningLeft.Status != model.CollectRecordStatusRunning || strings.Contains(runningLeft.DetailJson, "src-a") {
		t.Fatalf("running record status=%s detail=%s", runningLeft.Status, runningLeft.DetailJson)
	}
	if runningLeft.PageSuccess != 2 || runningLeft.FilmCount != 1 {
		t.Fatalf("running counts pageSuccess=%d films=%d", runningLeft.PageSuccess, runningLeft.FilmCount)
	}

	interruptedLeft, err := GetCollectRecordByBatchID("batch-interrupted")
	if err != nil {
		t.Fatal(err)
	}
	if interruptedLeft.Status != model.CollectRecordStatusInterrupted || interruptedLeft.ErrorMsg != "服务重启，任务异常中断" {
		t.Fatalf("interrupted status=%s err=%s", interruptedLeft.Status, interruptedLeft.ErrorMsg)
	}
	if strings.Contains(interruptedLeft.DetailJson, "src-a") || interruptedLeft.PageSuccess != 4 {
		t.Fatalf("interrupted detail=%s pageSuccess=%d", interruptedLeft.DetailJson, interruptedLeft.PageSuccess)
	}
}
