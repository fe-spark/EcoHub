package syslog

import (
	"bufio"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
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
)

var defaultLogger = newRollingLogger()

type rollingLogger struct {
	mu       sync.Mutex
	file     *os.File
	fileSize int64
	nextSeq  int64
	entries  []Entry
}

type Entry struct {
	Seq  int64  `json:"seq"`
	Line string `json:"line"`
}

type DeltaResult struct {
	Entries []Entry
	NextSeq int64
	MinSeq  int64
	Expired bool
}

func newRollingLogger() *rollingLogger {
	return &rollingLogger{}
}

func Init() error {
	return defaultLogger.open()
}

func Writer() io.Writer {
	return defaultLogger
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

func (l *rollingLogger) Write(p []byte) (int, error) {
	l.mu.Lock()
	defer l.mu.Unlock()

	if l.file == nil {
		if err := l.openLocked(); err != nil {
			return 0, err
		}
	}
	if l.fileSize+int64(len(p)) > maxLogFileSize {
		if err := l.rotateLocked(); err != nil {
			return 0, err
		}
	}
	n, err := l.file.Write(p)
	l.fileSize += int64(n)
	if n > 0 {
		l.appendEntriesLocked(splitLogLines(string(p[:n])))
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

func (l *rollingLogger) appendEntriesLocked(lines []string) {
	for _, line := range lines {
		l.nextSeq++
		entry := Entry{Seq: l.nextSeq, Line: line}
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
			l.appendEntriesLocked(fileLines)
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
