package syslog

import (
	"bufio"
	"bytes"
	"errors"
	"fmt"
	"io"
	"os"
	"path/filepath"
	"strings"
	"time"
)

func activeLogPath() string {
	return filepath.Join(logDir, logFileName)
}

func rotatedLogPath(now time.Time) string {
	return filepath.Join(logDir, fmt.Sprintf("%s.%s", logFileName, now.Format(rotatedTimeFormat)))
}

func isRotatedLogFile(name string) bool {
	return strings.HasPrefix(name, logFileName+".") && len(name) > len(logFileName)+1
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
