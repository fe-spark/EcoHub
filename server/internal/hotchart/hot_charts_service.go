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
	hotChartsRefreshCooldown = 2 * time.Minute
	hotChartsPollInterval    = 6 * time.Hour
	hotChartsInitialDelay    = 15 * time.Second
)

// OnRefreshed 在榜单预热或定时刷新完成后由外部域（如首页推荐自动同步）挂载回调。
var OnRefreshed func()

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

// HotChartsService 管理豆瓣/Bangumi 原始榜单的定时拉取与缓存读取。
type HotChartsService struct {
	sf          singleflight.Group
	pollerOnce  sync.Once
	refreshMu   sync.Mutex
	lastRefresh time.Time
}

var HotChartsSvc = new(HotChartsService)

// StartPoller 启动后台定时预热与刷新（启动 15 秒后首次执行，之后每 6 小时刷新）。
func (s *HotChartsService) StartPoller() {
	s.pollerOnce.Do(func() {
		go func() {
			timer := time.NewTimer(hotChartsInitialDelay)
			defer timer.Stop()
			<-timer.C

			if err := s.warmRawBundle(false); err != nil {
				log.Printf("[HotCharts] 启动预热失败: %v", err)
			}
			if OnRefreshed != nil {
				OnRefreshed()
			}

			ticker := time.NewTicker(hotChartsPollInterval)
			defer ticker.Stop()
			for range ticker.C {
				if err := s.warmRawBundle(true); err != nil {
					log.Printf("[HotCharts] 定时刷新失败: %v", err)
				}
				if OnRefreshed != nil {
					OnRefreshed()
				}
			}
		}()
	})
}

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

func (s *HotChartsService) warmRawBundle(forceRefresh bool) error {
	if forceRefresh {
		s.refreshMu.Lock()
		allowRefresh := time.Since(s.lastRefresh) >= hotChartsRefreshCooldown
		if allowRefresh {
			s.lastRefresh = time.Now()
		}
		s.refreshMu.Unlock()
		if !allowRefresh {
			forceRefresh = false
		}
	}
	sfKey := fmt.Sprintf("%s:%t", hotChartsRawCacheKey, forceRefresh)
	_, err, _ := s.sf.Do(sfKey, func() (any, error) {
		return s.loadOrFetchRawBundle(forceRefresh)
	})
	return err
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

	bundle, err := fetchAllHotChartBuckets()
	if err == nil && bundle != nil && len(bundle.Buckets) > 0 {
		if db.Rdb != nil {
			if data, mErr := json.Marshal(bundle); mErr == nil {
				_ = db.Rdb.Set(db.Cxt, hotChartsRawCacheKey, string(data), hotChartsRawTTL).Err()
				_ = db.Rdb.Set(db.Cxt, hotChartsLastGoodKey, string(data), hotChartsLastGoodTTL).Err()
			}
		}
		return bundle, nil
	}

	// 兜底：若本次拉取失败，回退上一次成功缓存
	if db.Rdb != nil {
		if raw, lErr := db.Rdb.Get(db.Cxt, hotChartsLastGoodKey).Result(); lErr == nil && raw != "" {
			var fallback rawHotChartsBundle
			if json.Unmarshal([]byte(raw), &fallback) == nil && len(fallback.Buckets) > 0 {
				log.Printf("[HotCharts] 外部榜单拉取失败，已使用历史兜底缓存: %v", err)
				return &fallback, nil
			}
		}
	}
	if err != nil {
		return nil, err
	}
	return nil, errors.New("暂无可用热门榜单")
}
