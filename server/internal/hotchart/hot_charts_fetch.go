package hotchart

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

	"server/internal/repobase"
	"server/internal/utils"
)

func newHotChartsHTTPClient() *http.Client {
	transport := utils.GetOrCreateProxyTransport("")
	if ok, proxyURL, err := repobase.ResolveTMDBProxy(); err == nil && ok && proxyURL != "" {
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
			Name    string `json:"name"`
			NameCN  string `json:"name_cn"`
			AirDate string `json:"air_date"`
			Rating  struct {
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
