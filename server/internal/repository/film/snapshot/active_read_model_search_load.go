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

const filmMetaSelect = "mid, pid, cid, first_source_id, name, hits, score, year, update_stamp, area, language, class_tag, category_key, root_category_key"

type dbMetaRow struct {
	Mid             int64
	Pid             int64
	Cid             int64
	FirstSourceId   string
	Name            string
	Hits            int64
	Score           float64
	Year            int64
	UpdateStamp     int64
	Area            string
	Language        string
	ClassTag        string
	CategoryKey     string
	RootCategoryKey string
}

func loadFilmSearchMetaIndex(version string) (*filmSearchMetaIndex, error) {
	version = strings.TrimSpace(version)
	if version == "" || db.Mdb == nil {
		return nil, ErrSearchIndexMissing
	}
	if cur := activeFilmSearchMetas.Load(); cur != nil && cur.Version == version {
		return cur, nil
	}
	val, err, _ := searchMetaBuildSf.Do(version, func() (any, error) {
		if cur := activeFilmSearchMetas.Load(); cur != nil && cur.Version == version {
			return cur, nil
		}
		items, err := loadFilmSearchMetaItems(version)
		if err != nil {
			return nil, err
		}
		plays, err := loadPlaySources(nil)
		if err != nil {
			return nil, err
		}
		attachPlaySources(items, plays)
		idx := sealSearchMetaIndex(version, items)
		activeFilmSearchMetasMu.Lock()
		activeFilmSearchMetas.Store(idx)
		activeFilmSearchMetasMu.Unlock()
		log.Printf("[ActiveReadModel] 检索索引分页加载完成 version=%s count=%d", version, len(items))
		return idx, nil
	})
	if err != nil {
		return nil, err
	}
	idx, ok := val.(*filmSearchMetaIndex)
	if !ok || idx == nil {
		return nil, ErrSearchIndexMissing
	}
	return idx, nil
}

func loadFilmSearchMetaItems(version string) ([]FilmSearchMeta, error) {
	items := make([]FilmSearchMeta, 0)
	var lastMid int64
	for {
		var rows []dbMetaRow
		if err := db.Mdb.Model(&model.FilmIndex{}).
			Select(filmMetaSelect).
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
				items[i] = metaFromRow(rows[i])
			}
		}(startIdx, endIdx)
	}
	wg.Wait()
}

func metaFromRow(r dbMetaRow) FilmSearchMeta {
	item := utils.FilmSearchItem{
		Mid:         r.Mid,
		Name:        r.Name,
		Hits:        r.Hits,
		Score:       r.Score,
		Year:        r.Year,
		UpdateStamp: r.UpdateStamp,
	}
	utils.FillSearchDerivedFields(&item)
	return FilmSearchMeta{
		Mid:             r.Mid,
		Pid:             r.Pid,
		Cid:             r.Cid,
		FirstSourceId:   strings.TrimSpace(r.FirstSourceId),
		Area:            strings.TrimSpace(r.Area),
		Language:        strings.TrimSpace(r.Language),
		ClassTag:        strings.TrimSpace(r.ClassTag),
		CategoryKey:     strings.TrimSpace(r.CategoryKey),
		RootCategoryKey: strings.TrimSpace(r.RootCategoryKey),
		Item:            item,
	}
}
