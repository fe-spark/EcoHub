package hotchart

import (
	"testing"
	"time"
)

func TestMergeMissingBuckets_PreservesLastGoodWithoutLoss(t *testing.T) {
	partial := &rawHotChartsBundle{
		UpdatedAt: time.Now().Unix(),
		Buckets: []rawHotChartBucket{
			{
				Key:       "movie_showing",
				Title:     "院线热映",
				MediaKind: "movie",
				Entries:   []rawHotChartEntry{{Title: "新片A"}},
			},
		},
	}
	lastGood := &rawHotChartsBundle{
		UpdatedAt: time.Now().Add(-time.Hour).Unix(),
		Buckets: []rawHotChartBucket{
			{
				Key:       "movie_showing",
				Title:     "院线热映",
				MediaKind: "movie",
				Entries:   []rawHotChartEntry{{Title: "旧片A"}},
			},
			{
				Key:       "tv_hot",
				Title:     "热门剧集",
				MediaKind: "tv",
				Entries:   []rawHotChartEntry{{Title: "兜底剧B"}},
			},
		},
	}

	merged := mergeMissingBuckets(partial, lastGood)
	byKey := map[string]rawHotChartBucket{}
	for _, b := range merged.Buckets {
		byKey[b.Key] = b
	}
	if len(byKey["movie_showing"].Entries) != 1 || byKey["movie_showing"].Entries[0].Title != "新片A" {
		t.Fatalf("应保留本次成功拉取的桶，实际=%+v", byKey["movie_showing"])
	}
	if len(byKey["tv_hot"].Entries) != 1 || byKey["tv_hot"].Entries[0].Title != "兜底剧B" {
		t.Fatalf("缺失桶应从 LastGood 补齐，实际=%+v", byKey["tv_hot"])
	}
}

func TestHotChartsService_MemoryCacheFallbackWhenRedisNil(t *testing.T) {
	svc := &HotChartsService{}
	bundle := &rawHotChartsBundle{
		UpdatedAt: time.Now().Unix(),
		Buckets: []rawHotChartBucket{
			{
				Key:       "movie_showing",
				Title:     "院线热映",
				MediaKind: "movie",
				Entries:   []rawHotChartEntry{{Title: "内存缓存影片"}},
			},
		},
	}
	svc.storeMemBundle(bundle, time.Minute, true)

	got := svc.loadMemBundle()
	if got == nil || len(got.Buckets) != 1 || got.Buckets[0].Entries[0].Title != "内存缓存影片" {
		t.Fatalf("Redis 不可用时内存缓存应可读，实际=%+v", got)
	}
	lg := svc.loadLastGoodBundle()
	if lg == nil || len(lg.Buckets) != 1 || lg.Buckets[0].Entries[0].Title != "内存缓存影片" {
		t.Fatalf("完整拉取应同步写入内存 LastGood，实际=%+v", lg)
	}

	// 部分拉取不应覆盖内存 LastGood
	partial := &rawHotChartsBundle{
		UpdatedAt: time.Now().Unix(),
		Buckets: []rawHotChartBucket{
			{
				Key:       "movie_showing",
				Title:     "院线热映",
				MediaKind: "movie",
				Entries:   []rawHotChartEntry{{Title: "部分拉取影片"}},
			},
		},
	}
	svc.storeMemBundle(partial, time.Minute, false)
	lgAfter := svc.loadLastGoodBundle()
	if lgAfter == nil || lgAfter.Buckets[0].Entries[0].Title != "内存缓存影片" {
		t.Fatalf("部分拉取不得覆盖 LastGood，实际=%+v", lgAfter)
	}
}
