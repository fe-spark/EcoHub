package utils

import (
	"context"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"math"
	"net/http"
	"net/url"
	"os"
	"strconv"
	"strings"
	"time"

	"golang.org/x/time/rate"
)

var (
	ErrNoTmdbApiKey  = errors.New("未配置 TMDB API Key")
	ErrTmdbNotFound  = errors.New("TMDB 未检索到匹配条目")
	ErrTmdbMismatch  = errors.New("TMDB 检索结果与片名年份不匹配")
	ErrTmdbRateLimit = errors.New("TMDB 请求频次超限")
)

const (
	defaultTmdbBaseURL = "https://api.themoviedb.org/3"
	tmdbPosterBase     = "https://image.tmdb.org/t/p/w500"
	tmdbBackdropBase   = "https://image.tmdb.org/t/p/w1280"
)

// TmdbMediaDetail 统一归一化的 TMDB 影视元数据
type TmdbMediaDetail struct {
	TmdbID       int64    `json:"tmdb_id"`
	MediaType    string   `json:"media_type"` // "movie" | "tv"
	Title        string   `json:"title"`      // 中文优先，英文回退
	OriginalName string   `json:"original_name"`
	Overview     string   `json:"overview"`
	Year         int64    `json:"year"`
	ReleaseDate  string   `json:"release_date"` // YYYY-MM-DD
	PosterURL    string   `json:"poster_url"`
	BackdropURL  string   `json:"backdrop_url"`
	Rating       float64  `json:"rating"` // 保留一位小数
	Director     string   `json:"director"`
	Actors       string   `json:"actors"`
	GenreIDs     []int64  `json:"genre_ids"`
	Genres       []string `json:"genres"`
}

type TmdbClient struct {
	apiKey     string
	baseURL    string
	httpClient *http.Client
	limiter    *rate.Limiter
}

// NewTmdbClient 创建 TMDB 客户端，优先使用传入参数，未提供时读取系统环境变量
func NewTmdbClient(apiKey, baseURL string) (*TmdbClient, error) {
	return NewTmdbClientWithHTTPClient(apiKey, baseURL, nil)
}

func NewTmdbClientWithHTTPClient(apiKey, baseURL string, httpClient *http.Client) (*TmdbClient, error) {
	resolvedKey := resolveTmdbApiKey(apiKey)
	if resolvedKey == "" {
		return nil, ErrNoTmdbApiKey
	}

	resolvedBaseURL := resolveTmdbBaseURL(baseURL)

	if httpClient == nil {
		httpClient = &http.Client{
			Timeout: 10 * time.Second,
		}
	}

	return &TmdbClient{
		apiKey:     resolvedKey,
		baseURL:    resolvedBaseURL,
		httpClient: httpClient,
		limiter:    rate.NewLimiter(rate.Limit(4), 4), // 4 QPS, burst 4
	}, nil
}

func resolveTmdbApiKey(sourceKey string) string {
	if k := strings.TrimSpace(sourceKey); k != "" {
		return k
	}
	return strings.TrimSpace(os.Getenv("TMDB_API_KEY"))
}

func resolveTmdbBaseURL(sourceURL string) string {
	raw := strings.TrimSpace(sourceURL)
	if raw == "" {
		raw = strings.TrimSpace(os.Getenv("TMDB_BASE_URL"))
	}
	if raw == "" {
		return defaultTmdbBaseURL
	}
	raw = strings.TrimRight(raw, "/")
	if !strings.HasSuffix(raw, "/3") {
		raw = raw + "/3"
	}
	return raw
}

// SearchAndGetDetail 搜索并拉取影视详情（中文优先，必要时回退英文补全）
func (c *TmdbClient) SearchAndGetDetail(ctx context.Context, mediaType, title string, year int64) (*TmdbMediaDetail, error) {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if mediaType != "movie" && mediaType != "tv" {
		return nil, fmt.Errorf("不支持的媒体类型: %s", mediaType)
	}

	tmdbID, err := c.searchTopID(ctx, mediaType, title, year)
	if err != nil {
		return nil, err
	}

	return c.GetDetailByID(ctx, mediaType, tmdbID)
}

// GetDetailByID 直接根据 TMDB ID 获取详情（用于手工绑定场景，跳过 search）
func (c *TmdbClient) GetDetailByID(ctx context.Context, mediaType string, tmdbID int64) (*TmdbMediaDetail, error) {
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if mediaType != "movie" && mediaType != "tv" {
		return nil, fmt.Errorf("不支持的媒体类型: %s", mediaType)
	}

	// 1. 优先拉取中文详情
	zhDetail, err := c.fetchDetail(ctx, mediaType, tmdbID, "zh-CN")
	if err != nil {
		return nil, err
	}

	// 2. 若标题或简介为空，拉取英文详情回退补全
	if zhDetail.Title == "" || zhDetail.Overview == "" {
		enDetail, err := c.fetchDetail(ctx, mediaType, tmdbID, "en-US")
		if err == nil && enDetail != nil {
			if zhDetail.Title == "" {
				zhDetail.Title = enDetail.Title
			}
			if zhDetail.Overview == "" {
				zhDetail.Overview = enDetail.Overview
			}
			if zhDetail.Director == "" {
				zhDetail.Director = enDetail.Director
			}
			if zhDetail.Actors == "" {
				zhDetail.Actors = enDetail.Actors
			}
		}
	}

	return zhDetail, nil
}

// searchTopID 执行检索并获取置顶结果的 ID（带片名归一化与年份容错校验）
func (c *TmdbClient) searchTopID(ctx context.Context, mediaType, queryTitle string, year int64) (int64, error) {
	endpoint := fmt.Sprintf("%s/search/%s", c.baseURL, mediaType)
	params := url.Values{}
	params.Set("query", queryTitle)
	params.Set("language", "zh-CN")
	params.Set("page", "1")
	if year > 0 {
		if mediaType == "movie" {
			params.Set("year", strconv.FormatInt(year, 10))
		} else {
			params.Set("first_air_date_year", strconv.FormatInt(year, 10))
		}
	}

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())
	respBytes, err := c.doRequestWithRetry(ctx, fullURL)
	if err != nil {
		return 0, err
	}

	var searchResp struct {
		Results []struct {
			ID           int64   `json:"id"`
			Title        string  `json:"title"`
			Name         string  `json:"name"`
			OriginalName string  `json:"original_name"`
			ReleaseDate  string  `json:"release_date"`
			FirstAirDate string  `json:"first_air_date"`
			VoteAverage  float64 `json:"vote_average"`
		} `json:"results"`
	}

	if err := json.Unmarshal(respBytes, &searchResp); err != nil {
		return 0, fmt.Errorf("解析 TMDB 搜索响应失败: %w", err)
	}

	if len(searchResp.Results) == 0 {
		// 如果带年份搜索未找到且指定了年份，尝试去掉年份兜底搜索一次
		if year > 0 {
			return c.searchTopID(ctx, mediaType, queryTitle, 0)
		}
		return 0, ErrTmdbNotFound
	}

	top := searchResp.Results[0]
	topTitle := top.Title
	if topTitle == "" {
		topTitle = top.Name
	}
	topDate := top.ReleaseDate
	if topDate == "" {
		topDate = top.FirstAirDate
	}

	// 容错校验：若片名完全不相关且年份相差大于 1 年，判定为不匹配
	if year > 0 && len(topDate) >= 4 {
		if topYear, err := strconv.ParseInt(topDate[:4], 10, 64); err == nil {
			if math.Abs(float64(year-topYear)) > 1 && !isTitleRelated(queryTitle, topTitle) {
				return 0, ErrTmdbMismatch
			}
		}
	}

	return top.ID, nil
}

func isTitleRelated(a, b string) bool {
	cleanA := strings.ToLower(strings.TrimSpace(a))
	cleanB := strings.ToLower(strings.TrimSpace(b))
	if cleanA == "" || cleanB == "" {
		return false
	}
	return strings.Contains(cleanA, cleanB) || strings.Contains(cleanB, cleanA)
}

func (c *TmdbClient) fetchDetail(ctx context.Context, mediaType string, tmdbID int64, lang string) (*TmdbMediaDetail, error) {
	endpoint := fmt.Sprintf("%s/%s/%d", c.baseURL, mediaType, tmdbID)
	params := url.Values{}
	params.Set("language", lang)
	params.Set("append_to_response", "credits")

	fullURL := fmt.Sprintf("%s?%s", endpoint, params.Encode())
	respBytes, err := c.doRequestWithRetry(ctx, fullURL)
	if err != nil {
		return nil, err
	}

	var raw struct {
		ID            int64   `json:"id"`
		Title         string  `json:"title"`
		Name          string  `json:"name"`
		OriginalTitle string  `json:"original_title"`
		OriginalName  string  `json:"original_name"`
		Overview      string  `json:"overview"`
		ReleaseDate   string  `json:"release_date"`
		FirstAirDate  string  `json:"first_air_date"`
		PosterPath   string  `json:"poster_path"`
		BackdropPath string  `json:"backdrop_path"`
		VoteAverage  float64 `json:"vote_average"`
		Genres       []struct {
			ID   int64  `json:"id"`
			Name string `json:"name"`
		} `json:"genres"`
		Credits struct {
			Cast []struct {
				Name         string `json:"name"`
				OriginalName string `json:"original_name"`
			} `json:"cast"`
			Crew []struct {
				Name         string `json:"name"`
				OriginalName string `json:"original_name"`
				Job          string `json:"job"`
			} `json:"crew"`
		} `json:"credits"`
	}

	if err := json.Unmarshal(respBytes, &raw); err != nil {
		return nil, fmt.Errorf("解析 TMDB 详情响应失败: %w", err)
	}

	title := raw.Title
	if title == "" {
		title = raw.Name
	}
	releaseDate := raw.ReleaseDate
	if releaseDate == "" {
		releaseDate = raw.FirstAirDate
	}
	var year int64
	if len(releaseDate) >= 4 {
		year, _ = strconv.ParseInt(releaseDate[:4], 10, 64)
	}

	var posterURL, backdropURL string
	if raw.PosterPath != "" {
		posterURL = tmdbPosterBase + raw.PosterPath
	}
	if raw.BackdropPath != "" {
		backdropURL = tmdbBackdropBase + raw.BackdropPath
	}

	// 演职员提取
	actorsList := make([]string, 0, 5)
	for i, cast := range raw.Credits.Cast {
		if i >= 5 {
			break
		}
		actorName := strings.TrimSpace(cast.Name)
		if actorName == "" {
			actorName = strings.TrimSpace(cast.OriginalName)
		}
		if actorName != "" {
			actorsList = append(actorsList, actorName)
		}
	}

	directorList := make([]string, 0, 2)
	for _, crew := range raw.Credits.Crew {
		if strings.EqualFold(crew.Job, "Director") {
			directorName := strings.TrimSpace(crew.Name)
			if directorName == "" {
				directorName = strings.TrimSpace(crew.OriginalName)
			}
			if directorName != "" {
				directorList = append(directorList, directorName)
			}
		}
	}

	genreIDs := make([]int64, len(raw.Genres))
	genres := make([]string, len(raw.Genres))
	for i, g := range raw.Genres {
		genreIDs[i] = g.ID
		genres[i] = g.Name
	}

	origName := raw.OriginalTitle
	if origName == "" {
		origName = raw.OriginalName
	}

	return &TmdbMediaDetail{
		TmdbID:       raw.ID,
		MediaType:    mediaType,
		Title:        title,
		OriginalName: origName,
		Overview:     raw.Overview,
		Year:         year,
		ReleaseDate:  releaseDate,
		PosterURL:    posterURL,
		BackdropURL:  backdropURL,
		Rating:       math.Round(raw.VoteAverage*10) / 10,
		Director:     strings.Join(directorList, "、"),
		Actors:       strings.Join(actorsList, "、"),
		GenreIDs:     genreIDs,
		Genres:       genres,
	}, nil
}

func (c *TmdbClient) doRequestWithRetry(ctx context.Context, targetURL string) ([]byte, error) {
	const maxRetries = 3

	for attempt := 0; attempt <= maxRetries; attempt++ {
		if err := c.limiter.Wait(ctx); err != nil {
			return nil, err
		}

		req, err := http.NewRequestWithContext(ctx, http.MethodGet, targetURL, nil)
		if err != nil {
			return nil, err
		}

		req.Header.Set("Authorization", "Bearer "+c.apiKey)
		req.Header.Set("Accept", "application/json")

		resp, err := c.httpClient.Do(req)
		if err != nil {
			if ctx.Err() != nil {
				return nil, ctx.Err()
			}
			if attempt == maxRetries {
				return nil, fmt.Errorf("请求 TMDB 接口失败: %w", err)
			}
			time.Sleep(time.Duration(attempt+1) * 200 * time.Millisecond)
			continue
		}

		defer resp.Body.Close()

		if resp.StatusCode == http.StatusTooManyRequests {
			if attempt == maxRetries {
				return nil, ErrTmdbRateLimit
			}
			retryAfterSec := 1
			if h := resp.Header.Get("Retry-After"); h != "" {
				if s, err := strconv.Atoi(h); err == nil && s > 0 {
					retryAfterSec = s
				}
			}
			if retryAfterSec > 5 {
				retryAfterSec = 5
			}
			select {
			case <-ctx.Done():
				return nil, ctx.Err()
			case <-time.After(time.Duration(retryAfterSec) * time.Second):
				continue
			}
		}

		if resp.StatusCode == http.StatusNotFound {
			return nil, ErrTmdbNotFound
		}

		body, err := io.ReadAll(resp.Body)
		if err != nil {
			return nil, fmt.Errorf("读取 TMDB 响应失败: %w", err)
		}

		if resp.StatusCode < 200 || resp.StatusCode >= 300 {
			return nil, fmt.Errorf("TMDB 接口返回错误码 %d: %s", resp.StatusCode, string(body))
		}

		return body, nil
	}

	return nil, ErrTmdbRateLimit
}
