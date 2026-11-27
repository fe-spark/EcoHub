package notify

import (
	"strings"
	"testing"
)

func TestFormatFilmItemsPageHTML_EmptyItemsWithPositiveTotal(t *testing.T) {
	text, err := formatFilmItemsPageHTML("EcoHub", "全部更新", 1, 1, 0, 10, nil)
	if err != nil {
		t.Fatalf("formatFilmItemsPageHTML failed: %v", err)
	}
	if !strings.Contains(text, "暂无更新影片") {
		t.Fatalf("expected empty notice when items is empty despite total=10, got %q", text)
	}
	if strings.Contains(text, "1–0") {
		t.Fatalf("must not render inverted range 1–0, got %q", text)
	}
}
