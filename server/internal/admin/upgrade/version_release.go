package upgrade

import (
	"encoding/json"
	"log"
	"sync"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
)

var (
	releaseMu         sync.Mutex
	releaseStable     githubReleaseCache
	releasePre        githubReleaseCache
	releasePollerOnce sync.Once
	releasePollWakeCh = make(chan struct{}, 1)
	fetchReleaseFn    = fetchGithubLatestRelease
)

func releaseRedisKey(includePre bool) string {
	if includePre {
		return config.LatestReleasePreCacheKey
	}
	return config.LatestReleaseCacheKey
}

func currentRelease(includePre bool) (githubReleaseCache, bool) {
	releaseMu.Lock()
	rel := releaseStable
	if includePre {
		rel = releasePre
	}
	releaseMu.Unlock()
	if rel.TagName != "" {
		return rel, true
	}
	// 内存未命中时尝试从 Redis 回暖（如服务刚重启后 GitHub 尚未轮询成功）
	if db.Rdb != nil {
		if raw, err := db.Rdb.Get(db.Cxt, releaseRedisKey(includePre)).Result(); err == nil && raw != "" {
			var cached githubReleaseCache
			if json.Unmarshal([]byte(raw), &cached) == nil && cached.TagName != "" {
				storeRelease(includePre, cached)
				return cached, true
			}
		}
	}
	return githubReleaseCache{}, false
}

func storeRelease(includePre bool, rel githubReleaseCache) {
	releaseMu.Lock()
	if includePre {
		releasePre = rel
	} else {
		releaseStable = rel
	}
	releaseMu.Unlock()
	if db.Rdb != nil && rel.TagName != "" {
		if data, err := json.Marshal(rel); err == nil {
			_ = db.Rdb.Set(db.Cxt, releaseRedisKey(includePre), string(data), config.LatestReleaseCacheTTL).Err()
		}
	}
}

func pollRelease(includePre bool) bool {
	rel, err := fetchReleaseFn(includePre)
	if err != nil {
		// 单次拉取失败保留已有有效缓存，避免覆盖清零导致长达 1 小时不可用
		log.Printf("[Version] 拉取 GitHub Release 失败 pre=%v: %v", includePre, err)
		return false
	}
	storeRelease(includePre, rel)
	return true
}

// TriggerReleasePoll 通知版本轮询协程立即刷新一次（如修改升级代理配置后）。
func TriggerReleasePoll() {
	select {
	case releasePollWakeCh <- struct{}{}:
	default:
	}
}

// StartReleasePoller 进程内轮询 GitHub Release。请求只读内存（启动时从 Redis 回暖）。
func StartReleasePoller() {
	releasePollerOnce.Do(func() {
		go func() {
			failStreak := 0
			for {
				okStable := pollRelease(false)
				okPre := pollRelease(true)
				wait := config.LatestReleaseCacheTTL
				// 仅在尚未缓存任何有效版本时采用短退避重试，避免首次未就绪空等 1 小时
				if !okStable && !okPre {
					if _, hasStable := currentRelease(false); !hasStable {
						failStreak++
						backoff := time.Duration(30*(1<<min(failStreak-1, 4))) * time.Second
						if backoff < wait {
							wait = backoff
						}
					}
				} else {
					failStreak = 0
				}
				timer := time.NewTimer(wait)
				select {
				case <-timer.C:
				case <-releasePollWakeCh:
					timer.Stop()
				}
			}
		}()
	})
}
