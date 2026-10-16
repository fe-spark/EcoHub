package spider

import "sync"

// sourceExclusive 保证同一采集站的分类写入和删除不会交错。
var sourceExclusive sync.Map

func sourceExclusiveMu(id string) *sync.Mutex {
	if id == "" {
		id = "-"
	}
	val, _ := sourceExclusive.LoadOrStore(id, &sync.Mutex{})
	return val.(*sync.Mutex)
}

// WithSourceExclusive 串行化同一采集站的分类保存与删除。
func WithSourceExclusive(id string, fn func() error) error {
	mu := sourceExclusiveMu(id)
	mu.Lock()
	defer mu.Unlock()
	return fn()
}
