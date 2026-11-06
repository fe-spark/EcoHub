package support

import (
	"regexp"
	"sync"
	"sync/atomic"
)

type categoryRuleMatcher struct {
	Pattern *regexp.Regexp
	Target  string
}
type SourceCategoryInfo struct {
	RawName            string
	ParentSourceTypeId int64
}

type sourceCategoryRules struct {
	Exact map[string]string
	Regex []categoryRuleMatcher
}

type MappingSnapshot struct {
	Area             map[string]string
	Lang             map[string]string
	Filter           map[string]bool
	Attribute        map[string]string
	Plot             map[string]string
	CategoryRoot     map[string]sourceCategoryRules // sourceId -> 该站一级规则
	CategorySub      map[string]sourceCategoryRules // sourceId -> 该站二级规则
	Source           map[string]int64
	SourceCategories map[string]SourceCategoryInfo
}

var (
	mappingState           atomic.Pointer[MappingSnapshot]
	categoryNameCache      sync.Map
	categoryStableKeyCache sync.Map
	idToPid                = make(map[int64]int64)
	categoryShowMap        = make(map[int64]bool)
	catInitialized         = false
	catMu                  sync.RWMutex
)

func init() {
	mappingState.Store(emptyMappingSnapshot())
}

func emptyMappingSnapshot() *MappingSnapshot {
	return &MappingSnapshot{
		Area:             make(map[string]string),
		Lang:             make(map[string]string),
		Filter:           make(map[string]bool),
		Attribute:        make(map[string]string),
		Plot:             make(map[string]string),
		CategoryRoot:     make(map[string]sourceCategoryRules),
		CategorySub:      make(map[string]sourceCategoryRules),
		Source:           make(map[string]int64),
		SourceCategories: make(map[string]SourceCategoryInfo),
	}
}

func getMappingSnapshot() *MappingSnapshot {
	s := mappingState.Load()
	if s == nil {
		return emptyMappingSnapshot()
	}
	return s
}
