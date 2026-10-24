package snapshot

import (
	"context"
	"database/sql"
	"errors"
	"log"
	"runtime"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/utils"

	"golang.org/x/sync/errgroup"
)

const searchMetaLoadPageSize = 5000

// 少于这个数量时分段并发的连接开销大于收益。
const searchMetaParallelMin = 16384

const filmMetaSelect = "mid, pid, cid, first_source_id, name, hits, score, year, update_stamp, area, language, class_tag, category_key, root_category_key"

const filmMetaLoadSQL = "SELECT " + filmMetaSelect + " FROM " + model.TableFilmIndex + " WHERE deleted_at IS NULL"

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

type midSpan struct {
	lo, hi  int64
	bounded bool
}

func loadFilmSearchMetaIndex(version string) (*filmSearchMetaIndex, error) {
	version = strings.TrimSpace(version)
	if version == "" || db.Mdb == nil {
		return nil, ErrSearchIndexMissing
	}
	if cur := activeFilmSearchMetas.Load(); cur != nil && cur.Version == version {
		return cur, nil
	}
	return buildAndStoreFilmSearchMetaIndex(version)
}

// buildAndStoreFilmSearchMetaIndex 重建检索索引，完成后一次换上。重建期间请求继续读旧索引。
func buildAndStoreFilmSearchMetaIndex(version string) (*filmSearchMetaIndex, error) {
	version = strings.TrimSpace(version)
	if version == "" || db.Mdb == nil {
		return nil, ErrSearchIndexMissing
	}
	val, err, _ := searchMetaBuildSf.Do(version, func() (any, error) {
		activeFilmSearchMetasMu.Lock()
		defer activeFilmSearchMetasMu.Unlock()
		if cur := activeFilmSearchMetas.Load(); cur != nil && cur.Version == version {
			return cur, nil
		}
		isSearchIndexBuilding.Store(true)
		defer isSearchIndexBuilding.Store(false)
		started := time.Now()
		log.Printf("[ActiveReadModel] 检索索引加载中 version=%s", version)
		var (
			items []FilmSearchMeta
			plays map[int64][]string
		)
		g, gctx := errgroup.WithContext(context.Background())
		g.Go(func() error {
			var err error
			items, err = loadFilmSearchMetaItems(gctx, version)
			return err
		})
		g.Go(func() error {
			var err error
			plays, err = loadPlaySources(nil)
			return err
		})
		if err := g.Wait(); err != nil {
			return nil, err
		}
		log.Printf("[ActiveReadModel] 检索索引封口中 version=%s 影片=%d 播放线路=%d", version, len(items), len(plays))
		attachPlaySources(items, plays)
		idx := sealSearchMetaIndex(version, items)
		activeFilmSearchMetas.Store(idx)
		log.Printf("[ActiveReadModel] 检索索引加载完成 version=%s count=%d cost=%s", version, len(items), time.Since(started).Round(time.Millisecond))
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

func loadFilmSearchMetaItems(ctx context.Context, version string) ([]FilmSearchMeta, error) {
	var total int64
	if err := db.Mdb.WithContext(ctx).Model(&model.FilmIndex{}).Count(&total).Error; err != nil {
		return nil, err
	}
	if total == 0 {
		log.Printf("[ActiveReadModel] 检索索引加载中 version=%s 影片 0/0", version)
		return []FilmSearchMeta{}, nil
	}
	progress := newSearchLoadProgress(version, total)
	spans, err := searchMetaSpans(ctx, total)
	if err != nil {
		return nil, err
	}
	ctx, cancel := context.WithCancel(ctx)
	defer cancel()

	items := make([]FilmSearchMeta, 0, total)
	batches := make(chan []dbMetaRow, len(spans))
	var mu sync.Mutex
	var firstErr error
	fail := func(err error) {
		if err == nil {
			return
		}
		mu.Lock()
		if firstErr == nil {
			firstErr = err
			cancel()
		}
		mu.Unlock()
	}

	var scanWg sync.WaitGroup
	for _, span := range spans {
		scanWg.Add(1)
		go func(span midSpan) {
			defer scanWg.Done()
			if err := streamMetaRows(ctx, span, batches); err != nil && ctx.Err() == nil {
				fail(err)
			}
		}(span)
	}
	go func() {
		scanWg.Wait()
		close(batches)
	}()

	parallelPage := len(spans) == 1
	var fillWg sync.WaitGroup
	for i := 0; i < len(spans); i++ {
		fillWg.Add(1)
		go func() {
			defer fillWg.Done()
			for rows := range batches {
				if ctx.Err() != nil {
					continue
				}
				page := make([]FilmSearchMeta, len(rows))
				if parallelPage {
					fillSearchMetaPage(rows, page)
				} else {
					for n := range rows {
						page[n] = metaFromRow(rows[n])
					}
				}
				mu.Lock()
				items = append(items, page...)
				mu.Unlock()
				progress.add(len(page))
			}
		}()
	}
	fillWg.Wait()
	if firstErr != nil {
		return nil, firstErr
	}
	return items, nil
}

type searchLoadProgress struct {
	version string
	total   int64
	loaded  int64
	next    int64
	mu      sync.Mutex
}

func newSearchLoadProgress(version string, total int64) *searchLoadProgress {
	step := total / 10
	if step < 1 {
		step = total
	}
	return &searchLoadProgress{version: version, total: total, next: step}
}

func (p *searchLoadProgress) add(n int) {
	if p == nil || n <= 0 || p.total <= 0 {
		return
	}
	p.mu.Lock()
	defer p.mu.Unlock()
	p.loaded += int64(n)
	if p.loaded < p.next && p.loaded < p.total {
		return
	}
	loaded := p.loaded
	if loaded > p.total {
		loaded = p.total
	}
	log.Printf("[ActiveReadModel] 检索索引加载中 version=%s 影片 %d/%d %d%%",
		p.version, loaded, p.total, loaded*100/p.total)
	if p.loaded >= p.total {
		p.next = p.total + 1
		return
	}
	step := p.total / 10
	if step < 1 {
		step = 1
	}
	p.next = (p.loaded/step + 1) * step
}

func searchMetaSpans(ctx context.Context, total int64) ([]midSpan, error) {
	workers := 1
	if total >= searchMetaParallelMin {
		workers = runtime.GOMAXPROCS(0)
		if workers > 8 {
			workers = 8
		}
	}
	if workers == 1 {
		return []midSpan{{}}, nil
	}
	row := db.Mdb.WithContext(ctx).Model(&model.FilmIndex{}).Select("MIN(mid), MAX(mid)").Row()
	var lo, hi sql.NullInt64
	if err := row.Scan(&lo, &hi); err != nil {
		return nil, err
	}
	if !lo.Valid || !hi.Valid || hi.Int64 < lo.Int64 {
		return nil, errors.New("影片检索边界为空")
	}
	span := (hi.Int64 - lo.Int64) / int64(workers)
	if span < 1 {
		return []midSpan{{}}, nil
	}
	out := make([]midSpan, workers)
	for i := 0; i < workers; i++ {
		start := lo.Int64 + int64(i)*span
		end := start + span
		if i == workers-1 {
			end = hi.Int64 + 1
		}
		out[i] = midSpan{lo: start, hi: end, bounded: true}
	}
	return out, nil
}

func streamMetaRows(ctx context.Context, span midSpan, batches chan<- []dbMetaRow) error {
	q := filmMetaLoadSQL
	var args []any
	if span.bounded {
		q += " AND mid >= ? AND mid < ?"
		args = []any{span.lo, span.hi}
	}
	rows, err := db.Mdb.WithContext(ctx).Raw(q, args...).Rows()
	if err != nil {
		return err
	}
	defer rows.Close()
	batch := make([]dbMetaRow, 0, searchMetaLoadPageSize)
	flush := func() error {
		if len(batch) == 0 {
			return nil
		}
		out := batch
		batch = make([]dbMetaRow, 0, searchMetaLoadPageSize)
		select {
		case <-ctx.Done():
			return ctx.Err()
		case batches <- out:
			return nil
		}
	}
	for rows.Next() {
		if ctx.Err() != nil {
			return ctx.Err()
		}
		item, err := scanMetaRow(rows)
		if err != nil {
			return err
		}
		batch = append(batch, item)
		if len(batch) >= searchMetaLoadPageSize {
			if err := flush(); err != nil {
				return err
			}
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return flush()
}

func scanMetaRow(rows *sql.Rows) (dbMetaRow, error) {
	var mid, pid, cid, hits, year, stamp sql.NullInt64
	var score sql.NullFloat64
	var src, name, area, lang, tag, ck, rk sql.NullString
	if err := rows.Scan(&mid, &pid, &cid, &src, &name, &hits, &score, &year, &stamp, &area, &lang, &tag, &ck, &rk); err != nil {
		return dbMetaRow{}, err
	}
	return dbMetaRow{
		Mid:             mid.Int64,
		Pid:             pid.Int64,
		Cid:             cid.Int64,
		FirstSourceId:   src.String,
		Name:            name.String,
		Hits:            hits.Int64,
		Score:           score.Float64,
		Year:            year.Int64,
		UpdateStamp:     stamp.Int64,
		Area:            area.String,
		Language:        lang.String,
		ClassTag:        tag.String,
		CategoryKey:     ck.String,
		RootCategoryKey: rk.String,
	}, nil
}

func fillSearchMetaPage(rows []dbMetaRow, items []FilmSearchMeta) {
	numWorkers := runtime.GOMAXPROCS(0)
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
