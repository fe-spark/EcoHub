package snapshot

import (
	"context"
	"log"
	"sync"
	"sync/atomic"
	"time"

	"server/internal/infra/db"

	"golang.org/x/sync/errgroup"
)

const defaultSearchRebuildDebounce = 500 * time.Millisecond

// SearchIndexProgress 检索索引构建的数字进度
type SearchIndexProgress struct {
	Total   int64  `json:"total"`
	Current int64  `json:"current"`
	Percent int    `json:"percent"`
	Status  string `json:"status"` // "indexing" | "done" | "idle"
}

var (
	searchRebuildDebounce         = 500 * time.Millisecond
	scanActiveFilmSearchIndex     = scanAndStoreActiveFilmSearchIndex
	searchIndexProgressListenerMu sync.RWMutex
	searchIndexProgressListeners  []func(SearchIndexProgress)
	latestSearchIndexProgress     atomic.Pointer[SearchIndexProgress]
	searchRebuild                 = struct {
		mu      sync.Mutex
		gen     uint64
		running bool
		timer   *time.Timer
	}{}
)

// AddSearchIndexProgressListener 注册检索索引重建进度监听回调
func AddSearchIndexProgressListener(fn func(SearchIndexProgress)) {
	if fn == nil {
		return
	}
	searchIndexProgressListenerMu.Lock()
	defer searchIndexProgressListenerMu.Unlock()
	searchIndexProgressListeners = append(searchIndexProgressListeners, fn)
}

// GetLatestSearchIndexProgress 获取最新检索索引进度快照
func GetLatestSearchIndexProgress() *SearchIndexProgress {
	p := latestSearchIndexProgress.Load()
	if p == nil {
		return &SearchIndexProgress{Status: "idle"}
	}
	return p
}

// NotifySearchIndexProgress 广播检索索引构建数字进度
func NotifySearchIndexProgress(total, current int64, status string) {
	if total < 0 {
		total = 0
	}
	if current < 0 {
		current = 0
	}
	if current > total && total > 0 {
		current = total
	}
	percent := 0
	if total > 0 {
		percent = int(current * 100 / total)
	} else if status == "done" {
		percent = 100
	}
	if status == "indexing" {
		isSearchIndexBuilding.Store(true)
	} else {
		isSearchIndexBuilding.Store(false)
	}
	p := SearchIndexProgress{
		Total:   total,
		Current: current,
		Percent: percent,
		Status:  status,
	}
	latestSearchIndexProgress.Store(&p)
	searchIndexProgressListenerMu.RLock()
	listeners := append([]func(SearchIndexProgress){}, searchIndexProgressListeners...)
	searchIndexProgressListenerMu.RUnlock()
	for _, fn := range listeners {
		fn(p)
	}
}

// RequestActiveFilmSearchIndexRebuild 没有可增量的检索索引时才全库扫描。采集结束优先走 ResealSearchIndexMids。
func RequestActiveFilmSearchIndexRebuild() {
	searchRebuild.mu.Lock()
	searchRebuild.gen++
	NotifySearchIndexProgress(0, 0, "indexing")
	if searchRebuild.running {
		searchRebuild.mu.Unlock()
		return
	}
	if searchRebuild.timer != nil {
		searchRebuild.timer.Stop()
	}
	searchRebuild.timer = time.AfterFunc(searchRebuildDebounce, startSearchRebuild)
	searchRebuild.mu.Unlock()
}

func startSearchRebuild() {
	searchRebuild.mu.Lock()
	searchRebuild.timer = nil
	if searchRebuild.running {
		searchRebuild.mu.Unlock()
		return
	}
	searchRebuild.running = true
	searchRebuild.mu.Unlock()

	searchMetaBuildWg.Add(1)
	go func() {
		defer searchMetaBuildWg.Done()
		runSearchRebuild()
	}()
}

func runSearchRebuild() {
	for {
		searchRebuild.mu.Lock()
		seen := searchRebuild.gen
		searchRebuild.mu.Unlock()

		err := scanActiveFilmSearchIndex(seen)
		if err != nil {
			log.Printf("[ActiveReadModel] 检索索引重建失败: %v", err)
			notifySearchIndexIfCurrent(seen, 0, 0, "idle")
		}

		searchRebuild.mu.Lock()
		if searchRebuild.gen == seen {
			searchRebuild.running = false
			searchRebuild.mu.Unlock()
			return
		}
		searchRebuild.mu.Unlock()
	}
}

// notifySearchIndexIfCurrent 只在代数未变时发终态。旧一轮完成不能把新一轮的构建标记清掉。
func notifySearchIndexIfCurrent(seen uint64, total, current int64, status string) {
	searchRebuild.mu.Lock()
	defer searchRebuild.mu.Unlock()
	if searchRebuild.gen != seen {
		return
	}
	NotifySearchIndexProgress(total, current, status)
}

func scanAndStoreActiveFilmSearchIndex(seen uint64) error {
	version := currentSearchIndexVersion()
	if version == "" || db.Mdb == nil {
		notifySearchIndexIfCurrent(seen, 0, 0, "idle")
		return nil
	}
	activeFilmSearchMetasMu.Lock()
	defer activeFilmSearchMetasMu.Unlock()

	started := time.Now()
	items, plays, err := loadSearchIndexParts(version)
	if err != nil {
		return err
	}
	attachPlaySources(items, plays)
	idx := sealSearchMetaIndex(version, items)
	activeFilmSearchMetas.Store(idx)
	cost := time.Since(started).Round(time.Millisecond)
	log.Printf("[ActiveReadModel] 检索索引重建完成 version=%s count=%d cost=%s", version, len(items), cost)
	notifySearchIndexIfCurrent(seen, int64(len(items)), int64(len(items)), "done")
	return nil
}

func currentSearchIndexVersion() string {
	version := GetActiveReadModelVersion()
	if version == "" {
		version = EnsureLiveReadVersion()
	}
	return version
}

func loadSearchIndexParts(version string) ([]FilmSearchMeta, map[int64][]string, error) {
	g, gctx := errgroup.WithContext(context.Background())
	var (
		items []FilmSearchMeta
		plays map[int64][]string
	)
	g.Go(func() error {
		var err error
		items, err = loadFilmSearchMetaItems(gctx, version)
		return err
	})
	g.Go(func() error {
		var err error
		plays, err = loadPlaySources(nil)
		return err
	})
	if err := g.Wait(); err != nil {
		return nil, nil, err
	}
	return items, plays, nil
}
