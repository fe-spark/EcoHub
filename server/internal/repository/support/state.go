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

type MappingSnapshot struct {
	Area              map[string]string
	Lang              map[string]string
	Filter            map[string]bool
	Attribute         map[string]string
	Plot              map[string]string
	CategoryRoot      map[string]string
	CategorySub       map[string]string
	CategoryRootRegex []categoryRuleMatcher
	CategorySubRegex  []categoryRuleMatcher
	Source            map[string]int64
}

var (
	mappingState atomic.Pointer[MappingSnapshot]
	categoryNameCache sync.Map

	idToPid = make(map[int64]int64)
	catMu   sync.RWMutex
)

func init() {
	mappingState.Store(&MappingSnapshot{
		Area:         make(map[string]string),
		Lang:         make(map[string]string),
		Filter:       make(map[string]bool),
		Attribute:    make(map[string]string),
		Plot:         make(map[string]string),
		CategoryRoot: make(map[string]string),
		CategorySub:  make(map[string]string),
		Source:       make(map[string]int64),
	})
}

func getMappingSnapshot() *MappingSnapshot {
	s := mappingState.Load()
	if s == nil {
		return &MappingSnapshot{
			Area:         make(map[string]string),
			Lang:         make(map[string]string),
			Filter:       make(map[string]bool),
			Attribute:    make(map[string]string),
			Plot:         make(map[string]string),
			CategoryRoot: make(map[string]string),
			CategorySub:  make(map[string]string),
			Source:       make(map[string]int64),
		}
	}
	return s
}
