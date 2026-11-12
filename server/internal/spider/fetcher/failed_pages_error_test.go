package fetcher

import (
	"strings"
	"testing"
)

func TestFormatFailedPagesErrorKeepsReason(t *testing.T) {
	pages := []int{3131, 3132, 3133, 3134, 3135, 3136, 3137, 3138, 3139, 3141, 3144}
	reason := `Get "https://jinyingzy.com/api.php/provide/vod?ac=detail&pg=3144": context deadline exceeded, url=https://jinyingzy.com/api.php/provide/vod?ac=detail&pg=3144`
	got := formatFailedPagesError(pages, len(pages), reason)
	if !strings.Contains(got, "共 11 页失败") || !strings.Contains(got, "...") {
		t.Fatalf("page list should stay a short sample: %s", got)
	}
	if !strings.Contains(got, reason) {
		t.Fatalf("reason was truncated: %s", got)
	}

	long := strings.Repeat("错", failedPageErrorRunes+10)
	trimmed := formatFailedPagesError([]int{3}, 1, long)
	if !strings.HasSuffix(trimmed, "...") || strings.Count(trimmed, "错") != failedPageErrorRunes {
		t.Fatalf("long reason should keep %d runes, got %d", failedPageErrorRunes, strings.Count(trimmed, "错"))
	}
}
