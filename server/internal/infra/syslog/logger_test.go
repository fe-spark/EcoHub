package syslog

import (
	"bytes"
	"errors"
	"os"
	"path/filepath"
	"strings"
	"testing"
	"time"
)

func TestNormalizeLevel(t *testing.T) {
	cases := map[string]string{
		"":        LevelInfo,
		"INFO":    LevelInfo,
		"warn":    LevelWarn,
		"WARNING": LevelWarn,
		"error":   LevelError,
		"ERR":     LevelError,
	}
	for in, want := range cases {
		if got := normalizeLevel(in); got != want {
			t.Fatalf("normalizeLevel(%q)=%q want %q", in, got, want)
		}
	}
}

func TestLevelFromStructuredLine(t *testing.T) {
	line := "2026/08/08 13:31:15 [ERROR] boom"
	lv, ok := levelFromStructuredLine(line)
	if !ok || lv != LevelError {
		t.Fatalf("got %q ok=%v", lv, ok)
	}
	// 标准 log 无级别标签（仅有业务方括号）→ 不识别
	if _, ok := levelFromStructuredLine("2026/08/08 13:31:15 [Spider] 采集进度 失败=0"); ok {
		t.Fatal("must not treat body keywords or non-level brackets as level")
	}
	// 正文含 error/失败 也不认
	if _, ok := levelFromStructuredLine("2026/08/08 13:31:15 something failed 失败 error"); ok {
		t.Fatal("must not scan message body for level")
	}
	// 微秒时间戳
	if lv, ok := levelFromStructuredLine("2026/08/08 13:31:15.123456 [WARN] x"); !ok || lv != LevelWarn {
		t.Fatalf("microseconds prefix: lv=%q ok=%v", lv, ok)
	}
}

func TestStampLevelOnLine(t *testing.T) {
	got := stampLevelOnLine(LevelInfo, "2026/08/08 13:31:15 [Spider] 采集进度 失败=0")
	want := "2026/08/08 13:31:15 [INFO] [Spider] 采集进度 失败=0"
	if got != want {
		t.Fatalf("got %q want %q", got, want)
	}
	// 已有标签不重复注入
	already := "2026/08/08 13:31:15 [ERROR] boom"
	if stampLevelOnLine(LevelInfo, already) != already {
		t.Fatal("should not double-stamp")
	}
	// 从带标签行可恢复 level
	if lv, ok := levelFromStructuredLine(got); !ok || lv != LevelInfo {
		t.Fatalf("stamped line level=%q ok=%v", lv, ok)
	}
	// 非标准时间前缀（gin 访问日志、多行续行）：保持原样，不注入合成时间戳/标签
	ginLine := "[GIN] 2026/08/08 - 13:31:15 | 200 | GET /api/list"
	if out := stampLevelOnLine(LevelInfo, ginLine); out != ginLine {
		t.Fatalf("gin line must stay unchanged, got %q", out)
	}
	contLine := "    at foo.go:42 (stack trace continuation)"
	if out := stampLevelOnLine(LevelError, contLine); out != contLine {
		t.Fatalf("continuation line must stay unchanged, got %q", out)
	}
}

func TestStampLevelPayloadPreservesNewline(t *testing.T) {
	in := []byte("2026/08/08 13:31:15 hello\n")
	out := stampLevelPayload(LevelError, in)
	if !strings.HasSuffix(string(out), "\n") {
		t.Fatal("should keep trailing newline")
	}
	if !strings.Contains(string(out), "[ERROR]") {
		t.Fatalf("missing level tag: %s", out)
	}
	// 返回契约：LevelWriter 应返回原始 len，此处只测内容变长
	if len(out) <= len(in) {
		t.Fatal("stamped payload should be longer")
	}
}

func TestAppendEntriesUsesWriteTimeLevel(t *testing.T) {
	l := newRollingLogger()
	l.mirror = nil
	// 无标签：用写入时传入的 level
	l.appendEntriesLocked(LevelInfo, []string{"2026/08/08 13:31:15 [Spider] 采集进度 失败=0"})
	if l.entries[0].Level != LevelInfo {
		t.Fatalf("want info, got %s", l.entries[0].Level)
	}
	// 有结构化标签：以标签为准
	l.appendEntriesLocked(LevelInfo, []string{"2026/08/08 13:31:15 [WARN] hello"})
	if l.entries[1].Level != LevelWarn {
		t.Fatalf("want warn from tag, got %s", l.entries[1].Level)
	}
}

func TestLevelWriterKeepsLevel(t *testing.T) {
	w := LevelWriter(LevelError).(levelWriter)
	if w.level != LevelError {
		t.Fatalf("got %s", w.level)
	}
}

func TestWriteWithLevelMirrorOriginal(t *testing.T) {
	l := newRollingLogger()
	l.mirror = nil
	f, err := os.CreateTemp(t.TempDir(), "syslog-test-*.log")
	if err != nil {
		t.Fatal(err)
	}
	t.Cleanup(func() { _ = f.Close() })
	l.file = f
	l.fileSize = 0

	var mirror bytes.Buffer
	l.mirror = &mirror

	tagged := []byte("2026/08/08 13:31:15 [ERROR] boom\n")
	original := []byte("2026/08/08 13:31:15 boom\n")
	if _, err := l.writeWithLevel(LevelError, tagged, original); err != nil {
		t.Fatal(err)
	}

	// 落盘为带标签版本，供重启后恢复级别
	fileData, err := os.ReadFile(f.Name())
	if err != nil {
		t.Fatal(err)
	}
	if !strings.Contains(string(fileData), "[ERROR]") {
		t.Fatalf("file should contain tagged payload: %q", fileData)
	}
	// 镜像输出原始内容（不带标签），与旧版控制台输出一致
	if mirror.String() != string(original) {
		t.Fatalf("mirror got %q want %q", mirror.String(), original)
	}
	if strings.Contains(mirror.String(), "[ERROR]") {
		t.Fatal("mirror must not contain level tag")
	}
	// 缓冲区条目级别按写入时级别
	if len(l.entries) == 0 || l.entries[0].Level != LevelError {
		t.Fatalf("entry level want error, got %+v", l.entries)
	}
}

func TestIsRotatedLogFile(t *testing.T) {
	cases := []struct {
		name string
		want bool
	}{
		{"ecohub.log", false},
		{"ecohub.log.", false},
		{"ecohub.log.20260906-150405.000000000", true},
		{"ecohub.log.1", true},
		{"ecohub.1.log", false},
		{"other.log", false},
		{"", false},
	}
	for _, tc := range cases {
		if got := isRotatedLogFile(tc.name); got != tc.want {
			t.Errorf("isRotatedLogFile(%q) = %v, want %v", tc.name, got, tc.want)
		}
	}
}

func useTempLogDir(t *testing.T) string {
	t.Helper()
	dir := t.TempDir()
	prev := logDir
	logDir = dir
	t.Cleanup(func() { logDir = prev })
	return dir
}

func TestPruneExpiredLogs(t *testing.T) {
	dir := useTempLogDir(t)

	activeFile := filepath.Join(dir, logFileName)
	oldRotated := filepath.Join(dir, "ecohub.log.20260801-120000.000000000")
	recentRotated := filepath.Join(dir, "ecohub.log.20260906-120000.000000000")
	unrelated := filepath.Join(dir, "other.log")

	_ = os.WriteFile(activeFile, []byte("active-content"), 0644)
	_ = os.WriteFile(oldRotated, []byte("old-content"), 0644)
	_ = os.WriteFile(recentRotated, []byte("recent-content"), 0644)
	_ = os.WriteFile(unrelated, []byte("unrelated-content"), 0644)

	oldTime := time.Now().Add(-10 * 24 * time.Hour)
	_ = os.Chtimes(oldRotated, oldTime, oldTime)
	_ = os.Chtimes(activeFile, oldTime, oldTime)
	_ = os.Chtimes(unrelated, oldTime, oldTime)

	pruned, err := PruneExpiredLogs(7 * 24 * time.Hour)
	if err != nil {
		t.Fatalf("PruneExpiredLogs error: %v", err)
	}
	if pruned != 1 {
		t.Fatalf("expected 1 pruned file, got %d", pruned)
	}

	if _, err := os.Stat(oldRotated); !errors.Is(err, os.ErrNotExist) {
		t.Errorf("expected oldRotated to be deleted, err=%v", err)
	}
	if _, err := os.Stat(recentRotated); err != nil {
		t.Errorf("expected recentRotated to exist, err=%v", err)
	}
	if _, err := os.Stat(activeFile); err != nil {
		t.Errorf("expected activeFile to exist, err=%v", err)
	}
	if _, err := os.Stat(unrelated); err != nil {
		t.Errorf("expected unrelated to exist, err=%v", err)
	}

	prunedZero, err := PruneExpiredLogs(0)
	if err != nil || prunedZero != 0 {
		t.Fatalf("expected 0, nil for non-positive retention, got %d, %v", prunedZero, err)
	}
}

func TestPruneExpiredLogs_EmptyOrNoOp(t *testing.T) {
	dir := t.TempDir()
	prev := logDir
	logDir = filepath.Join(dir, "missing")
	t.Cleanup(func() { logDir = prev })

	pruned, err := PruneExpiredLogs(24 * time.Hour)
	if err != nil {
		t.Fatalf("unexpected error: %v", err)
	}
	if pruned != 0 {
		t.Fatalf("expected 0 pruned files in missing dir, got %d", pruned)
	}
}
