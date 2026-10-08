package writer

import (
	"sort"
	"strconv"
	"strings"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository/film/shared"
	"server/internal/repository/support"
	"server/internal/utils"
)

const matchKeyLockStripes = 65536

var (
	filmMidLocks      [2048]sync.Mutex
	filmMatchKeyLocks [matchKeyLockStripes]sync.Mutex
)

func getFilmMidLock(mid int64) *sync.Mutex {
	if mid <= 0 {
		return &filmMidLocks[0]
	}
	return &filmMidLocks[uint64(mid)%2048]
}

func matchKeyLockIndex(key string) int {
	key = strings.TrimSpace(key)
	if key == "" {
		return 0
	}
	hash := utils.GenerateHashKey(key)
	var idx uint64
	if len(hash) >= 8 {
		idx, _ = strconv.ParseUint(hash[:8], 16, 32)
	}
	return int(idx % matchKeyLockStripes)
}

func matchKeyLockIndexes(keys []string) []int {
	seen := make(map[int]struct{}, len(keys))
	out := make([]int, 0, len(keys))
	for _, key := range keys {
		idx := matchKeyLockIndex(key)
		if _, ok := seen[idx]; ok {
			continue
		}
		seen[idx] = struct{}{}
		out = append(out, idx)
	}
	sort.Ints(out)
	return out
}

func peerMatchKeys(source *model.FilmSource, detail model.MovieDetail) []string {
	pid := detail.RawPid
	if source != nil {
		if local := support.GetRootId(support.GetLocalCategoryId(source.Id, detail.Cid)); local > 0 {
			pid = local
		} else if local := support.GetRootId(support.GetLocalCategoryId(source.Id, detail.RawPid)); local > 0 {
			pid = local
		}
	}
	keys := shared.BuildMovieMatchKeysWithCategory(detail.DbId, detail.Name, pid)
	sort.Strings(keys)
	return keys
}

func peekPeerFilmMid(source *model.FilmSource, detail model.MovieDetail) int64 {
	if db.Mdb == nil || source == nil {
		return 0
	}
	var mapping model.MovieSourceMapping
	if err := db.Mdb.Where("source_id = ? AND source_mid = ?", source.Id, detail.Id).
		First(&mapping).Error; err == nil && mapping.GlobalMid > 0 {
		return mapping.GlobalMid
	}
	allKeys := peerMatchKeys(source, detail)
	if len(allKeys) == 0 {
		return 0
	}
	var matchKey model.MovieMatchKey
	if err := db.Mdb.Where("match_key IN ?", allKeys).Order("mid ASC").First(&matchKey).Error; err == nil {
		return matchKey.Mid
	}
	return 0
}

func lockPeerCollect(source *model.FilmSource, detail model.MovieDetail) func() {
	// 匹配键锁按下标升序获取，所有片子的加锁顺序一致。
	held := make([]*sync.Mutex, 0, 8)
	for _, idx := range matchKeyLockIndexes(peerMatchKeys(source, detail)) {
		mu := &filmMatchKeyLocks[idx]
		mu.Lock()
		held = append(held, mu)
	}
	if source != nil {
		mu := getSourceWriteLock(source.Id)
		mu.Lock()
		held = append(held, mu)
	}
	if mid := peekPeerFilmMid(source, detail); mid > 0 {
		mu := getFilmMidLock(mid)
		mu.Lock()
		held = append(held, mu)
	}
	return func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i].Unlock()
		}
	}
}

// lockPeerCollectPage 锁住这一页的全部匹配键，再锁本站。
// 锁下标升序，和单片路径一致。页内不再按片预查 mid。
func lockPeerCollectPage(source *model.FilmSource, details []model.MovieDetail) func() {
	keys := make([]string, 0, len(details)*3)
	for _, detail := range details {
		keys = append(keys, peerMatchKeys(source, detail)...)
	}
	held := make([]*sync.Mutex, 0, 8)
	for _, idx := range matchKeyLockIndexes(keys) {
		mu := &filmMatchKeyLocks[idx]
		mu.Lock()
		held = append(held, mu)
	}
	if source != nil {
		mu := getSourceWriteLock(source.Id)
		mu.Lock()
		held = append(held, mu)
	}
	return func() {
		for i := len(held) - 1; i >= 0; i-- {
			held[i].Unlock()
		}
	}
}

func saveSinglePeerDetailSynced(source *model.FilmSource, detail model.MovieDetail) (int64, bool, bool, error) {
	unlock := lockPeerCollect(source, detail)
	defer unlock()
	return saveSinglePeerDetailTx(source, detail)
}
