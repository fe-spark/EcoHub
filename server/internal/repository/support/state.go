package support

import (
	"sync"
)

var (
	cacheAreaMap         sync.Map
	cacheLangMap         sync.Map
	cacheFilterMap       sync.Map
	cacheAttribute       sync.Map
	cachePlotMap         sync.Map
	cacheCategoryMap     sync.Map
	cacheSourceMap       sync.Map

	idToPid = make(map[int64]int64)
	catMu   sync.RWMutex
)
