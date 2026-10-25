package snapshot

import (
	"context"
	"log"
	"sync"
	"time"

	"server/internal/infra/db"

	"golang.org/x/sync/errgroup"
)

const defaultSearchRebuildDebounce = 2 * time.Second

var (
	searchRebuildDebounce     = defaultSearchRebuildDebounce
	scanActiveFilmSearchIndex = scanAndStoreActiveFilmSearchIndex
	searchRebuild             = struct {
		mu      sync.Mutex
		gen     uint64
		running bool
		timer   *time.Timer
	}{}
)

// RequestActiveFilmSearchIndexRebuild 采集队列结束时调用。只递增代数并延后启动，不在调用方扫库。
func RequestActiveFilmSearchIndexRebuild() {
	searchRebuild.mu.Lock()
	searchRebuild.gen++
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
	go runSearchRebuild()
}

func runSearchRebuild() {
	for {
		searchRebuild.mu.Lock()
		seen := searchRebuild.gen
		searchRebuild.mu.Unlock()

		err := scanActiveFilmSearchIndex()
		if err != nil {
			log.Printf("[ActiveReadModel] 检索索引重建失败: %v", err)
		}

		searchRebuild.mu.Lock()
		if err != nil && searchRebuild.gen == seen {
			searchRebuild.running = false
			searchRebuild.mu.Unlock()
			return
		}
		if searchRebuild.gen == seen {
			searchRebuild.running = false
			searchRebuild.mu.Unlock()
			return
		}
		searchRebuild.mu.Unlock()
	}
}

func scanAndStoreActiveFilmSearchIndex() error {
	version := currentSearchIndexVersion()
	if version == "" || db.Mdb == nil {
		return nil
	}
	isSearchIndexBuilding.Store(true)
	defer isSearchIndexBuilding.Store(false)
	started := time.Now()
	log.Printf("[ActiveReadModel] 检索索引重建开始 version=%s", version)
	items, plays, err := loadSearchIndexParts(version)
	if err != nil {
		return err
	}
	attachPlaySources(items, plays)
	idx := sealSearchMetaIndex(version, items)
	activeFilmSearchMetasMu.Lock()
	activeFilmSearchMetas.Store(idx)
	activeFilmSearchMetasMu.Unlock()
	log.Printf("[ActiveReadModel] 检索索引重建完成 version=%s count=%d cost=%s", version, len(items), time.Since(started).Round(time.Millisecond))
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
