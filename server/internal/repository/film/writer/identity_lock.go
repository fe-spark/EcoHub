package writer

import (
	"errors"
	"regexp"
	"strconv"
	"strings"
	"sync"

	"server/internal/utils"
)

var errNilCollectSource = errors.New("采集源为空")

var yearRegex = regexp.MustCompile(`[1-9][0-9]{3}`)

func parseYear(raw string) int64 {
	m := yearRegex.FindString(raw)
	if m == "" {
		return 0
	}
	y, _ := strconv.ParseInt(m, 10, 64)
	return y
}

func uniqueMIDs(mids []int64) []int64 {
	out := make([]int64, 0, len(mids))
	seen := make(map[int64]struct{}, len(mids))
	for _, m := range mids {
		if m <= 0 {
			continue
		}
		if _, ok := seen[m]; ok {
			continue
		}
		seen[m] = struct{}{}
		out = append(out, m)
	}
	return out
}

var filmIdentityLocks [matchKeyLockStripes]sync.Mutex

func getFilmIdentityLock(name string) *sync.Mutex {
	clean := utils.NormalizeIdentityTitle(name)
	if clean == "" {
		clean = strings.TrimSpace(name)
	}
	hash := utils.GenerateHashKey(clean)
	var idx uint64
	if len(hash) >= 8 {
		idx, _ = strconv.ParseUint(hash[:8], 16, 32)
	}
	return &filmIdentityLocks[idx%matchKeyLockStripes]
}

var sourceWriteLocks sync.Map

func getSourceWriteLock(sourceID string) *sync.Mutex {
	if lock, ok := sourceWriteLocks.Load(sourceID); ok {
		return lock.(*sync.Mutex)
	}
	lock := &sync.Mutex{}
	actual, _ := sourceWriteLocks.LoadOrStore(sourceID, lock)
	return actual.(*sync.Mutex)
}
