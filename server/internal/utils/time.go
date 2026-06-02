package utils

import (
	"fmt"
	"strings"
	"time"
)

var collectTimeLayouts = []string{
	time.DateTime,
	"2006-01-02 15:04",
	time.DateOnly,
	time.RFC3339,
	"2006/01/02 15:04:05",
	"2006/01/02 15:04",
	"2006/01/02",
}

// ParseCollectUpdateTime 解析采集站返回的 vod_time。
func ParseCollectUpdateTime(raw string) (int64, error) {
	value := strings.TrimSpace(raw)
	if value == "" {
		return 0, fmt.Errorf("更新时间为空")
	}
	for _, layout := range collectTimeLayouts {
		parsed, err := time.ParseInLocation(layout, value, time.Local)
		if err == nil {
			return parsed.Unix(), nil
		}
	}
	return 0, fmt.Errorf("不支持的时间格式")
}
