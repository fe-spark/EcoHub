package handler

import (
	"net/http"
	"testing"

	"server/internal/model/dto"
)

func TestSystemLogHandler_Delta(t *testing.T) {
	// 1. Initial fetch (lines=10)
	c, w := testContext(http.MethodGet, "/api/manage/system/logs/delta?lines=10")
	SystemLogHd.Delta(c)
	if w.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w.Code)
	}
	resp := decodeResponse(t, w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected code SUCCESS, got %d", resp.Code)
	}
	data, ok := resp.Data.(map[string]any)
	if !ok {
		t.Fatalf("expected map data, got %T", resp.Data)
	}
	if _, ok := data["entries"]; !ok {
		t.Errorf("missing entries in response data")
	}
	if _, ok := data["nextSeq"]; !ok {
		t.Errorf("missing nextSeq in response data")
	}

	// 2. Incremental fetch with after
	c2, w2 := testContext(http.MethodGet, "/api/manage/system/logs/delta?after=1&limit=50")
	SystemLogHd.Delta(c2)
	if w2.Code != http.StatusOK {
		t.Fatalf("expected 200, got %d", w2.Code)
	}
	resp2 := decodeResponse(t, w2)
	if resp2.Code != dto.SUCCESS {
		t.Fatalf("expected code SUCCESS, got %d", resp2.Code)
	}
}
