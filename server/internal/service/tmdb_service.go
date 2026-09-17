package service

import (
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strings"
	"time"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"server/internal/repository/film/writer"
)

type TMDBService struct{}

var TMDBSvc = new(TMDBService)

const (
	tmdbAPIBaseURL = "https://api.themoviedb.org/3"
	httpTimeout    = 15 * time.Second
)

var (
	// 清洗影视标题中常见的噪音标签
	noiseRegexes = []*regexp.Regexp{
		regexp.MustCompile(`(?i)\[.*?\]`),
		regexp.MustCompile(`(?i)【.*?】`),
		regexp.MustCompile(`(?i)[（(]?\b(4k|1080p|720p|2160p|hd|bd|tc|ts|dvd|web-dl|bluray)\b[）)]?`),
		regexp.MustCompile(`(?i)[0-9]+(帧|fps)`),
		regexp.MustCompile(`(?i)(国粤双语|国语版|粤语版|原声版|中文字幕|中英双字|双语字幕|抢先版|修复版|加长版|无删减|未删减)`),
		regexp.MustCompile(`(?i)(第[0-9一二三四五六七八九十]+[集期话]|更新至[0-9]+[集期话]|全[0-9]+[集期话]|完结|连载中)`),
	}
	yearRegex          = regexp.MustCompile(`\b(19\d{2}|20\d{2})\b`)
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

	// 清洗标点与多余空格
	s = strings.TrimSpace(s)
	s = regexp.MustCompile(`\s+`).ReplaceAllString(s, " ")
	return s, extractedYear
}

func getHTTPClient(proxyStr string) *http.Client {
	var transport *http.Transport
	if t, ok := http.DefaultTransport.(*http.Transport); ok {
		transport = t.Clone()
	} else {
		transport = &http.Transport{}
	}

	proxyStr = strings.TrimSpace(proxyStr)
	if proxyStr != "" {
		if !strings.HasPrefix(proxyStr, "http://") && !strings.HasPrefix(proxyStr, "https://") && !strings.HasPrefix(proxyStr, "socks5://") {
			proxyStr = "http://" + proxyStr
		}
		if u, err := url.Parse(proxyStr); err == nil {
			transport.Proxy = http.ProxyURL(u)
		}
	} else {
		transport.Proxy = http.ProxyFromEnvironment
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

// UpdateConfig 更新 TMDB 配置
func (s *TMDBService) UpdateConfig(cfg model.TMDBConfig) error {
	existing := repository.GetTMDBConfig()
	if repository.IsMaskedTMDBApiKey(cfg.ApiKey) {
		cfg.ApiKey = existing.ApiKey
	}
	return repository.SaveTMDBConfig(cfg)
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

	client := getHTTPClient(strings.TrimSpace(cfg.Proxy))
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

// Search 检索 TMDB 候选条目
func (s *TMDBService) Search(query, year, mediaType string) ([]model.TMDBCandidate, error) {
	cfg := repository.GetTMDBConfig()
	if !cfg.Enabled || cfg.ApiKey == "" {
		return nil, errors.New("尚未配置或未启用 TMDB 刮削功能，请前往「系统设置 → 刮削配置」完成设置")
	}

	cleanedQuery, detectedYear := CleanKeywordForSearch(query)
	if cleanedQuery == "" {
		cleanedQuery = strings.TrimSpace(query)
	}
	year = strings.TrimSpace(year)

	client := getHTTPClient(cfg.Proxy)
	var endpoint string
	qVals := url.Values{}
	qVals.Set("api_key", cfg.ApiKey)
	qVals.Set("language", cfg.Language)
	qVals.Set("query", cleanedQuery)
	qVals.Set("page", "1")
	qVals.Set("include_adult", "false")

	doFetch := func(targetEndpoint string, targetQVals url.Values) ([]rawTMDBItem, error) {
		reqURL := fmt.Sprintf("%s%s?%s", tmdbAPIBaseURL, targetEndpoint, targetQVals.Encode())
		req, err := http.NewRequest(http.MethodGet, reqURL, nil)
		if err != nil {
			return nil, err
		}

		resp, err := client.Do(req)
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
		return searchResp.Results, nil
	}

	var (
		results []rawTMDBItem
		err     error
	)

	mediaType = strings.ToLower(strings.TrimSpace(mediaType))
	switch mediaType {
	case "movie":
		endpoint = "/search/movie"
		if year != "" {
			qVals.Set("year", year)
		}
		results, err = doFetch(endpoint, qVals)
	case "tv":
		endpoint = "/search/tv"
		if year != "" {
			qVals.Set("first_air_date_year", year)
		}
		results, err = doFetch(endpoint, qVals)
	default:
		endpoint = "/search/multi"
		results, err = doFetch(endpoint, qVals)
	}

	if err != nil {
		return nil, err
	}

	candidates := make([]model.TMDBCandidate, 0, len(results))
	imagePrefix := cfg.ImageDomain
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

		// 用户显式指定了年份过滤条件时，严格按年份过滤候选条目
		if year != "" && y != year {
			continue
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

	// 当未显式指定年份，但搜索片名中包含检测出的年份时，优先将匹配该年份的候选条目排在前面
	if year == "" && detectedYear != "" && len(candidates) > 1 {
		sort.SliceStable(candidates, func(i, j int) bool {
			matchI := candidates[i].Year == detectedYear
			matchJ := candidates[j].Year == detectedYear
			if matchI && !matchJ {
				return true
			}
			return false
		})
	}

	return candidates, nil
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

	client := getHTTPClient(cfg.Proxy)
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

// ApplyDetail 将 TMDB 刮削出的元数据应用到指定影片并落库刷新
func (s *TMDBService) ApplyDetail(req model.TMDBApplyReq) error {
	if req.Mid <= 0 || req.TmdbID <= 0 {
		return errors.New("影片ID或 TMDB ID 参数非法")
	}

	tmdbData, err := s.FetchDetail(req.TmdbID, req.MediaType)
	if err != nil {
		return fmt.Errorf("获取 TMDB 详情失败: %w", err)
	}

	var detailRec model.MovieDetailInfo
	if err := db.Mdb.Where("mid = ?", req.Mid).First(&detailRec).Error; err != nil {
		return fmt.Errorf("未找到对应的影片信息 (mid=%d): %w", req.Mid, err)
	}

	var detail model.MovieDetail
	if err := json.Unmarshal([]byte(detailRec.Content), &detail); err != nil {
		return fmt.Errorf("解析既有影片数据失败: %w", err)
	}
	detail.Id = req.Mid

	var indexRec model.FilmIndex
	_ = db.Mdb.Where("mid = ?", req.Mid).First(&indexRec).Error

	fieldSet := make(map[string]struct{}, len(req.Fields))
	for _, f := range req.Fields {
		fieldSet[strings.ToLower(strings.TrimSpace(f))] = struct{}{}
	}
	applyAll := len(fieldSet) == 0

	shouldApply := func(names ...string) bool {
		if applyAll {
			return true
		}
		for _, name := range names {
			if _, ok := fieldSet[strings.ToLower(name)]; ok {
				return true
			}
		}
		return false
	}

	// 字段覆盖
	if shouldApply("poster", "picture") && tmdbData.Poster != "" {
		detail.Picture = tmdbData.Poster
		detail.CustomPicture = tmdbData.Poster
		detail.IsCustomPicture = true
	}
	if shouldApply("backdrop", "pictureslide") && tmdbData.Backdrop != "" {
		detail.PictureSlide = tmdbData.Backdrop
		detail.CustomPictureSlide = tmdbData.Backdrop
		detail.IsCustomPicture = true
	}
	if shouldApply("overview", "content") && tmdbData.Overview != "" {
		detail.MovieDescriptor.Content = tmdbData.Overview
		detail.MovieDescriptor.Blurb = tmdbData.Overview
	}
	if shouldApply("subtitle", "originaltitle") && tmdbData.OriginalTitle != "" {
		detail.MovieDescriptor.SubTitle = tmdbData.OriginalTitle
	}
	if shouldApply("actor", "credits") && len(tmdbData.Actors) > 0 {
		detail.MovieDescriptor.Actor = strings.Join(tmdbData.Actors, "/")
	}
	if shouldApply("director", "credits") && len(tmdbData.Directors) > 0 {
		detail.MovieDescriptor.Director = strings.Join(tmdbData.Directors, "/")
	}
	if shouldApply("year", "releasedate") {
		if tmdbData.Year != "" {
			detail.MovieDescriptor.Year = tmdbData.Year
		}
		if tmdbData.ReleaseDate != "" {
			detail.MovieDescriptor.ReleaseDate = tmdbData.ReleaseDate
		} else if detail.MovieDescriptor.ReleaseDate == "" && tmdbData.Year != "" {
			detail.MovieDescriptor.ReleaseDate = tmdbData.Year
		}
	}
	if shouldApply("score", "voteaverage") && tmdbData.VoteScore != "" {
		detail.MovieDescriptor.DbScore = tmdbData.VoteScore
	}
	if shouldApply("tag", "genres") && len(tmdbData.Genres) > 0 {
		detail.MovieDescriptor.ClassTag = strings.Join(tmdbData.Genres, ",")
	}

	detail.MovieDescriptor.UpdateTime = time.Now().Format(time.DateTime)

	sourceID := indexRec.SourceId
	if sourceID == "" {
		sourceID = detailRec.SourceId
	}
	if sourceID == "" {
		sourceID = "manual"
	}

	return writer.SaveDetail(sourceID, detail)
}

// FetchFormPrefill 拉取用于前端表单自动填充的元数据
func (s *TMDBService) FetchFormPrefill(tmdbID int64, mediaType string) (map[string]any, error) {
	d, err := s.FetchDetail(tmdbID, mediaType)
	if err != nil {
		return nil, err
	}

	result := map[string]any{
		"name":         d.Title,
		"subTitle":     d.OriginalTitle,
		"picture":      d.Poster,
		"pictureSlide": d.Backdrop,
		"content":      d.Overview,
		"actor":        strings.Join(d.Actors, "/"),
		"director":     strings.Join(d.Directors, "/"),
		"year":         d.Year,
		"releaseDate":  d.ReleaseDate,
		"dbScore":      d.VoteScore,
		"classTag":     strings.Join(d.Genres, ","),
	}
	return result, nil
}
