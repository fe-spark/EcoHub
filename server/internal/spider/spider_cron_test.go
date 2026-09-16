package spider

import (
	"testing"

	"server/internal/model"
)

func TestAddLogCleanCron_ValidAndInvalid(t *testing.T) {
	// Valid spec
	cid, err := AddLogCleanCron("test_log_clean", "0 0 3 * * *")
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if cid < 0 {
		t.Fatalf("expected valid entry id, got %v", cid)
	}
	CronCollect.Remove(cid)

	// Invalid spec
	_, err = AddLogCleanCron("test_log_clean_invalid", "invalid-spec")
	if err == nil {
		t.Fatalf("expected error on invalid spec, got nil")
	}
}

func TestExecuteLogCleanTask(t *testing.T) {
	ft := model.FilmCollectTask{
		Id:     "test_sys_log_clean",
		Model:  4,
		State:  true,
		Remark: "自动清理过期运行日志",
	}
	// Should execute without panic
	executeLogCleanTask(ft)

	// Test with empty remark fallback
	ft2 := model.FilmCollectTask{
		Id:     "test_sys_log_clean_empty_remark",
		Model:  4,
		State:  true,
		Remark: "",
	}
	executeLogCleanTask(ft2)
}
