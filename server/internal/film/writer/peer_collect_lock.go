package writer

import (
	"sort"
	"strconv"
	"strings"
	"sync"

	"server/internal/film/shared"
	"server/internal/model"
	"server/internal/taxonomy"
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

func resolveMatchCategoryPid(source *model.FilmSource, detail model.MovieDetail) int64 {
	sourceId := ""
	if source != nil {
		sourceId = strings.TrimSpace(source.Id)
	}
	if sourceId == "" || sourceId == "manual" {
		if detail.Cid > 0 {
			if root := taxonomy.GetRootId(detail.Cid); root > 0 {
				return root
			}
		}
		if detail.Pid > 0 {
			return detail.Pid
		}
		return detail.RawPid
	}

	sourceCid := detail.Cid
	sourcePid := detail.Pid
	if detail.RawCid > 0 {
		sourceCid = detail.RawCid
	}
	if detail.RawPid > 0 {
		sourcePid = detail.RawPid
	}

	if sourceCid > 0 {
		if localCid := taxonomy.GetLocalCategoryId(sourceId, sourceCid); localCid > 0 {
			if root := taxonomy.GetRootId(localCid); root > 0 {
				return root
			}
		}
	}
	if sourcePid > 0 {
		if localPid := taxonomy.GetLocalCategoryId(sourceId, sourcePid); localPid > 0 {
			if root := taxonomy.GetRootId(localPid); root > 0 {
				return root
			}
		}
	}
	if detail.RawPid > 0 {
		return detail.RawPid
	}
	return detail.Pid
}

func peerMatchKeys(source *model.FilmSource, detail model.MovieDetail) []string {
	pid := resolveMatchCategoryPid(source, detail)
	keys := shared.BuildMovieMatchKeysWithCategory(detail.DbId, detail.Name, pid)
	sort.Strings(keys)
	return keys
}

func lockPeerCollect(source *model.FilmSource, _ model.MovieDetail) func() {
	// 全站平权模式下各采集站仅写入自身归属的档案、映射与播放线路，按 source.Id 串行即可，避免跨站同名片虚锁阻塞并发写槽。
	if source == nil || strings.TrimSpace(source.Id) == "" {
		return func() {}
	}
	mu := getSourceWriteLock(source.Id)
	mu.Lock()
	return mu.Unlock
}

// lockPeerCollectPage 仅锁当前采集站，避免不同站点因同名片在 Go 内存 matchKey 锁上跨站串行阻塞。
func lockPeerCollectPage(source *model.FilmSource, _ []model.MovieDetail) func() {
	if source == nil || strings.TrimSpace(source.Id) == "" {
		return func() {}
	}
	mu := getSourceWriteLock(source.Id)
	mu.Lock()
	return mu.Unlock
}

func saveSinglePeerDetailSynced(source *model.FilmSource, detail model.MovieDetail) (int64, bool, bool, error) {
	unlock := lockPeerCollect(source, detail)
	defer unlock()
	mid, isNew, playChanged, _, err := saveSinglePeerDetailTx(source, detail)
	return mid, isNew, playChanged, err
}
