package danmaku

import (
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"
)

const (
	maxDanmakuResponseBytes = 4 << 20 // 4MB
	maxDanmakuPostPerMin    = 20
)

var (
	danmakuSniffSF      singleflight.Group
	danmakuHexColorRe   = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	danmakuPostLimitMu  sync.Mutex
	danmakuPostLimitMap = map[string][]time.Time{}
)

func readLimitedDanmakuBody(r io.Reader) ([]byte, error) {
	data, err := io.ReadAll(io.LimitReader(r, maxDanmakuResponseBytes+1))
	if err != nil {
		return nil, err
	}
	if len(data) > maxDanmakuResponseBytes {
		return nil, fmt.Errorf("弹幕响应超过 %d 字节上限", maxDanmakuResponseBytes)
	}
	return data, nil
}

func validateDanmakuPostInput(mid int64, t float64, color string, mode int) (string, string) {
	if mid <= 0 {
		return "", "影片ID无效"
	}
	if math.IsNaN(t) || math.IsInf(t, 0) || t < 0 || t > 86400 {
		return "", "弹幕时间轴无效"
	}
	if mode < 0 || mode > 2 {
		return "", "弹幕位置模式无效"
	}
	color = strings.TrimSpace(color)
	if color == "" {
		color = "#ffffff"
	} else if !danmakuHexColorRe.MatchString(color) {
		return "", "弹幕颜色格式无效"
	}
	return color, ""
}

func allowDanmakuPost(ip string) bool {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		ip = "unknown"
	}
	now := time.Now()
	cutoff := now.Add(-time.Minute)

	danmakuPostLimitMu.Lock()
	defer danmakuPostLimitMu.Unlock()

	if len(danmakuPostLimitMap) > 4096 {
		for k, times := range danmakuPostLimitMap {
			if len(times) == 0 || times[len(times)-1].Before(cutoff) {
				delete(danmakuPostLimitMap, k)
			}
		}
	}

	hist := danmakuPostLimitMap[ip]
	kept := hist[:0]
	for _, ts := range hist {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= maxDanmakuPostPerMin {
		danmakuPostLimitMap[ip] = kept
		return false
	}
	danmakuPostLimitMap[ip] = append(kept, now)
	return true
}
