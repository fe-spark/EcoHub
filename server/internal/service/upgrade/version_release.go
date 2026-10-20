package upgrade

import (
	"log"
	"sync"
	"time"

	"server/internal/config"
)

var (
	releaseMu         sync.Mutex
	releaseStable     githubReleaseCache
	releasePre        githubReleaseCache
	releasePollerOnce sync.Once
)

func currentRelease(includePre bool) (githubReleaseCache, bool) {
	releaseMu.Lock()
	defer releaseMu.Unlock()
	rel := releaseStable
	if includePre {
		rel = releasePre
	}
	if rel.TagName == "" {
		return githubReleaseCache{}, false
	}
	return rel, true
}

func storeRelease(includePre bool, rel githubReleaseCache) {
	releaseMu.Lock()
	defer releaseMu.Unlock()
	if includePre {
		releasePre = rel
		return
	}
	releaseStable = rel
}

func pollRelease(includePre bool) {
	rel, err := fetchGithubLatestRelease(includePre)
	if err != nil {
		log.Printf("[Version] 拉取 GitHub Release 失败 pre=%v: %v", includePre, err)
		storeRelease(includePre, githubReleaseCache{})
		return
	}
	storeRelease(includePre, rel)
}

// StartReleasePoller 进程内轮询 GitHub Release。请求只读内存。
func StartReleasePoller() {
	releasePollerOnce.Do(func() {
		go func() {
			for {
				pollRelease(false)
				pollRelease(true)
				time.Sleep(config.LatestReleaseCacheTTL)
			}
		}()
	})
}
