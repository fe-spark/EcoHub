package service

import (
	"encoding/json"
	"testing"

	"server/internal/model"
)

// 批次明细里超过 500 页的失败页全部取出，重试失败页不截断。
func TestParseRetryDetail_KeepsAllFailedPages(t *testing.T) {
	pages := make([]int, 6000)
	for i := range pages {
		pages[i] = 4501 + i
	}
	raw, err := json.Marshal([]model.CollectRecordSourceDetail{
		{SourceID: "jy", Status: "partial", Hour: -1, FailedCnt: len(pages), FailedPages: pages},
		{SourceID: "ok", Status: "success", FailedPages: []int{1}},
	})
	if err != nil {
		t.Fatal(err)
	}
	failed, hours, byPage, err := parseRetryDetail(string(raw))
	if err != nil {
		t.Fatal(err)
	}
	if !failed["jy"] || failed["ok"] || hours["jy"] != -1 {
		t.Fatalf("failed=%v hours=%v", failed, hours)
	}
	if got := byPage["jy"]; len(got) != 6000 || got[0] != 4501 || got[5999] != 10500 {
		t.Fatalf("failed pages = %d (first/last %v), want all 6000", len(got), got[:1])
	}
	if _, ok := byPage["ok"]; ok {
		t.Fatal("successful source must not be retried")
	}
}
