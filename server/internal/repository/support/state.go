package support

import (
	"sync"

	"server/internal/model"
)

var (
	cacheAreaMap     sync.Map
	cacheLangMap     sync.Map
	cacheFilterMap   sync.Map
	cacheAttribute   sync.Map
	cachePlotMap     sync.Map
	cacheCategoryMap sync.Map
	cacheSourceMap   sync.Map
	cacheMainCats    []model.Category

	idToPid = make(map[int64]int64)
	catMu   sync.RWMutex
)
