package hotchart

import (
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"strings"
	"sync"
	"time"

	filmsnapshot "server/internal/film/snapshot"
	"server/internal/infra/db"

	"golang.org/x/sync/singleflight"
)

const (
	hotChartsRawCacheKey     = "EcoHub:Home:HotCharts:Raw:v1"
	hotChartsLastGoodKey     = "EcoHub:Home:HotCharts:LastGood:v1"
	hotChartsRawTTL          = 6 * time.Hour
	hotChartsLastGoodTTL     = 7 * 24 * time.Hour
	hotChartsFetchCandidateN = 36
	hotChartsHTTPTimeout     = 8 * time.Second
	hotChartsFailCooldown    = 60 * time.Second
)

type rawHotChartEntry struct {
	DoubanID int64  `json:"doubanId"`
	Title    string `json:"title"`
	Year     int64  `json:"year"`
	Rate     string `json:"rate"`
}

type rawHotChartBucket struct {
	Key       string             `json:"key"`
	Title     string             `json:"title"`
	MediaKind string             `json:"mediaKind"`
	Entries   []rawHotChartEntry `json:"entries"`
}

type rawHotChartsBundle struct {
	Buckets   []rawHotChartBucket `json:"buckets"`
	UpdatedAt int64               `json:"updatedAt"`
}

// HotChartsService 管理豆瓣/Bangumi 原始榜单的按需拉取与缓存读取。
type HotChartsService struct {
	sf         singleflight.Group
	refreshMu  sync.Mutex
	lastFailAt time.Time
}

var HotChartsSvc = new(HotChartsService)

// GetRawCandidatesByKey 按榜单 key 返回外部候选条目及评分映射（供推荐自动同步与一键排片复用）。
func (s *HotChartsService) GetRawCandidatesByKey(key string, forceRefresh bool) ([]filmsnapshot.HotChartCandidate, map[int]string, error) {
	bundle, err := s.loadOrFetchRawBundle(forceRefresh)
	if err != nil {
		return nil, nil, err
	}
	key = strings.TrimSpace(key)
	for _, b := range bundle.Buckets {
		if b.Key == key {
			cands := make([]filmsnapshot.HotChartCandidate, 0, len(b.Entries))
			rates := make(map[int]string, len(b.Entries))
			for idx, e := range b.Entries {
				cands = append(cands, filmsnapshot.HotChartCandidate{
					DoubanID:  e.DoubanID,
					Title:     e.Title,
					Year:      e.Year,
					MediaKind: b.MediaKind,
				})
				rates[idx] = strings.TrimSpace(e.Rate)
			}
			return cands, rates, nil
		}
	}
	return nil, nil, fmt.Errorf("未找到榜单 %s", key)
}

// GetRawCandidatesByCategoryName 根据一级分类名称挑选对应的豆瓣/Bangumi 榜单候选列表（供轮播一键排片豆瓣推荐策略使用）。
func (s *HotChartsService) GetRawCandidatesByCategoryName(catName string) ([]filmsnapshot.HotChartCandidate, error) {
	bundle, err := s.loadOrFetchRawBundle(false)
	if err != nil {
		return nil, err
	}
	catName = strings.TrimSpace(catName)
	var keys []string
	var fallbackKind string
	switch {
	case strings.Contains(catName, "电影"):
		keys = []string{"movie_showing", "movie_hot"}
		fallbackKind = "movie"
	case strings.Contains(catName, "综艺"):
		keys = []string{"variety_hot"}
		fallbackKind = "variety"
	case strings.Contains(catName, "动漫") || strings.Contains(catName, "动画") || strings.Contains(catName, "番"):
		keys = []string{"bangumi_calendar"}
		fallbackKind = "anime"
	case strings.Contains(catName, "剧"):
		keys = []string{"tv_hot"}
		fallbackKind = "tv"
	default:
		keys = []string{"movie_showing", "movie_hot", "tv_hot", "variety_hot", "bangumi_calendar"}
	}

	bucketMap := make(map[string]rawHotChartBucket, len(bundle.Buckets))
	for _, b := range bundle.Buckets {
		bucketMap[b.Key] = b
	}

	out := make([]filmsnapshot.HotChartCandidate, 0, 64)
	seenTitle := make(map[string]struct{})
	for _, k := range keys {
		b, ok := bucketMap[k]
		if !ok {
			continue
		}
		mk := b.MediaKind
		if fallbackKind != "" {
			mk = fallbackKind
		}
		for _, e := range b.Entries {
			if _, dup := seenTitle[e.Title]; dup {
				continue
			}
			seenTitle[e.Title] = struct{}{}
			out = append(out, filmsnapshot.HotChartCandidate{
				DoubanID:  e.DoubanID,
				Title:     e.Title,
				Year:      e.Year,
				MediaKind: mk,
			})
		}
	}
	return out, nil
}

func (s *HotChartsService) loadOrFetchRawBundle(forceRefresh bool) (*rawHotChartsBundle, error) {
	if !forceRefresh && db.Rdb != nil {
		if raw, err := db.Rdb.Get(db.Cxt, hotChartsRawCacheKey).Result(); err == nil && raw != "" {
			var bundle rawHotChartsBundle
			if json.Unmarshal([]byte(raw), &bundle) == nil && len(bundle.Buckets) > 0 {
				return &bundle, nil
			}
		}
	}

	sfKey := fmt.Sprintf("%s:%t", hotChartsRawCacheKey, forceRefresh)
	val, err, _ := s.sf.Do(sfKey, func() (any, error) {
		if !forceRefresh {
			s.refreshMu.Lock()
			inFailCooldown := !s.lastFailAt.IsZero() && time.Since(s.lastFailAt) < hotChartsFailCooldown
			s.refreshMu.Unlock()
			if inFailCooldown {
				if fb := loadLastGoodHotChartsBundle(); fb != nil {
					return fb, nil
				}
				return nil, errors.New("外部榜单请求冷却中，请稍后重试")
			}
		}

		bundle, err := fetchAllHotChartBuckets()
		if err == nil && bundle != nil && len(bundle.Buckets) > 0 {
			s.refreshMu.Lock()
			s.lastFailAt = time.Time{}
			s.refreshMu.Unlock()
			if db.Rdb != nil {
				if data, mErr := json.Marshal(bundle); mErr == nil {
					_ = db.Rdb.Set(db.Cxt, hotChartsRawCacheKey, string(data), hotChartsRawTTL).Err()
					_ = db.Rdb.Set(db.Cxt, hotChartsLastGoodKey, string(data), hotChartsLastGoodTTL).Err()
				}
			}
			return bundle, nil
		}

		s.refreshMu.Lock()
		s.lastFailAt = time.Now()
		s.refreshMu.Unlock()

		if fallback := loadLastGoodHotChartsBundle(); fallback != nil {
			log.Printf("[HotCharts] 外部榜单拉取失败，已使用历史兜底缓存: %v", err)
			return fallback, nil
		}
		if err != nil {
			return nil, err
		}
		return nil, errors.New("暂无可用热门榜单")
	})
	if err != nil {
		return nil, err
	}
	return val.(*rawHotChartsBundle), nil
}

func loadLastGoodHotChartsBundle() *rawHotChartsBundle {
	if db.Rdb == nil {
		return nil
	}
	raw, err := db.Rdb.Get(db.Cxt, hotChartsLastGoodKey).Result()
	if err != nil || raw == "" {
		return nil
	}
	var fallback rawHotChartsBundle
	if json.Unmarshal([]byte(raw), &fallback) == nil && len(fallback.Buckets) > 0 {
		return &fallback
	}
	return nil
}
