// Package syslog 提供带级别的滚动日志：级别在写入时确定，随 Entry 下发前端。
//
// 约定：
//   - log.SetOutput(syslog.Writer()) / log.Printf → INFO（调用即定级）
//   - gin.DefaultErrorWriter = LevelWriter(LevelError) → ERROR
//   - 需要 WARN/ERROR 时请用 syslog.Warnf / Errorf，勿依赖正文关键词猜测
//
// 落盘行格式：时间戳后写入结构化标签 [INFO]|[WARN]|[ERROR]，供进程重启后恢复 level。
// stdout 镜像保持原始内容（不带标签），与旧版控制台输出一致。
package syslog

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"regexp"
	"strings"
	"sync"
	"time"
)

const (
	logDir            = "logs"
	logFileName       = "ecohub.log"
	maxLogFileSize    = 10 * 1024 * 1024
	maxLogRetention   = 7 * 24 * time.Hour
	maxRecentLines    = 2000
	readChunkSize     = 32 * 1024
	entryBufferSize   = 10000
	rotatedTimeFormat = "20060102-150405.000000000"

	// 日志级别：打印时确定，随 Entry 下发前端，禁止前端按正文猜。
	LevelInfo  = "info"
	LevelWarn  = "warn"
	LevelError = "error"
)

// 仅识别写入时打上的结构化级别标签（时间戳后），用于从文件恢复缓冲。
// 不扫描正文关键词。
var structuredLevelPrefix = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?) \[(INFO|WARN|ERROR)\] `)

// 标准 log 时间前缀（可含微秒），用于给无标签行注入 [LEVEL]。
var stdLogTimePrefix = regexp.MustCompile(`^(\d{4}/\d{2}/\d{2} \d{2}:\d{2}:\d{2}(?:\.\d+)?) `)

var defaultLogger = newRollingLogger()

type rollingLogger struct {
	mu       sync.Mutex
	file     *os.File
	fileSize int64
	nextSeq  int64
	entries  []Entry
	// mirror 额外镜像输出（通常 os.Stdout）；由 Init/SetMirror 配置。
	mirror io.Writer
}

// Entry 单条日志；Level 在写入时确定。
type Entry struct {
	Seq   int64  `json:"seq"`
	Level string `json:"level"`
	Line  string `json:"line"`
}

type DeltaResult struct {
	Entries []Entry
	NextSeq int64
	MinSeq  int64
	Expired bool
}

// levelWriter 以固定级别写入（给 log.SetOutput / gin 等 io.Writer 用）。
type levelWriter struct {
	level string
}

func newRollingLogger() *rollingLogger {
	return &rollingLogger{mirror: os.Stdout}
}

func Init() error {
	return defaultLogger.open()
}

// SetMirror 设置文件以外的镜像输出（默认 os.Stdout）；传 nil 关闭镜像。
func SetMirror(w io.Writer) {
	defaultLogger.mu.Lock()
	defer defaultLogger.mu.Unlock()
	defaultLogger.mirror = w
}

// Writer 默认 INFO 级别的 io.Writer（兼容 log.SetOutput / gin.DefaultWriter）。
func Writer() io.Writer {
	return LevelWriter(LevelInfo)
}

// LevelWriter 返回以指定级别写入的 io.Writer。
// 级别在 Write 调用时即确定；会为标准 log 行注入 [LEVEL] 标签便于落盘恢复。
func LevelWriter(level string) io.Writer {
	return levelWriter{level: normalizeLevel(level)}
}

func (w levelWriter) Write(p []byte) (int, error) {
	stamped := stampLevelPayload(w.level, p)
	// 落盘用带 [LEVEL] 标签的版本（便于重启后恢复级别）；镜像输出原始内容
	if _, err := defaultLogger.writeWithLevel(w.level, stamped, p); err != nil {
		return 0, err
	}
	// 必须返回原始长度，满足 io.Writer / log 包契约（内容可能因注入标签变长）
	return len(p), nil
}

// Infof / Warnf / Errorf 打印时显式带级别，并写入结构化 [LEVEL] 标签便于落盘恢复。
func Infof(format string, v ...any)  { emit(LevelInfo, format, v...) }
func Warnf(format string, v ...any)  { emit(LevelWarn, format, v...) }
func Errorf(format string, v ...any) { emit(LevelError, format, v...) }

func Info(v ...any)  { emit(LevelInfo, "%s", fmt.Sprint(v...)) }
func Warn(v ...any)  { emit(LevelWarn, "%s", fmt.Sprint(v...)) }
func Error(v ...any) { emit(LevelError, "%s", fmt.Sprint(v...)) }

func emit(level, format string, v ...any) {
	msg := strings.TrimRight(fmt.Sprintf(format, v...), "\n")
	ts := time.Now().Format("2006/01/02 15:04:05")
	// 落盘带 [LEVEL] 标签，便于重启后恢复级别；镜像输出同内容但无标签
	line := fmt.Sprintf("%s [%s] %s\n", ts, strings.ToUpper(normalizeLevel(level)), msg)
	console := fmt.Sprintf("%s %s\n", ts, msg)
	_, _ = defaultLogger.writeWithLevel(level, []byte(line), []byte(console))
}

func RecentLines(lines int) ([]string, error) {
	if lines <= 0 {
		lines = 500
	}
	if lines > maxRecentLines {
		lines = maxRecentLines
	}
	return readLastLines(activeLogPath(), lines)
}

func RecentEntries(lines int) ([]Entry, int64, error) {
	return defaultLogger.recentEntries(lines)
}

func DeltaAfter(after int64, limit int) DeltaResult {
	return defaultLogger.deltaAfter(after, limit)
}

func (l *rollingLogger) open() error {
	l.mu.Lock()
	defer l.mu.Unlock()

	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}
	if err := pruneExpiredLogsLocked(time.Now()); err != nil {
		return err
	}
	file, err := os.OpenFile(activeLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	if l.file != nil {
		_ = l.file.Close()
	}
	l.file = file
	l.fileSize = info.Size()
	return nil
}

// writeWithLevel 级别由调用方在写入时传入，绝不根据正文猜。
// filePayload 落盘（含 [LEVEL] 标签），mirrorPayload 镜像输出（原始内容）；
// mirror（stdout）在锁外写，避免管道阻塞卡住全部日志。
func (l *rollingLogger) writeWithLevel(level string, filePayload, mirrorPayload []byte) (int, error) {
	level = normalizeLevel(level)

	l.mu.Lock()
	mirror := l.mirror

	if l.file == nil {
		if err := l.openLocked(); err != nil {
			l.mu.Unlock()
			// 文件不可用时仍尽量镜像，便于排障
			if mirror != nil && len(mirrorPayload) > 0 {
				_, _ = mirror.Write(mirrorPayload)
			}
			return 0, err
		}
	}
	if l.fileSize+int64(len(filePayload)) > maxLogFileSize {
		if err := l.rotateLocked(); err != nil {
			l.mu.Unlock()
			if mirror != nil && len(mirrorPayload) > 0 {
				_, _ = mirror.Write(mirrorPayload)
			}
			return 0, err
		}
	}
	n, err := l.file.Write(filePayload)
	l.fileSize += int64(n)
	if n > 0 {
		l.appendEntriesLocked(level, splitLogLines(string(filePayload[:n])))
	}
	l.mu.Unlock()

	if mirror != nil && len(mirrorPayload) > 0 {
		_, _ = mirror.Write(mirrorPayload)
	}
	return n, err
}

func (l *rollingLogger) openLocked() error {
	if err := os.MkdirAll(logDir, 0755); err != nil {
		return err
	}
	if err := pruneExpiredLogsLocked(time.Now()); err != nil {
		return err
	}
	file, err := os.OpenFile(activeLogPath(), os.O_CREATE|os.O_APPEND|os.O_WRONLY, 0644)
	if err != nil {
		return err
	}
	info, err := file.Stat()
	if err != nil {
		_ = file.Close()
		return err
	}
	l.file = file
	l.fileSize = info.Size()
	return nil
}

func (l *rollingLogger) rotateLocked() error {
	if l.file != nil {
		_ = l.file.Close()
		l.file = nil
	}
	if _, err := os.Stat(activeLogPath()); err == nil {
		if err := os.Rename(activeLogPath(), rotatedLogPath(time.Now())); err != nil {
			return err
		}
	}
	if err := pruneExpiredLogsLocked(time.Now()); err != nil {
		return err
	}
	return l.openLocked()
}

func pruneExpiredLogsLocked(now time.Time) error {
	entries, err := os.ReadDir(logDir)
	if errors.Is(err, os.ErrNotExist) {
		return nil
	}
	if err != nil {
		return err
	}
	deadline := now.Add(-maxLogRetention)
	for _, entry := range entries {
		if entry.IsDir() || !isRotatedLogFile(entry.Name()) {
			continue
		}
		path := filepath.Join(logDir, entry.Name())
		info, err := entry.Info()
		if err != nil {
			return err
		}
		if info.ModTime().Before(deadline) {
			if err := os.Remove(path); err != nil && !errors.Is(err, os.ErrNotExist) {
				return err
			}
		}
	}
	return nil
}

// appendEntriesLocked 使用写入时给定的 level；行内已有结构化 [LEVEL] 时以标签为准。
func (l *rollingLogger) appendEntriesLocked(defaultLevel string, lines []string) {
	defaultLevel = normalizeLevel(defaultLevel)
	for _, line := range lines {
		level := defaultLevel
		if lv, ok := levelFromStructuredLine(line); ok {
			level = lv
		}
		l.nextSeq++
		entry := Entry{Seq: l.nextSeq, Level: level, Line: line}
		l.entries = append(l.entries, entry)
		if len(l.entries) > entryBufferSize {
			l.entries = l.entries[len(l.entries)-entryBufferSize:]
		}
	}
}

func (l *rollingLogger) recentEntries(lines int) ([]Entry, int64, error) {
	if lines <= 0 {
		lines = 500
	}
	if lines > maxRecentLines {
		lines = maxRecentLines
	}
	l.mu.Lock()
	if len(l.entries) == 0 {
		l.mu.Unlock()
		fileLines, err := RecentLines(lines)
		if err != nil {
			return nil, 0, err
		}
		l.mu.Lock()
		if len(l.entries) == 0 {
			// 从文件恢复：仅识别结构化 [LEVEL] 标签，其余默认 info
			l.appendEntriesLocked(LevelInfo, fileLines)
		}
		defer l.mu.Unlock()
	} else {
		defer l.mu.Unlock()
	}
	if len(l.entries) == 0 {
		return []Entry{}, l.nextSeq, nil
	}
	start := len(l.entries) - lines
	if start < 0 {
		start = 0
	}
	entries := append([]Entry(nil), l.entries[start:]...)
	return entries, l.nextSeq, nil
}

func (l *rollingLogger) deltaAfter(after int64, limit int) DeltaResult {
	if limit <= 0 {
		limit = entryBufferSize
	}
	if limit > entryBufferSize {
		limit = entryBufferSize
	}
	l.mu.Lock()
	defer l.mu.Unlock()
	if len(l.entries) == 0 {
		return DeltaResult{Entries: []Entry{}, NextSeq: l.nextSeq, MinSeq: l.nextSeq + 1}
	}
	minSeq := l.entries[0].Seq
	if after > 0 && after < minSeq-1 {
		start := len(l.entries) - limit
		if start < 0 {
			start = 0
		}
		entries := append([]Entry(nil), l.entries[start:]...)
		return DeltaResult{Entries: entries, NextSeq: l.nextSeq, MinSeq: minSeq, Expired: true}
	}
	start := len(l.entries)
	for i, entry := range l.entries {
		if entry.Seq > after {
			start = i
			break
		}
	}
	end := start + limit
	if end > len(l.entries) {
		end = len(l.entries)
	}
	entries := append([]Entry(nil), l.entries[start:end]...)
	return DeltaResult{Entries: entries, NextSeq: l.nextSeq, MinSeq: minSeq}
}

func splitLogLines(raw string) []string {
	parts := strings.Split(raw, "\n")
	lines := make([]string, 0, len(parts))
	for _, part := range parts {
		part = strings.TrimRight(part, "\r")
		if part != "" {
			lines = append(lines, part)
		}
	}
	return lines
}

func activeLogPath() string {
	return filepath.Join(logDir, logFileName)
}

func rotatedLogPath(now time.Time) string {
	return filepath.Join(logDir, fmt.Sprintf("%s.%s", logFileName, now.Format(rotatedTimeFormat)))
}

func isRotatedLogFile(name string) bool {
	return strings.HasPrefix(name, logFileName+".")
}

func readLastLines(path string, limit int) ([]string, error) {
	file, err := os.Open(path)
	if errors.Is(err, os.ErrNotExist) {
		return []string{}, nil
	}
	if err != nil {
		return nil, err
	}
	defer file.Close()

	info, err := file.Stat()
	if err != nil {
		return nil, err
	}
	if info.Size() == 0 {
		return []string{}, nil
	}

	var data []byte
	buffer := make([]byte, readChunkSize)
	for offset := info.Size(); offset > 0 && countLines(data) <= limit; {
		readSize := int64(readChunkSize)
		if offset < readSize {
			readSize = offset
		}
		offset -= readSize
		if _, err := file.ReadAt(buffer[:readSize], offset); err != nil && !errors.Is(err, io.EOF) {
			return nil, err
		}
		data = append(append([]byte(nil), buffer[:readSize]...), data...)
	}

	return lastNonEmptyLines(data, limit), nil
}

func countLines(data []byte) int {
	count := 0
	for _, b := range data {
		if b == '\n' {
			count++
		}
	}
	return count
}

func lastNonEmptyLines(data []byte, limit int) []string {
	scanner := bufio.NewScanner(strings.NewReader(string(data)))
	scanner.Buffer(make([]byte, 0, 64*1024), 1024*1024)
	lines := make([]string, 0, limit)
	for scanner.Scan() {
		line := strings.TrimRight(scanner.Text(), "\r")
		if line == "" {
			continue
		}
		lines = append(lines, line)
		if len(lines) > limit {
			lines = lines[len(lines)-limit:]
		}
	}
	return lines
}

func normalizeLevel(level string) string {
	switch strings.ToLower(strings.TrimSpace(level)) {
	case LevelWarn, "warning":
		return LevelWarn
	case LevelError, "err", "fatal", "panic":
		return LevelError
	default:
		return LevelInfo
	}
}

// levelFromStructuredLine 只认「时间戳 [LEVEL] 」前缀（写入时打上），不扫正文。
func levelFromStructuredLine(line string) (string, bool) {
	m := structuredLevelPrefix.FindStringSubmatch(line)
	if len(m) != 3 {
		return "", false
	}
	return normalizeLevel(m[2]), true
}

// stampLevelPayload 为 payload 中每一行注入结构化级别标签（已有则跳过）。
// 保留原始是否以 \n 结尾的形态。
func stampLevelPayload(level string, p []byte) []byte {
	if len(p) == 0 {
		return p
	}
	level = normalizeLevel(level)
	endsWithNL := p[len(p)-1] == '\n'
	// 按行处理；最后一段若无换行也是一行
	raw := string(p)
	if endsWithNL {
		raw = raw[:len(raw)-1]
	}
	if raw == "" {
		return p
	}
	parts := strings.Split(raw, "\n")
	var b bytes.Buffer
	for i, part := range parts {
		part = strings.TrimRight(part, "\r")
		if part != "" {
			b.WriteString(stampLevelOnLine(level, part))
		}
		if i < len(parts)-1 {
			b.WriteByte('\n')
		}
	}
	if endsWithNL {
		b.WriteByte('\n')
	}
	return b.Bytes()
}

// stampLevelOnLine 在标准时间戳后插入 [LEVEL]；已有标签保持原样。
// 非标准时间前缀的行（gin 访问日志、多行消息的续行等）保持原样，不注入合成时间戳
// 以免篡改正文；这类行从文件恢复时按写入级别默认（通常为 info）。
func stampLevelOnLine(level, line string) string {
	if line == "" {
		return line
	}
	if _, ok := levelFromStructuredLine(line); ok {
		return line
	}
	tag := "[" + strings.ToUpper(normalizeLevel(level)) + "]"
	if loc := stdLogTimePrefix.FindStringSubmatchIndex(line); loc != nil {
		// line = <time> + " " + rest  →  <time> + " [LEVEL] " + rest
		// loc[0]:loc[1] 全匹配；loc[2]:loc[3] 为 time 捕获组
		timeEnd := loc[3]
		rest := line[loc[1]:]
		return line[:timeEnd] + " " + tag + " " + rest
	}
	return line
}
