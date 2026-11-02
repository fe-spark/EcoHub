package repository

import (
	"strings"
	"testing"
	"time"

	"server/internal/model"
)

func TestListOpenSourceIncidents(t *testing.T) {
	setupTestDB(t)

	now := time.Date(2026, 11, 2, 15, 0, 0, 0, time.Local)
	fail := func(id, batch, name, errMsg string, at time.Time) {
		t.Helper()
		raw := `[{"sourceId":"` + id + `","sourceName":"` + name + `","status":"failed","error":"` + errMsg + `"}]`
		if err := CreateCollectRecord(&model.CollectRecord{
			BatchID: batch, Trigger: model.CollectTriggerCron, Status: model.CollectRecordStatusFailed,
			StartedAt: at, DetailJson: raw,
		}); err != nil {
			t.Fatal(err)
		}
	}
	other := func(id, batch, status string, at time.Time) {
		t.Helper()
		raw := `[{"sourceId":"` + id + `","sourceName":"` + id + `","status":"` + status + `"}]`
		if err := CreateCollectRecord(&model.CollectRecord{
			BatchID: batch, Trigger: model.CollectTriggerCron, Status: status,
			StartedAt: at, DetailJson: raw,
		}); err != nil {
			t.Fatal(err)
		}
	}

	fail("src-trip", "trip-1", "熔断站", "最新错误", now)
	fail("src-trip", "trip-2", "熔断站", "较早错误", now.Add(-time.Hour))
	fail("src-trip", "trip-3", "熔断站", "更早错误", now.Add(-2*time.Hour))

	other("src-healed", "heal-new", "done", now)
	fail("src-healed", "heal-old", "已恢复", "旧错误", now.Add(-time.Hour))

	fail("src-once", "once-1", "单次站", "一次失败", now.Add(-time.Hour))

	other("src-stop", "stop-new", "stopped", now)
	fail("src-stop", "stop-old", "停止站", "旧失败", now.Add(-time.Hour))

	fail("src-yesterday", "y-1", "昨日站", "昨日", now.Add(-24*time.Hour))
	fail("src-yesterday", "y-2", "昨日站", "昨日", now.Add(-25*time.Hour))
	fail("src-yesterday", "y-3", "昨日站", "昨日", now.Add(-26*time.Hour))

	fail("src-streak", "streak-new", "中断站", "最新失败", now)
	other("src-streak", "streak-ok", "success", now.Add(-time.Hour))
	fail("src-streak", "streak-old", "中断站", "更早失败", now.Add(-2*time.Hour))

	fail("src-old", "old-1", "过期站", "过期", now.AddDate(0, 0, -31))

	if err := CreateCollectRecord(&model.CollectRecord{
		BatchID: "empty-detail", Trigger: model.CollectTriggerManual,
		Status: model.CollectRecordStatusRunning, StartedAt: now.Add(time.Minute), DetailJson: "[]",
	}); err != nil {
		t.Fatal(err)
	}

	list, err := ListOpenSourceIncidents(now)
	if err != nil {
		t.Fatal(err)
	}
	byID := map[string]model.SourceIncident{}
	for _, inc := range list {
		byID[inc.SourceID] = inc
	}
	if _, ok := byID["src-healed"]; ok {
		t.Fatal("success after failure should be hidden")
	}
	if _, ok := byID["src-stop"]; ok {
		t.Fatal("newest stopped should be hidden")
	}
	if _, ok := byID["src-old"]; ok {
		t.Fatal("failure outside 30 days should be hidden")
	}

	trip := byID["src-trip"]
	if trip.ConsecutiveFailures != 3 || !trip.Tripped || trip.LatestError != "最新错误" || trip.LatestBatchID != "trip-1" {
		t.Fatalf("trip fold: %+v", trip)
	}
	once := byID["src-once"]
	if once.ConsecutiveFailures != 1 || once.Tripped {
		t.Fatalf("single failure: %+v", once)
	}
	streak := byID["src-streak"]
	if streak.ConsecutiveFailures != 1 || streak.Tripped || streak.LatestBatchID != "streak-new" {
		t.Fatalf("streak should stop at success: %+v", streak)
	}
	yesterday := byID["src-yesterday"]
	if yesterday.ConsecutiveFailures != 3 || !yesterday.Tripped {
		t.Fatalf("yesterday trip should stay open: %+v", yesterday)
	}

	skip, err := TrippedSourceIDsToday(now)
	if err != nil {
		t.Fatal(err)
	}
	if _, ok := skip["src-trip"]; !ok {
		t.Fatal("tripped today should be skipped")
	}
	if _, ok := skip["src-yesterday"]; ok {
		t.Fatal("yesterday trip still gets one probe today")
	}
	if _, ok := skip["src-once"]; ok {
		t.Fatal("single failure is not tripped")
	}
}

func TestListOpenSourceIncidentsCorruptDetail(t *testing.T) {
	setupTestDB(t)
	now := time.Date(2026, 11, 2, 15, 0, 0, 0, time.Local)
	if err := CreateCollectRecord(&model.CollectRecord{
		BatchID: "bad-json", Trigger: model.CollectTriggerCron,
		Status: model.CollectRecordStatusFailed, StartedAt: now, DetailJson: "{",
	}); err != nil {
		t.Fatal(err)
	}
	_, err := ListOpenSourceIncidents(now)
	if err == nil || !strings.Contains(err.Error(), "批次 bad-json 明细损坏") {
		t.Fatalf("expected corrupt detail error, got %v", err)
	}
}
