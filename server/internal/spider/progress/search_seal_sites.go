package progress

import "sync"

// SearchSealSite 全局封口队列里的一个采集站。
type SearchSealSite struct {
	SourceID string `json:"sourceId"`
	State    string `json:"state"`
	Ahead    int    `json:"ahead"`
}

var (
	sealSitesMu sync.Mutex
	sealSites   []SearchSealSite
)

// SetSearchSealSites 替换当前封口队列快照。空列表表示没有站在封。
func SetSearchSealSites(sites []SearchSealSite) {
	sealSitesMu.Lock()
	defer sealSitesMu.Unlock()
	if len(sites) == 0 {
		sealSites = nil
		return
	}
	sealSites = append([]SearchSealSite(nil), sites...)
}

// CurrentSearchSealSites 返回封口队列快照的副本。
func CurrentSearchSealSites() []SearchSealSite {
	sealSitesMu.Lock()
	defer sealSitesMu.Unlock()
	if len(sealSites) == 0 {
		return nil
	}
	return append([]SearchSealSite(nil), sealSites...)
}
