package snapshot

import (
	"context"
	"log"
	"sort"
	"strings"
	"sync"
	"time"

	"golang.org/x/sync/singleflight"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository/film/shared"
	"server/internal/utils"
)

// nonPrimarySearchIdleTTL 非主站临时检索索引在无访问后的自动释放时长。
var nonPrimarySearchIdleTTL = 5 * time.Minute

type nonPrimarySearchIndex struct {
	sourceID string
	items    []FilmSearchMeta
	posts    *searchPostings
}

var nonPrimarySearchCache struct {
	mu        sync.Mutex
	entry     *nonPrimarySearchIndex
	timer     *time.Timer
	allGen    uint64
	sourceGen map[string]uint64
	sf        singleflight.Group
}

// DropNonPrimarySearchIndex 释放非主站临时检索索引。sourceID 为空时清空全部缓存。
func DropNonPrimarySearchIndex(sourceID string) {
	sourceID = strings.TrimSpace(sourceID)
	nonPrimarySearchCache.mu.Lock()
	defer nonPrimarySearchCache.mu.Unlock()

	if sourceID == "" {
		nonPrimarySearchCache.allGen++
		nonPrimarySearchCache.sourceGen = nil
		clearNonPrimaryEntryLocked()
		return
	}
	if nonPrimarySearchCache.sourceGen == nil {
		nonPrimarySearchCache.sourceGen = make(map[string]uint64)
	}
	nonPrimarySearchCache.sourceGen[sourceID]++
	if nonPrimarySearchCache.entry != nil && nonPrimarySearchCache.entry.sourceID == sourceID {
		clearNonPrimaryEntryLocked()
	}
}

func clearNonPrimaryEntryLocked() {
	nonPrimarySearchCache.entry = nil
	if nonPrimarySearchCache.timer != nil {
		nonPrimarySearchCache.timer.Stop()
		nonPrimarySearchCache.timer = nil
	}
}

func armNonPrimaryTimerLocked(entry *nonPrimarySearchIndex) {
	if nonPrimarySearchCache.timer != nil {
		nonPrimarySearchCache.timer.Stop()
		nonPrimarySearchCache.timer = nil
	}
	ttl := nonPrimarySearchIdleTTL
	if ttl <= 0 {
		return
	}
	nonPrimarySearchCache.timer = time.AfterFunc(ttl, func() {
		nonPrimarySearchCache.mu.Lock()
		defer nonPrimarySearchCache.mu.Unlock()
		if nonPrimarySearchCache.entry != entry {
			return
		}
		nonPrimarySearchCache.entry = nil
		nonPrimarySearchCache.timer = nil
	})
}

func loadNonPrimarySearchIndex(sourceID string) (*nonPrimarySearchIndex, error) {
	sourceID = strings.TrimSpace(sourceID)
	if sourceID == "" || db.Mdb == nil {
		return nil, ErrSearchIndexMissing
	}
	nonPrimarySearchCache.mu.Lock()
	if cur := nonPrimarySearchCache.entry; cur != nil && cur.sourceID == sourceID {
		armNonPrimaryTimerLocked(cur)
		nonPrimarySearchCache.mu.Unlock()
		return cur, nil
	}
	nonPrimarySearchCache.mu.Unlock()

	val, err, _ := nonPrimarySearchCache.sf.Do(sourceID, func() (any, error) {
		nonPrimarySearchCache.mu.Lock()
		if cur := nonPrimarySearchCache.entry; cur != nil && cur.sourceID == sourceID {
			armNonPrimaryTimerLocked(cur)
			nonPrimarySearchCache.mu.Unlock()
			return cur, nil
		}
		startAllGen := nonPrimarySearchCache.allGen
		startSrcGen := nonPrimarySearchCache.sourceGen[sourceID]
		nonPrimarySearchCache.mu.Unlock()

		started := time.Now()
		items, err := loadSearchMetaItemsForSource(context.Background(), sourceID, false, "")
		if err != nil {
			return nil, err
		}
		idx := &nonPrimarySearchIndex{
			sourceID: sourceID,
			items:    items,
			posts:    buildSearchPostings(items),
		}
		nonPrimarySearchCache.mu.Lock()
		if nonPrimarySearchCache.allGen == startAllGen &&
			nonPrimarySearchCache.sourceGen[sourceID] == startSrcGen &&
			currentPrimarySourceID() != sourceID {
			nonPrimarySearchCache.entry = idx
			armNonPrimaryTimerLocked(idx)
		}
		nonPrimarySearchCache.mu.Unlock()
		log.Printf("[ManageFilmSearch] 非主站检索索引按需加载完成 source=%s count=%d cost=%s",
			sourceID, len(items), time.Since(started).Round(time.Millisecond))
		return idx, nil
	})
	if err != nil {
		return nil, err
	}
	idx, _ := val.(*nonPrimarySearchIndex)
	return idx, nil
}

// searchNonPrimaryManageFilms 使用按需懒加载的非主站倒排索引完成全站无盲区关键词打分分页。
func searchNonPrimaryManageFilms(filterCtx *manageFilterContext, page *dto.Page, name string) ([]model.FilmIndex, error) {
	query := utils.BuildQueryContext(name)
	if query.CleanKey == "" {
		page.Total = 0
		page.PageCount = 1
		return []model.FilmIndex{}, nil
	}
	idx, err := loadNonPrimarySearchIndex(filterCtx.sourceID)
	if err != nil {
		return nil, err
	}
	type scored struct {
		pos   int
		score int
	}
	positions, narrowed := idx.posts.lookup(query)
	var hits []scored
	visit := func(pos int) {
		if pos < 0 || pos >= len(idx.items) {
			return
		}
		meta := &idx.items[pos]
		if !filterCtx.passOwnedMeta(meta) {
			return
		}
		score := utils.ScoreFilmMatch(meta.Item, query)
		if score <= 0 {
			return
		}
		hits = append(hits, scored{pos: pos, score: score})
	}
	if narrowed {
		for _, pos := range positions {
			visit(int(pos))
		}
	} else {
		for i := range idx.items {
			visit(i)
		}
	}
	sort.SliceStable(hits, func(i, j int) bool {
		a, b := &idx.items[hits[i].pos], &idx.items[hits[j].pos]
		if hits[i].score != hits[j].score {
			return hits[i].score > hits[j].score
		}
		if a.Item.Hits != b.Item.Hits {
			return a.Item.Hits > b.Item.Hits
		}
		if a.Item.Year != b.Item.Year {
			return a.Item.Year > b.Item.Year
		}
		if a.Item.UpdateStamp != b.Item.UpdateStamp {
			return a.Item.UpdateStamp > b.Item.UpdateStamp
		}
		return a.Mid > b.Mid
	})
	page.Total = len(hits)
	page.PageCount = (page.Total + page.PageSize - 1) / page.PageSize
	if page.PageCount <= 0 {
		page.PageCount = 1
	}
	offset := shared.PageOffset(page)
	if offset >= page.Total {
		return []model.FilmIndex{}, nil
	}
	end := offset + page.PageSize
	if end > len(hits) {
		end = len(hits)
	}
	mids := make([]int64, 0, end-offset)
	for _, hit := range hits[offset:end] {
		if mid := idx.items[hit.pos].Mid; mid > 0 {
			mids = append(mids, mid)
		}
	}
	if len(mids) == 0 {
		return []model.FilmIndex{}, nil
	}
	var list []model.FilmIndex
	if err := db.Mdb.Where("mid IN ?", mids).Find(&list).Error; err != nil {
		return nil, err
	}
	ordered := orderFilmsByMids(mids, list)
	attachFilmScrapes(ordered)
	if err := fillFilmSourceMids(filterCtx.sourceID, ordered); err != nil {
		return nil, err
	}
	return ordered, nil
}
