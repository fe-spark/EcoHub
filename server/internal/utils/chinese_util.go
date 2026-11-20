package utils

import (
	"github.com/longbridgeapp/opencc"
)

var (
	t2s, _ = opencc.New("t2s")
	s2t, _ = opencc.New("s2t")
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
	if s == "" || t2s == nil || !containsHan(s) || !containsTraditionalRune(s) {
		return s
	}
	out, err := t2s.Convert(s)
	if err != nil || out == s {
		return s
	}
	return out
}

// SimplifiedToTraditional 将简体字符串转换为繁体。
func SimplifiedToTraditional(s string) string {
	if s == "" || s2t == nil || !containsHan(s) {
		return s
	}
	out, err := s2t.Convert(s)
	if err != nil || out == "" || out == s {
		return s
	}
	return out
}
