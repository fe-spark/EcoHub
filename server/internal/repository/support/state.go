package support

import (
	"regexp"
	"sync"
)

type categoryRuleMatcher struct {
	Pattern *regexp.Regexp
	Target  string
}

var (
	cacheAreaMap         sync.Map
	cacheLangMap         sync.Map
	cacheFilterMap       sync.Map
	cacheAttribute       sync.Map
	cachePlotMap         sync.Map
	cacheCategoryRootMap sync.Map
	cacheCategorySubMap  sync.Map
	categoryRootRegexMu  sync.RWMutex
	categorySubRegexMu   sync.RWMutex
	categoryRootRegex    []categoryRuleMatcher
	categorySubRegex     []categoryRuleMatcher
	cacheCategoryMap     sync.Map
	cacheSourceMap       sync.Map

	idToPid = make(map[int64]int64)
	catMu   sync.RWMutex
)
