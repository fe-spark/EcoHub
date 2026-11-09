package service

import (
	"strings"
	"testing"
	"unicode/utf8"
)

func TestTruncateReasonLines(t *testing.T) {
	long := strings.Repeat("清", 300)
	got := truncateReasonLines("清理空记录失败: "+long+"\n\n清理无线路幽灵影片失败: x", 200, 600)
	lines := strings.Split(got, "\n")
	if len(lines) != 2 {
		t.Fatalf("lines = %d: %q", len(lines), got)
	}
	if utf8.RuneCountInString(lines[0]) != 201 || !strings.HasSuffix(lines[0], "…") {
		t.Fatalf("first line not truncated per line: %d", utf8.RuneCountInString(lines[0]))
	}
	if lines[1] != "清理无线路幽灵影片失败: x" {
		t.Fatalf("second step lost: %q", lines[1])
	}
	if !utf8.ValidString(got) {
		t.Fatal("invalid utf8")
	}
	total := truncateReasonLines(strings.Repeat(long+"\n", 10), 200, 600)
	if utf8.RuneCountInString(total) != 601 {
		t.Fatalf("total = %d", utf8.RuneCountInString(total))
	}
}
