package utils

import (
	"github.com/longbridgeapp/opencc"
)

var (
	t2s, _ = opencc.New("t2s")
)

// TraditionalToSimplified 将繁体字符串转换为简体
func TraditionalToSimplified(s string) string {
	if s == "" {
		return ""
	}
	if t2s == nil {
		return s
	}
	out, err := t2s.Convert(s)
	if err != nil {
		return s
	}
	return out
}
