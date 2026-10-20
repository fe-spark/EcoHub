package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"server/internal/model"
	"server/internal/repository"
	"server/internal/utils"
)

type TMDBService struct{}

var TMDBSvc = new(TMDBService)

const (
	httpTimeout = 8 * time.Second
)

var (
	tmdbAPIBaseURL = "https://api.themoviedb.org/3"
)

var (
	// 清洗影视标题中常见的噪音标签
	noiseRegexes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\[.*?\]`),
		regexp.MustCompile(`(?i)【.*?】`),
		regexp.MustCompile(`(?i)[（(]?\b(4k|1080p|720p|2160p|hd|bd|tc|ts|dvd|web-dl|bluray|uhd|remux|h264|h265|x264|x265|hevc|aac)\b[）)]?`),
		regexp.MustCompile(`(?i)[0-9]+(帧|fps)`),
		regexp.MustCompile(`(?i)[-_—·\s\x{3000}]*(预告片|预告|先导片|先导预告|片花|花絮|特辑|彩蛋|幕后|采访|纯享版|精编版|精华版|速看版|解说版|正片)`),
		regexp.MustCompile(`(?i)[-_—·\s\x{3000}]+(trailer|teaser|pv|preview|sp)\b`),
		regexp.MustCompile(`(?i)[-_—·\s\x{3000}]*(普通话版|普通话|国粤双语|国语版|国语|粤语版|粤语|英语版|原声版|中配版|中配|配音版|译制版|重制版|重置版|真人版|剧场版|电影版|特别篇)`),
		regexp.MustCompile(`(?i)[-_—·\s\x{3000}]*(动态漫画|动态漫|有声漫|短剧|微短剧|竖屏剧)`),
		regexp.MustCompile(`(?i)(中文字幕|中英双字|双语字幕|抢先版|枪版|高清版|超清版|蓝光版|完整版|未删减版|无删减版|未删减|无删减|修复版|加长版|导演剪辑版|公映版)`),
		regexp.MustCompile(`(?i)(第[0-9一二三四五六七八九十]+[集期话]|更新至[0-9]+[集期话]|全[0-9]+[集期话]|完结|连载中|(Episode|EP)[0-9]+)`),
	}
	yearRegex          = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
	bracketYearRegex   = regexp.MustCompile(`[（(]\s*(19\d{2}|20\d{2})\s*[）)]`)
	emptyBracketsRegex = regexp.MustCompile(`[（(]\s*[）)]|[【\[]\s*[】\]]`)
)

// CleanKeywordForSearch 清理搜索关键词中的格式、清晰度与集数杂质
func CleanKeywordForSearch(raw string) (cleaned string, extractedYear string) {
	s := raw
	for _, reg := range noiseRegexes {
		s = reg.ReplaceAllString(s, " ")
	}

	// 提取可能带有的 4 位年份
	if match := yearRegex.FindString(s); match != "" {
		extractedYear = match
		s = yearRegex.ReplaceAllString(s, " ")
	}

	// 清理残留的空括号
	s = emptyBracketsRegex.ReplaceAllString(s, " ")

	// 清洗标点与多余空格（包括半角和全角中文空格）
	s = strings.Trim(s, " \t\r\n\u3000-_·—:：.~～〜!！?？")
	s = regexp.MustCompile(`[\s\x{3000}]+`).ReplaceAllString(s, " ")
	return s, extractedYear
}

func getHTTPClient() *http.Client {
	var transport *http.Transport
	if ok, proxyURL := repository.ResolveTMDBProxy(); ok && proxyURL != "" {
		transport = utils.GetOrCreateProxyTransport(proxyURL)
	} else {
		transport = utils.GetOrCreateProxyTransport("")
	}
	return &http.Client{
		Transport: transport,
		Timeout:   httpTimeout,
	}
}

// GetConfig 获取脱敏配置
func (s *TMDBService) GetConfig() model.TMDBConfig {
	return repository.PublicTMDBConfig(repository.GetTMDBConfig())
}

func tmdbScrapeReady(cfg model.TMDBConfig) bool {
	return cfg.Enabled && strings.TrimSpace(cfg.ApiKey) != ""
}

// UpdateConfig 更新 TMDB 配置
func (s *TMDBService) UpdateConfig(cfg model.TMDBConfig) error {
	existing := repository.GetTMDBConfig()
	if repository.IsMaskedTMDBApiKey(cfg.ApiKey) {
		cfg.ApiKey = existing.ApiKey
	}
	if err := repository.SaveTMDBConfig(cfg); err != nil {
		return err
	}

	return nil
}

// TestConnection 验证 API Key 与网络代理连通性
func (s *TMDBService) TestConnection(cfg model.TMDBConfig) error {
	apiKey := strings.TrimSpace(cfg.ApiKey)
	if repository.IsMaskedTMDBApiKey(apiKey) {
		apiKey = repository.GetTMDBConfig().ApiKey
	}
	if apiKey == "" {
		return errors.New("API Key 不能为空")
	}

	client := getHTTPClient()
	testURL := fmt.Sprintf("%s/configuration?api_key=%s", tmdbAPIBaseURL, url.QueryEscape(apiKey))

	req, err := http.NewRequest(http.MethodGet, testURL, nil)
	if err != nil {
		return fmt.Errorf("构造测试请求失败: %w", err)
	}

	resp, err := client.Do(req)
	if err != nil {
		return fmt.Errorf("连接 TMDB 失败，请检查网络或代理设置: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusUnauthorized {
		return errors.New("TMDB API Key 无效或已被封禁 (HTTP 401)")
	}
	if resp.StatusCode != http.StatusOK {
		return fmt.Errorf("TMDB 响应异常 (HTTP %d)", resp.StatusCode)
	}
	return nil
}

// rawTMDBItem TMDB API 原始条目
type rawTMDBItem struct {
	ID            int64   `json:"id"`
	MediaType     string  `json:"media_type"`
	Title         string  `json:"title"`
	Name          string  `json:"name"`
	OriginalTitle string  `json:"original_title"`
	OriginalName  string  `json:"original_name"`
	ReleaseDate   string  `json:"release_date"`
	FirstAirDate  string  `json:"first_air_date"`
	PosterPath    string  `json:"poster_path"`
	BackdropPath  string  `json:"backdrop_path"`
	VoteAverage   float64 `json:"vote_average"`
	Overview      string  `json:"overview"`
}

// SearchRaw 已无调用方。弹窗检索请用 Search。
func (s *TMDBService) SearchRaw(query, year, mediaType string) ([]model.TMDBCandidate, error) {
	return s.Search(query, year, mediaType)
}

// prepareManualSearch 只去掉采集噪音。片名正文里的数字保留，仅提取括号中的年份。
func prepareManualSearch(query, year string) (string, string) {
	rawQuery := strings.TrimSpace(query)
	searchQuery := rawQuery
	detectedYear := ""
	if match := bracketYearRegex.FindStringSubmatch(searchQuery); len(match) > 1 {
		detectedYear = match[1]
		searchQuery = bracketYearRegex.ReplaceAllString(searchQuery, " ")
	}
	for _, reg := range noiseRegexes {
		searchQuery = reg.ReplaceAllString(searchQuery, " ")
	}
	searchQuery = emptyBracketsRegex.ReplaceAllString(searchQuery, " ")
	searchQuery = strings.Trim(searchQuery, " \t\r\n\u3000-_·—:：.~～〜!！?？")
	searchQuery = regexp.MustCompile(`[\s\x{3000}]+`).ReplaceAllString(searchQuery, " ")
	if searchQuery == "" {
		searchQuery = rawQuery
	}
	year = strings.TrimSpace(year)
	if year == "" {
		year = detectedYear
	}
	return searchQuery, year
}

// Search 供刮削弹窗检索。只去噪，并严格使用调用方传入的年份，只请求一次。
func (s *TMDBService) Search(query, year, mediaType string) ([]model.TMDBCandidate, error) {
	cfg := repository.GetTMDBConfig()
	if !cfg.Enabled || cfg.ApiKey == "" {
		return nil, errors.New("尚未配置或未启用 TMDB 刮削功能，请前往「系统设置 → 刮削配置」完成设置")
	}
	searchQuery, year := prepareManualSearch(query, year)
	return s.fetchTMDBCandidates(cfg, searchQuery, year, mediaType)
}

func (s *TMDBService) fetchTMDBCandidates(cfg model.TMDBConfig, kw, year, mediaType string) ([]model.TMDBCandidate, error) {
	kw = strings.TrimSpace(kw)
	if kw == "" {
		return nil, nil
	}
	year = strings.TrimSpace(year)
	mediaType = strings.ToLower(strings.TrimSpace(mediaType))

	qVals := url.Values{}
	qVals.Set("api_key", cfg.ApiKey)
	qVals.Set("language", cfg.Language)
	qVals.Set("query", kw)
	qVals.Set("page", "1")
	qVals.Set("include_adult", "false")

	var endpoint string
	switch mediaType {
	case "movie":
		endpoint = "/search/movie"
		if year != "" {
			qVals.Set("year", year)
		}
	case "tv":
		endpoint = "/search/tv"
		if year != "" {
			qVals.Set("first_air_date_year", year)
		}
	default:
		endpoint = "/search/multi"
	}

	reqURL := fmt.Sprintf("%s%s?%s", tmdbAPIBaseURL, endpoint, qVals.Encode())
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := getHTTPClient().Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 TMDB 失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(resp.Body)
		return nil, fmt.Errorf("TMDB 返回错误 (HTTP %d): %s", resp.StatusCode, string(body))
	}

	var searchResp struct {
		Results []rawTMDBItem `json:"results"`
	}
	if err := json.NewDecoder(resp.Body).Decode(&searchResp); err != nil {
		return nil, fmt.Errorf("解析 TMDB 数据失败: %w", err)
	}
	return parseCandidates(searchResp.Results, mediaType, year, cfg.ImageDomain), nil
}

func parseCandidates(results []rawTMDBItem, mediaType, filterYear, imagePrefix string) []model.TMDBCandidate {
	candidates := make([]model.TMDBCandidate, 0, len(results))
	for _, item := range results {
		mType := item.MediaType
		if mType == "" {
			mType = mediaType
		}
		if mType != "movie" && mType != "tv" {
			continue
		}

		title := item.Title
		if title == "" {
			title = item.Name
		}
		origTitle := item.OriginalTitle
		if origTitle == "" {
			origTitle = item.OriginalName
		}
		relDate := item.ReleaseDate
		if relDate == "" {
			relDate = item.FirstAirDate
		}
		y := ""
		if len(relDate) >= 4 {
			y = relDate[:4]
		}

		if filterYear != "" && y != "" {
			candY, err1 := strconv.Atoi(y)
			filterY, err2 := strconv.Atoi(filterYear)
			if err1 == nil && err2 == nil {
				diff := candY - filterY
				if diff < -1 || diff > 1 {
					continue
				}
			} else if y != filterYear {
				continue
			}
		}

		var posterURL, backdropURL string
		if item.PosterPath != "" {
			posterURL = fmt.Sprintf("%s/w500%s", imagePrefix, item.PosterPath)
		}
		if item.BackdropPath != "" {
			backdropURL = fmt.Sprintf("%s/w1280%s", imagePrefix, item.BackdropPath)
		}

		candidates = append(candidates, model.TMDBCandidate{
			ID:            item.ID,
			MediaType:     mType,
			Title:         title,
			OriginalTitle: origTitle,
			ReleaseDate:   relDate,
			Year:          y,
			Poster:        posterURL,
			Backdrop:      backdropURL,
			VoteAverage:   item.VoteAverage,
			Overview:      item.Overview,
		})
	}
	return candidates
}

type tmdbCreditPerson struct {
	Name string `json:"name"`
	Job  string `json:"job"`
}

type tmdbDetailRaw struct {
	rawTMDBItem
	Tagline string `json:"tagline"`
	Genres  []struct {
		Name string `json:"name"`
	} `json:"genres"`
	Credits struct {
		Cast []struct {
			Name string `json:"name"`
		} `json:"cast"`
		Crew []tmdbCreditPerson `json:"crew"`
	} `json:"credits"`
}

// FetchDetail 拉取影片/剧集详情与演职员表
func (s *TMDBService) FetchDetail(tmdbID int64, mediaType string) (*model.TMDBDetail, error) {
	cfg := repository.GetTMDBConfig()
	if !cfg.Enabled || cfg.ApiKey == "" {
		return nil, errors.New("TMDB 刮削尚未启用或配置")
	}

	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	if mediaType != "tv" {
		mediaType = "movie"
	}

	client := getHTTPClient()
	qVals := url.Values{}
	qVals.Set("api_key", cfg.ApiKey)
	qVals.Set("language", cfg.Language)
	qVals.Set("append_to_response", "credits")

	reqURL := fmt.Sprintf("%s/%s/%d?%s", tmdbAPIBaseURL, mediaType, tmdbID, qVals.Encode())
	req, err := http.NewRequest(http.MethodGet, reqURL, nil)
	if err != nil {
		return nil, err
	}

	resp, err := client.Do(req)
	if err != nil {
		return nil, fmt.Errorf("请求 TMDB 详情失败: %w", err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		return nil, fmt.Errorf("TMDB 详情返回错误 (HTTP %d)", resp.StatusCode)
	}

	var raw tmdbDetailRaw
	if err := json.NewDecoder(resp.Body).Decode(&raw); err != nil {
		return nil, err
	}

	title := raw.Title
	if title == "" {
		title = raw.Name
	}
	origTitle := raw.OriginalTitle
	if origTitle == "" {
		origTitle = raw.OriginalName
	}
	relDate := raw.ReleaseDate
	if relDate == "" {
		relDate = raw.FirstAirDate
	}
	y := ""
	if len(relDate) >= 4 {
		y = relDate[:4]
	}

	genres := make([]string, 0, len(raw.Genres))
	for _, g := range raw.Genres {
		if strings.TrimSpace(g.Name) != "" {
			genres = append(genres, strings.TrimSpace(g.Name))
		}
	}

	actors := make([]string, 0, 10)
	for i, c := range raw.Credits.Cast {
		if i >= 10 {
			break
		}
		if name := strings.TrimSpace(c.Name); name != "" {
			actors = append(actors, name)
		}
	}

	directors := make([]string, 0, 3)
	for _, c := range raw.Credits.Crew {
		if strings.EqualFold(c.Job, "Director") {
			if name := strings.TrimSpace(c.Name); name != "" {
				directors = append(directors, name)
			}
		}
	}

	var posterURL, backdropURL string
	if raw.PosterPath != "" {
		posterURL = fmt.Sprintf("%s/w780%s", cfg.ImageDomain, raw.PosterPath)
	}
	if raw.BackdropPath != "" {
		backdropURL = fmt.Sprintf("%s/original%s", cfg.ImageDomain, raw.BackdropPath)
	}

	scoreStr := ""
	if raw.VoteAverage > 0 {
		scoreStr = fmt.Sprintf("%.1f", raw.VoteAverage)
	}

	return &model.TMDBDetail{
		ID:            raw.ID,
		MediaType:     mediaType,
		Title:         title,
		OriginalTitle: origTitle,
		ReleaseDate:   relDate,
		Year:          y,
		Poster:        posterURL,
		Backdrop:      backdropURL,
		VoteAverage:   raw.VoteAverage,
		VoteScore:     scoreStr,
		Overview:      raw.Overview,
		Tagline:       raw.Tagline,
		Genres:        genres,
		Directors:     directors,
		Actors:        actors,
	}, nil
}
