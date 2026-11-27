package danmaku

import (
	"fmt"
	"io"
	"math"
	"regexp"
	"strings"
	"sync"
	"time"

	"server/internal/utils"

	"golang.org/x/sync/singleflight"
)

const (
	maxDanmakuResponseBytes = 4 << 20 // 4MB
	maxDanmakuPostPerMin    = 20
	maxDanmakuSniffPerMin   = 30
	maxDanmakuSniffInFlight = 8
	maxDanmakuRateLimitKeys = 4096
)

var (
	danmakuSniffSF    singleflight.Group
	danmakuHexColorRe = regexp.MustCompile(`^#[0-9a-fA-F]{6}$`)
	danmakuPostLimit  = newIPWindowLimiter(maxDanmakuPostPerMin, time.Minute)
	danmakuSniffLimit = newIPWindowLimiter(maxDanmakuSniffPerMin, time.Minute)
	danmakuSniffSem   = make(chan struct{}, maxDanmakuSniffInFlight)
)

type ipWindowLimiter struct {
	mu     sync.Mutex
	limit  int
	window time.Duration
	hits   map[string][]time.Time
}

func newIPWindowLimiter(limit int, window time.Duration) *ipWindowLimiter {
	return &ipWindowLimiter{
		limit:  limit,
		window: window,
		hits:   map[string][]time.Time{},
	}
}

func (l *ipWindowLimiter) allow(ip string) bool {
	key := utils.NormalizeClientIPKey(ip)
	now := time.Now()
	cutoff := now.Add(-l.window)

	l.mu.Lock()
	defer l.mu.Unlock()

	if len(l.hits) >= maxDanmakuRateLimitKeys {
		for k, times := range l.hits {
			if len(times) == 0 || times[len(times)-1].Before(cutoff) {
				delete(l.hits, k)
			}
		}
		// 活跃 IP 仍超硬上限时批量淘汰，防止高并发洪水下 map 无界增长。
		if len(l.hits) >= maxDanmakuRateLimitKeys {
			drop := len(l.hits) - maxDanmakuRateLimitKeys/2
			for k := range l.hits {
				delete(l.hits, k)
				drop--
				if drop <= 0 {
					break
				}
			}
		}
	}

	hist := l.hits[key]
	kept := hist[:0]
	for _, ts := range hist {
		if ts.After(cutoff) {
			kept = append(kept, ts)
		}
	}
	if len(kept) >= l.limit {
		l.hits[key] = kept
		return false
	}
	l.hits[key] = append(kept, now)
	return true
}

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
	return danmakuPostLimit.allow(ip)
}

func allowDanmakuSniff(ip string) bool {
	return danmakuSniffLimit.allow(ip)
}
