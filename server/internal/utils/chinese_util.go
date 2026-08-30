package utils

import (
	"github.com/longbridgeapp/opencc"
)

var (
	t2s, _ = opencc.New("t2s")
)

func containsHan(s string) bool {
	for _, r := range s {
		if r >= 0x4E00 && r <= 0x9FFF {
			return true
		}
	}
	return false
}

// TraditionalToSimplified 将繁体字符串转换为简体
func TraditionalToSimplified(s string) string {
	if s == "" || t2s == nil || !containsHan(s) {
		return s
	}
	out, err := t2s.Convert(s)
	if err != nil {
		return s
	}
	return out
}
