package service

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"
	"sync"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	filmsnapshot "server/internal/repository/film/snapshot"
	"server/internal/utils"

	"golang.org/x/sync/singleflight"
)

const (
	hotChartsRawCacheKey     = "EcoHub:Home:HotCharts:Raw:v1"
	hotChartsLastGoodKey     = "EcoHub:Home:HotCharts:LastGood:v1"
	hotChartsRawTTL          = 6 * time.Hour
	hotChartsLastGoodTTL     = 7 * 24 * time.Hour
	hotChartsMatchedTTL      = 30 * time.Minute
	hotChartsPerTabLimit     = 12
	hotChartsFetchCandidateN = 36
	hotChartsHTTPTimeout     = 8 * time.Second
	hotChartsRefreshCooldown = 2 * time.Minute
	hotChartsPollInterval    = 6 * time.Hour
	hotChartsInitialDelay    = 15 * time.Second
)

// HotChartItem 单条榜单展示项（复用标准影片基础字段并附加榜单评分与排名）。
type HotChartItem struct {
	model.MovieBasicInfo
	DoubanScore string `json:"doubanScore,omitempty"`
	Rank        int    `json:"rank"`
}

// HotChartTab 单个榜单分组。
type HotChartTab struct {
	Key   string         `json:"key"`
	Title string         `json:"title"`
	Items []HotChartItem `json:"items"`
}

// HotChartsResponse 首页热门榜单响应。
type HotChartsResponse struct {
	Tabs      []HotChartTab `json:"tabs"`
	UpdatedAt int64         `json:"updatedAt"`
}

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

			if _, err := s.GetHotCharts(false); err != nil {
				log.Printf("[HotCharts] 启动预热失败: %v", err)
			}
			HomeRecommendSvc.SyncAllAutoGroups()

			ticker := time.NewTicker(hotChartsPollInterval)
			defer ticker.Stop()
			for range ticker.C {
				if _, err := s.GetHotCharts(true); err != nil {
					log.Printf("[HotCharts] 定时刷新失败: %v", err)
				}
				HomeRecommendSvc.SyncAllAutoGroups()
			}
		}()
	})
}

// GetRawCandidatesByKey 按榜单 key 返回外部候选条目及媒体类型（供推荐自动同步与一键排片复用）。
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

// GetHotCharts 获取反查本地片库后的热门榜单。
func (s *HotChartsService) GetHotCharts(forceRefresh bool) (*HotChartsResponse, error) {
	if err := filmsnapshot.WarmSearchMetaIndex(); err != nil {
		return nil, err
	}
	sourceID := ""
	if def := repository.CurrentPrimarySource(); def != nil {
		sourceID = strings.TrimSpace(def.Id)
	}
	if sourceID == "" {
		return &HotChartsResponse{Tabs: []HotChartTab{}, UpdatedAt: time.Now().Unix()}, nil
	}

	version := filmsnapshot.GetActiveReadModelVersion()
	matchedKey := fmt.Sprintf("EcoHub:Home:HotCharts:Matched:v%s:src_%s", version, sourceID)

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

	if !forceRefresh && db.Rdb != nil {
		if raw, err := db.Rdb.Get(db.Cxt, matchedKey).Result(); err == nil && raw != "" {
			var cached HotChartsResponse
			if json.Unmarshal([]byte(raw), &cached) == nil && len(cached.Tabs) > 0 {
				return &cached, nil
			}
		}
	}

	sfKey := fmt.Sprintf("%s:%t", matchedKey, forceRefresh)
	val, err, _ := s.sf.Do(sfKey, func() (any, error) {
		bundle, err := s.loadOrFetchRawBundle(forceRefresh)
		if err != nil {
			return nil, err
		}
		resp, err := s.buildMatchedResponse(version, sourceID, bundle)
		if err != nil {
			return nil, err
		}
		if db.Rdb != nil && len(resp.Tabs) > 0 {
			if data, err := json.Marshal(resp); err == nil {
				_ = db.Rdb.Set(db.Cxt, matchedKey, string(data), hotChartsMatchedTTL).Err()
			}
		}
		return resp, nil
	})
	if err != nil {
		return nil, err
	}
	return val.(*HotChartsResponse), nil
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

func (s *HotChartsService) buildMatchedResponse(version, sourceID string, bundle *rawHotChartsBundle) (*HotChartsResponse, error) {
	if bundle == nil || len(bundle.Buckets) == 0 {
		return &HotChartsResponse{Tabs: []HotChartTab{}, UpdatedAt: time.Now().Unix()}, nil
	}

	type matchedBucket struct {
		bucket       rawHotChartBucket
		mids         []int64
		midToCandIdx map[int64]int
	}
	results := make([]matchedBucket, len(bundle.Buckets))
	allMids := make([]int64, 0, len(bundle.Buckets)*hotChartsPerTabLimit)

	var wg sync.WaitGroup
	for idx, b := range bundle.Buckets {
		wg.Add(1)
		go func(i int, bucket rawHotChartBucket) {
			defer wg.Done()
			cands := make([]filmsnapshot.HotChartCandidate, 0, len(bucket.Entries))
			for _, e := range bucket.Entries {
				cands = append(cands, filmsnapshot.HotChartCandidate{
					DoubanID:  e.DoubanID,
					Title:     e.Title,
					Year:      e.Year,
					MediaKind: bucket.MediaKind,
				})
			}
			mids, midMap, err := filmsnapshot.MatchHotChartCandidates(version, sourceID, cands, hotChartsPerTabLimit)
			if err != nil {
				log.Printf("[HotCharts] 匹配本地片库失败 key=%s: %v", bucket.Key, err)
				return
			}
			results[i] = matchedBucket{bucket: bucket, mids: mids, midToCandIdx: midMap}
		}(idx, b)
	}
	wg.Wait()

	for _, r := range results {
		allMids = append(allMids, r.mids...)
	}
	byID, err := infosByMid(version, allMids)
	if err != nil {
		return nil, err
	}

	tabs := make([]HotChartTab, 0, len(results))
	for _, r := range results {
		if len(r.mids) == 0 {
			continue
		}
		items := make([]HotChartItem, 0, len(r.mids))
		rank := 0
		for _, mid := range r.mids {
			info, ok := byID[mid]
			if !ok {
				continue
			}
			rank++
			rate := ""
			if cIdx, exists := r.midToCandIdx[mid]; exists && cIdx >= 0 && cIdx < len(r.bucket.Entries) {
				rate = strings.TrimSpace(r.bucket.Entries[cIdx].Rate)
			}
			items = append(items, HotChartItem{
				MovieBasicInfo: info,
				DoubanScore:    rate,
				Rank:           rank,
			})
		}
		if len(items) > 0 {
			tabs = append(tabs, HotChartTab{
				Key:   r.bucket.Key,
				Title: r.bucket.Title,
				Items: items,
			})
		}
	}

	return &HotChartsResponse{
		Tabs:      tabs,
		UpdatedAt: bundle.UpdatedAt,
	}, nil
}

func newHotChartsHTTPClient() *http.Client {
	transport := utils.GetOrCreateProxyTransport("")
	if ok, proxyURL, err := repository.ResolveTMDBProxy(); err == nil && ok && proxyURL != "" {
		transport = utils.GetOrCreateProxyTransport(proxyURL)
	}
	return &http.Client{
		Transport: transport,
		Timeout:   hotChartsHTTPTimeout,
	}
}

func fetchJSONBytes(client *http.Client, targetURL, referer string) ([]byte, error) {
	ctx, cancel := context.WithTimeout(context.Background(), hotChartsHTTPTimeout)
	defer cancel()

	req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
	if err != nil {
		return nil, err
	}
	req.Header.Set("User-Agent", "Mozilla/5.0 (Windows NT 10.0; Win64; x64) AppleWebKit/537.36 (KHTML, like Gecko) Chrome/128.0.0.0 Safari/537.36")
	req.Header.Set("Accept", "application/json, text/plain, */*")
	if referer != "" {
		req.Header.Set("Referer", referer)
	}
	resp, err := client.Do(req)
	if err != nil {
		return nil, err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("HTTP %d", resp.StatusCode)
	}
	return io.ReadAll(io.LimitReader(resp.Body, 4<<20))
}

func fetchAllHotChartBuckets() (*rawHotChartsBundle, error) {
	client := newHotChartsHTTPClient()
	type chartSpec struct {
		key       string
		title     string
		mediaKind string
		fetcher   func(*http.Client) ([]rawHotChartEntry, error)
	}

	specs := []chartSpec{
		{
			key:       "movie_showing",
			title:     "院线热映",
			mediaKind: "movie",
			fetcher:   func(c *http.Client) ([]rawHotChartEntry, error) { return fetchDoubanRexxarCollection(c, "movie_showing") },
		},
		{
			key:       "movie_hot",
			title:     "热门电影",
			mediaKind: "movie",
			fetcher:   func(c *http.Client) ([]rawHotChartEntry, error) { return fetchDoubanRexxarCollection(c, "movie_hot_gaia") },
		},
		{
			key:       "tv_hot",
			title:     "热门剧集",
			mediaKind: "tv",
			fetcher:   func(c *http.Client) ([]rawHotChartEntry, error) { return fetchDoubanRexxarCollection(c, "tv_hot") },
		},
		{
			key:       "variety_hot",
			title:     "热门综艺",
			mediaKind: "variety",
			fetcher:   func(c *http.Client) ([]rawHotChartEntry, error) { return fetchDoubanRexxarCollection(c, "tv_variety_show") },
		},
		{
			key:       "bangumi_calendar",
			title:     "当季新番",
			mediaKind: "anime",
			fetcher:   fetchAnimeChartWithFallback,
		},
	}

	buckets := make([]rawHotChartBucket, len(specs))
	var wg sync.WaitGroup
	for i, sp := range specs {
		wg.Add(1)
		go func(idx int, spec chartSpec) {
			defer wg.Done()
			entries, err := spec.fetcher(client)
			if err != nil {
				log.Printf("[HotCharts] 拉取榜单失败 key=%s: %v", spec.key, err)
				return
			}
			buckets[idx] = rawHotChartBucket{
				Key:       spec.key,
				Title:     spec.title,
				MediaKind: spec.mediaKind,
				Entries:   entries,
			}
		}(i, sp)
	}
	wg.Wait()

	nonEmpty := make([]rawHotChartBucket, 0, len(buckets))
	for _, b := range buckets {
		if len(b.Entries) > 0 {
			nonEmpty = append(nonEmpty, b)
		}
	}
	if len(nonEmpty) == 0 {
		return nil, errors.New("所有外部热门榜单拉取失败")
	}
	return &rawHotChartsBundle{
		Buckets:   nonEmpty,
		UpdatedAt: time.Now().Unix(),
	}, nil
}

func fetchDoubanRexxarCollection(client *http.Client, collection string) ([]rawHotChartEntry, error) {
	targetURL := fmt.Sprintf("https://m.douban.com/rexxar/api/v2/subject_collection/%s/items?start=0&count=%d", collection, hotChartsFetchCandidateN)
	body, err := fetchJSONBytes(client, targetURL, "https://m.douban.com/movie")
	if err != nil {
		return nil, err
	}
	return parseDoubanRexxarShowing(body)
}

func fetchAnimeChartWithFallback(client *http.Client) ([]rawHotChartEntry, error) {
	if entries, err := fetchBangumiCalendar(client); err == nil && len(entries) > 0 {
		return entries, nil
	}
	return fetchDoubanRexxarCollection(client, "tv_animation")
}

func parseDoubanRexxarShowing(body []byte) ([]rawHotChartEntry, error) {
	var payload struct {
		Items []struct {
			ID     string `json:"id"`
			Title  string `json:"title"`
			Year   string `json:"year"`
			Rating struct {
				Value float64 `json:"value"`
			} `json:"rating"`
		} `json:"subject_collection_items"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	out := make([]rawHotChartEntry, 0, len(payload.Items))
	for _, it := range payload.Items {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		dbID, _ := strconv.ParseInt(strings.TrimSpace(it.ID), 10, 64)
		yr, _ := strconv.ParseInt(strings.TrimSpace(it.Year), 10, 64)
		rate := ""
		if it.Rating.Value > 0 {
			rate = fmt.Sprintf("%.1f", it.Rating.Value)
		}
		out = append(out, rawHotChartEntry{
			DoubanID: dbID,
			Title:    title,
			Year:     yr,
			Rate:     rate,
		})
	}
	return out, nil
}

func fetchDoubanSearchSubjects(client *http.Client, kind, tag string) ([]rawHotChartEntry, error) {
	url := fmt.Sprintf("https://movie.douban.com/j/search_subjects?type=%s&tag=%s&sort=recommend&page_limit=%d&page_start=0", kind, tag, hotChartsFetchCandidateN)
	body, err := fetchJSONBytes(client, url, "https://movie.douban.com/")
	if err != nil {
		return nil, err
	}
	return parseDoubanSearchSubjects(body)
}

func parseDoubanSearchSubjects(body []byte) ([]rawHotChartEntry, error) {
	var payload struct {
		Subjects []struct {
			ID    string `json:"id"`
			Title string `json:"title"`
			Rate  string `json:"rate"`
		} `json:"subjects"`
	}
	if err := json.Unmarshal(body, &payload); err != nil {
		return nil, err
	}
	out := make([]rawHotChartEntry, 0, len(payload.Subjects))
	for _, it := range payload.Subjects {
		title := strings.TrimSpace(it.Title)
		if title == "" {
			continue
		}
		dbID, _ := strconv.ParseInt(strings.TrimSpace(it.ID), 10, 64)
		out = append(out, rawHotChartEntry{
			DoubanID: dbID,
			Title:    title,
			Rate:     strings.TrimSpace(it.Rate),
		})
	}
	return out, nil
}

func fetchBangumiCalendar(client *http.Client) ([]rawHotChartEntry, error) {
	body, err := fetchJSONBytes(client, "https://api.bgm.tv/calendar", "https://bgm.tv/")
	if err != nil {
		return nil, err
	}
	return parseBangumiCalendar(body)
}

func parseBangumiCalendar(body []byte) ([]rawHotChartEntry, error) {
	var days []struct {
		Items []struct {
			Name       string `json:"name"`
			NameCN     string `json:"name_cn"`
			AirDate    string `json:"air_date"`
			Rating     struct {
				Score float64 `json:"score"`
			} `json:"rating"`
			Collection struct {
				Doing int `json:"doing"`
			} `json:"collection"`
		} `json:"items"`
	}
	if err := json.Unmarshal(body, &days); err != nil {
		return nil, err
	}

	type bgmCandidate struct {
		title string
		year  int64
		rate  string
		doing int
		score float64
	}
	seen := make(map[string]struct{})
	cands := make([]bgmCandidate, 0, 64)
	for _, d := range days {
		for _, it := range d.Items {
			title := strings.TrimSpace(it.NameCN)
			if title == "" {
				title = strings.TrimSpace(it.Name)
			}
			if title == "" {
				continue
			}
			if _, ok := seen[title]; ok {
				continue
			}
			seen[title] = struct{}{}
			var yr int64
			if len(it.AirDate) >= 4 {
				yr, _ = strconv.ParseInt(it.AirDate[:4], 10, 64)
			}
			rate := ""
			if it.Rating.Score > 0 {
				rate = fmt.Sprintf("%.1f", it.Rating.Score)
			}
			cands = append(cands, bgmCandidate{
				title: title,
				year:  yr,
				rate:  rate,
				doing: it.Collection.Doing,
				score: it.Rating.Score,
			})
		}
	}

	sort.SliceStable(cands, func(i, j int) bool {
		if cands[i].doing != cands[j].doing {
			return cands[i].doing > cands[j].doing
		}
		return cands[i].score > cands[j].score
	})

	limit := hotChartsFetchCandidateN
	if len(cands) < limit {
		limit = len(cands)
	}
	out := make([]rawHotChartEntry, 0, limit)
	for _, c := range cands[:limit] {
		out = append(out, rawHotChartEntry{
			Title: c.title,
			Year:  c.year,
			Rate:  c.rate,
		})
	}
	return out, nil
}
