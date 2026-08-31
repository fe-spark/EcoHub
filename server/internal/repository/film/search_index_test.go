package film

import (
	"fmt"
	"testing"

	"server/internal/utils"
)

func testSearchIndex(rows []filmSearchIndexRow) *filmSearchMemoryIndex {
	idx := &filmSearchMemoryIndex{Items: parallelBuildItems(rows)}
	idx.buildInverted()
	return idx
}

func candMids(idx *filmSearchMemoryIndex, cands []int32) map[int64]struct{} {
	out := make(map[int64]struct{}, len(cands))
	for _, id := range cands {
		if int(id) < 0 || int(id) >= len(idx.Items) {
			continue
		}
		out[idx.Items[id].Mid] = struct{}{}
	}
	return out
}

func hasMid(mids map[int64]struct{}, mid int64) bool {
	_, ok := mids[mid]
	return ok
}

func TestCollectCandidatesRecallAndPrune(t *testing.T) {
	rows := []filmSearchIndexRow{
		{Mid: 101, Name: "庆余年 第二季", Hits: 100},
		{Mid: 102, Name: "庆余年 第一季", Hits: 90},
		{Mid: 103, Name: "关于庆余年的拍摄花絮与解说", Hits: 80},
		{Mid: 104, Name: "流浪地球2", Hits: 70},
		{Mid: 105, Name: "凡人修仙传", Hits: 60},
		{Mid: 106, Name: "星际穿越", Hits: 50},
		{Mid: 107, Name: "小夜测试", Hits: 40},
		{Mid: 108, Name: "选择之她·他", Hits: 30},
		{Mid: 109, Name: "仙逆", Hits: 20},
	}
	for i := 0; i < 40; i++ {
		rows = append(rows, filmSearchIndexRow{
			Mid:  int64(1000 + i),
			Name: fmt.Sprintf("占位影片%02d", i),
			Hits: int64(i),
		})
	}
	idx := testSearchIndex(rows)
	if len(idx.Items) < 45 {
		t.Fatalf("expected padded index, got %d items", len(idx.Items))
	}

	assertHas := func(t *testing.T, keyword string, mid int64) []int32 {
		t.Helper()
		q := utils.BuildQueryContext(keyword)
		cands := idx.collectCandidates(q)
		if cands == nil {
			t.Fatalf("%q: collectCandidates returned nil (full scan)", keyword)
		}
		if len(cands) == 0 {
			t.Fatalf("%q: no candidates", keyword)
		}
		if len(cands) >= len(idx.Items) {
			t.Fatalf("%q: candidates=%d not pruned (items=%d)", keyword, len(cands), len(idx.Items))
		}
		if !hasMid(candMids(idx, cands), mid) {
			t.Fatalf("%q: missing mid=%d (cands=%d)", keyword, mid, len(cands))
		}
		return cands
	}

	t.Run("chinese_title", func(t *testing.T) {
		assertHas(t, "庆余年", 101)
	})
	t.Run("pinyin_initials", func(t *testing.T) {
		assertHas(t, "lldq", 104)
	})
	t.Run("pinyin_full", func(t *testing.T) {
		assertHas(t, "liulang", 104)
	})
	t.Run("pinyin_polyphone", func(t *testing.T) {
		assertHas(t, "frxxz", 105)
	})
	t.Run("pinyin_syllable_not_cross", func(t *testing.T) {
		cands := idx.collectCandidates(utils.BuildQueryContext("ceshi"))
		mids := candMids(idx, cands)
		if !hasMid(mids, 107) {
			t.Fatal("ceshi should recall 小夜测试")
		}
		hits := scoreMemoryIndex(idx, "ceshi", "", 0, 0)
		for _, h := range hits {
			if h.mid == 108 {
				t.Fatal("ceshi must not score-match 选择之她·他")
			}
		}
	})
	t.Run("subsequence", func(t *testing.T) {
		assertHas(t, "凡人传", 105)
	})
	t.Run("multi_token_intersects", func(t *testing.T) {
		qing := idx.collectCandidates(utils.BuildQueryContext("庆余年"))
		season2 := idx.collectCandidates(utils.BuildQueryContext("第二季"))
		both := idx.collectCandidates(utils.BuildQueryContext("庆余年 第二季"))
		if both == nil {
			t.Fatal("cross-token query full-scanned")
		}
		if len(both) == 0 {
			t.Fatal("cross-token query missed")
		}
		if len(both) > len(qing) || len(both) > len(season2) {
			t.Fatalf("intersect should shrink candidates, qing=%d season2=%d both=%d", len(qing), len(season2), len(both))
		}
		mids := candMids(idx, both)
		if !hasMid(mids, 101) {
			t.Fatal("expected 庆余年 第二季")
		}
		if hasMid(mids, 102) {
			t.Fatal("第一季 should be removed by intersect")
		}
		if hasMid(mids, 103) {
			t.Fatal("花絮 should be removed by intersect")
		}
	})
	t.Run("empty_query_not_full_scan", func(t *testing.T) {
		cands := idx.collectCandidates(utils.BuildQueryContext("   "))
		if cands == nil {
			t.Fatal("empty query must not full-scan")
		}
		if len(cands) != 0 {
			t.Fatalf("empty query candidates=%d", len(cands))
		}
	})
}

func TestScoreMemoryIndexPinyinAndPhrase(t *testing.T) {
	idx := testSearchIndex([]filmSearchIndexRow{
		{Mid: 104, Name: "流浪地球2", Hits: 100},
		{Mid: 101, Name: "庆余年 第二季", Hits: 90},
		{Mid: 106, Name: "星际穿越", Hits: 80},
		{Mid: 999, Name: "占位片", Hits: 1},
	})

	assertTop := func(keyword string, mid int64) {
		t.Helper()
		hits := scoreMemoryIndex(idx, keyword, "", 0, 0)
		if len(hits) == 0 {
			t.Fatalf("%q: no hits", keyword)
		}
		if hits[0].mid != mid {
			t.Fatalf("%q: top mid=%d want %d", keyword, hits[0].mid, mid)
		}
	}
	assertTop("lldq", 104)
	assertTop("qyn", 101)
	assertTop("qingyunian", 101)
}
