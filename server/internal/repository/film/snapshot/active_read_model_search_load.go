package snapshot

import (
	"log"
	"runtime"
	"strings"
	"sync"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/utils"
)

const searchMetaLoadPageSize = 5000

type dbMetaRow struct {
	Mid         int64
	Pid         int64
	Cid         int64
	Name        string
	Hits        int64
	Score       float64
	Year        int64
	UpdateStamp int64
}

func loadFilmSearchMetaIndex(version string) *filmSearchMetaIndex {
	version = strings.TrimSpace(version)
	if version == "" || db.Mdb == nil {
		return nil
	}
	if cur := activeFilmSearchMetas.Load(); cur != nil && cur.Version == version {
		return cur
	}
	val, err, _ := searchMetaBuildSf.Do(version, func() (any, error) {
		if cur := activeFilmSearchMetas.Load(); cur != nil && cur.Version == version {
			return cur, nil
		}
		items, err := loadFilmSearchMetaItems(version)
		if err != nil {
			return nil, err
		}
		idx := &filmSearchMetaIndex{Version: version, Items: items}
		activeFilmSearchMetasMu.Lock()
		activeFilmSearchMetas.Store(idx)
		activeFilmSearchMetasMu.Unlock()
		log.Printf("[ActiveReadModel] 检索索引分页加载完成 version=%s count=%d", version, len(items))
		return idx, nil
	})
	if err != nil || val == nil {
		if err != nil {
			log.Printf("[ActiveReadModel] 检索索引加载失败 version=%s: %v", version, err)
		}
		return nil
	}
	return val.(*filmSearchMetaIndex)
}

func loadFilmSearchMetaItems(version string) ([]FilmSearchMeta, error) {
	items := make([]FilmSearchMeta, 0)
	var lastMid int64
	for {
		var rows []dbMetaRow
		if err := db.Mdb.Model(&model.FilmIndex{}).
			Select("mid, pid, cid, name, hits, score, year, update_stamp").
			Where("mid > ?", lastMid).
			Order("mid ASC").
			Limit(searchMetaLoadPageSize).
			Find(&rows).Error; err != nil {
			return nil, err
		}
		if len(rows) == 0 {
			break
		}
		pageItems := make([]FilmSearchMeta, len(rows))
		fillSearchMetaPage(rows, pageItems)
		items = append(items, pageItems...)
		lastMid = rows[len(rows)-1].Mid
		if len(rows) < searchMetaLoadPageSize {
			break
		}
	}
	return items, nil
}

func fillSearchMetaPage(rows []dbMetaRow, items []FilmSearchMeta) {
	numWorkers := runtime.GOMAXPROCS(0)
	if numWorkers < 1 {
		numWorkers = 1
	}
	if numWorkers > 8 {
		numWorkers = 8
	}
	if len(rows) < 200 {
		numWorkers = 1
	}
	chunkSize := (len(rows) + numWorkers - 1) / numWorkers
	var wg sync.WaitGroup
	for w := 0; w < numWorkers; w++ {
		startIdx := w * chunkSize
		endIdx := startIdx + chunkSize
		if startIdx >= len(rows) {
			break
		}
		if endIdx > len(rows) {
			endIdx = len(rows)
		}
		wg.Add(1)
		go func(s, e int) {
			defer wg.Done()
			for i := s; i < e; i++ {
				r := rows[i]
				item := utils.FilmSearchItem{
					Mid:         r.Mid,
					Name:        r.Name,
					Hits:        r.Hits,
					Score:       r.Score,
					Year:        r.Year,
					UpdateStamp: r.UpdateStamp,
				}
				utils.FillSearchDerivedFields(&item)
				items[i] = FilmSearchMeta{Mid: r.Mid, Pid: r.Pid, Cid: r.Cid, Item: item}
			}
		}(startIdx, endIdx)
	}
	wg.Wait()
}
