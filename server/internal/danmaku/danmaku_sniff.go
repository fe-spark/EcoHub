package danmaku

import (
	"context"
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"errors"
	"fmt"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"strconv"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repobase"
	"server/internal/utils"
)

var epNumRe = regexp.MustCompile(`\d{1,4}`)
var titleCleanRe = regexp.MustCompile(`(?i)(?:【[^】]*】|\[[^\]]*\]|\([^\)]*\)|4k|1080p|720p|hd|bd|tc|ts|h264|h265|x264|x265|国语|国漫|中字|双字|普通话|粤语|先行版|抢先版|预告片)`)

func cleanSearchTitle(title string) string {
	cleaned := titleCleanRe.ReplaceAllString(title, " ")
	cleaned = strings.Join(strings.Fields(cleaned), " ")
	if strings.TrimSpace(cleaned) == "" {
		return strings.TrimSpace(title)
	}
	return cleaned
}

func signDandanplayRequest(req *http.Request, appID, appSecret, apiPath string) {
	appID = strings.TrimSpace(appID)
	appSecret = strings.TrimSpace(appSecret)
	if appID == "" {
		return
	}
	req.Header.Set("X-AppId", appID)
	if appSecret != "" {
		timestamp := time.Now().Unix()
		toSign := fmt.Sprintf("%s%d%s%s", appID, timestamp, apiPath, appSecret)
		hash := sha256.Sum256([]byte(toSign))
		sig := base64.StdEncoding.EncodeToString(hash[:])

		req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
		req.Header.Set("X-Signature", sig)
	}
}

func resolveDanmuBaseURL(cfg model.DanmakuConfig) string {
	raw := strings.TrimSpace(cfg.ApiURL)
	if raw != "" {
		return strings.TrimRight(raw, "/")
	}
	return "https://api.dandanplay.net/api/v2"
}

// sniffResult 一次全网弹幕嗅探的结果
type sniffResult struct {
	Items        []model.DanmakuItem
	Status       string // networkOK | networkEmpty | networkError
	Reason       string
	Cached       bool
	AnimeTitle   string // 匹配到的作品名（缓存命中时为空）
	EpisodeTitle string // 匹配到的分集名（缓存命中时为空）
}

// danmakuUpstreamError 带给人看的原因；原始错误只写日志，避免把接口地址或 token 回给前端
type danmakuUpstreamError struct {
	reason string
}

func (e *danmakuUpstreamError) Error() string { return e.reason }

// errProxyConfig 读不到后台「网络代理」配置时不发请求，避免绕过代理设置直连
var errProxyConfig = &danmakuUpstreamError{reason: "读取网络代理配置失败"}

func upstreamReason(err error) string {
	var ue *danmakuUpstreamError
	if errors.As(err, &ue) {
		return ue.reason
	}
	return "弹幕接口请求失败"
}

func requestFailure(err error) error {
	var netErr interface{ Timeout() bool }
	if errors.Is(err, context.DeadlineExceeded) || (errors.As(err, &netErr) && netErr.Timeout()) {
		return &danmakuUpstreamError{reason: "弹幕接口请求超时"}
	}
	return &danmakuUpstreamError{reason: "无法连接弹幕接口"}
}

func statusFailure(code int, official bool) error {
	switch {
	case (code == http.StatusUnauthorized || code == http.StatusForbidden) && official:
		return &danmakuUpstreamError{reason: fmt.Sprintf("弹弹play 鉴权失败（HTTP %d），请检查 AppId / AppSecret", code)}
	case code == http.StatusUnauthorized || code == http.StatusForbidden:
		return &danmakuUpstreamError{reason: fmt.Sprintf("弹幕接口拒绝访问（HTTP %d），请检查接口地址里的 token", code)}
	default:
		return &danmakuUpstreamError{reason: fmt.Sprintf("弹幕接口返回 HTTP %d", code)}
	}
}

// SniffResult 弹幕嗅探结果别名，供管理后台测试接口复用。
type SniffResult = sniffResult

// FetchSniffedDanmaku 从公开弹幕库嗅探对应剧集弹幕。useCache=false 时跳过读写缓存（后台测试用）。
func FetchSniffedDanmaku(cfg model.DanmakuConfig, title string, episode int, useCache bool) SniffResult {
	return fetchSniffedDanmaku(cfg, title, episode, useCache)
}

// fetchSniffedDanmaku 从公开弹幕库嗅探对应剧集弹幕。useCache=false 时跳过读写缓存（后台测试用）。
func fetchSniffedDanmaku(cfg model.DanmakuConfig, title string, episode int, useCache bool) sniffResult {
	if strings.TrimSpace(cfg.ApiURL) == "" && strings.TrimSpace(cfg.AppID) == "" {
		return sniffResult{Status: networkError, Reason: "未配置自建弹幕接口，也没有填写弹弹play AppId"}
	}
	baseURL := resolveDanmuBaseURL(cfg)
	normalizedTitle := cleanSearchTitle(title)
	if normalizedTitle == "" {
		return sniffResult{Status: networkEmpty, Reason: "缺少有效片名"}
	}
	cacheKey := fmt.Sprintf("%s:Danmaku:%s:%s:%d", config.RedisKeyPrefix, url.QueryEscape(baseURL), normalizedTitle, episode)
	if useCache && db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []model.DanmakuItem
			if json.Unmarshal([]byte(data), &cached) == nil {
				return sniffResult{Items: cached, Status: networkOK, Cached: true}
			}
		}
	}

	val, _, _ := danmakuSniffSF.Do(cacheKey, func() (any, error) {
		select {
		case danmakuSniffSem <- struct{}{}:
			defer func() { <-danmakuSniffSem }()
		case <-time.After(1500 * time.Millisecond):
			return sniffResult{Status: networkError, Reason: "弹幕嗅探并发繁忙，请稍后重试"}, nil
		}

		match, err := searchDandanplayEpisode(cfg, baseURL, normalizedTitle, episode)
		if err != nil {
			return sniffResult{Status: networkError, Reason: upstreamReason(err)}, nil
		}
		if match.EpisodeID <= 0 {
			return sniffResult{Status: networkEmpty, Reason: "没有匹配到这部片"}, nil
		}

		items, err := loadDandanplayComments(cfg, baseURL, match.EpisodeID)
		res := sniffResult{AnimeTitle: match.AnimeTitle, EpisodeTitle: match.EpisodeTitle}
		if err != nil {
			res.Status, res.Reason = networkError, upstreamReason(err)
			return res, nil
		}
		if len(items) == 0 {
			res.Status, res.Reason = networkEmpty, "匹配到了这部片，但这一集没有弹幕"
			return res, nil
		}
		if useCache && db.Rdb != nil {
			if data, err := json.Marshal(items); err == nil {
				db.Rdb.Set(db.Cxt, cacheKey, string(data), 7*24*time.Hour)
			}
		}
		res.Items, res.Status = items, networkOK
		return res, nil
	})
	return val.(sniffResult)
}

// dandanMatch 搜索匹配到的分集
type dandanMatch struct {
	EpisodeID    int64
	AnimeTitle   string
	EpisodeTitle string
}

type dandanEpisode struct {
	EpisodeID    int64  `json:"episodeId"`
	EpisodeTitle string `json:"episodeTitle"`
}

type dandanAnime struct {
	AnimeTitle string          `json:"animeTitle"`
	Episodes   []dandanEpisode `json:"episodes"`
}

func parseEpisodeNumber(raw string) int {
	indices := epNumRe.FindAllStringIndex(raw, -1)
	result := 0
	for _, loc := range indices {
		start, end := loc[0], loc[1]
		numStr := raw[start:end]
		num, err := strconv.Atoi(numStr)
		if err != nil || num <= 0 || num > 2000 {
			continue
		}
		// 排除 1080P / 720P / 4K / 2160P 等清晰度数字
		if end < len(raw) {
			after := raw[end]
			if after == 'p' || after == 'P' || after == 'k' || after == 'K' {
				continue
			}
		}
		if start > 0 {
			before := raw[start-1]
			if before == 'p' || before == 'P' || before == 'k' || before == 'K' {
				continue
			}
		}
		result = num
	}
	return result
}

// danmakuHTTPClient 按后台「网络代理」的弹幕模块开关选择出口：
// 代理开启且弹幕模块开启时走代理，否则用共享直连 Transport（不读 HTTPS_PROXY 等环境变量）。
// 播放页嗅探和后台「测试」都经过这里。
func danmakuHTTPClient(timeout time.Duration) (*http.Client, error) {
	ok, proxyURL, err := repobase.ResolveDanmakuProxy()
	if err != nil {
		return nil, err
	}
	if !ok {
		proxyURL = ""
	}
	return &http.Client{
		Transport:     utils.GetOrCreateProxyTransport(proxyURL),
		Timeout:       timeout,
		CheckRedirect: utils.CheckSafeOutboundRedirect,
	}, nil
}

func searchDandanplayEpisode(cfg model.DanmakuConfig, baseURL, title string, targetEpisode int) (dandanMatch, error) {
	if cleaned := cleanSearchTitle(title); cleaned != "" {
		title = cleaned
	}
	apiPath := "/api/v2/search/episodes"
	apiURL := fmt.Sprintf("%s/search/episodes?anime=%s", baseURL, url.QueryEscape(title))
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return dandanMatch{}, &danmakuUpstreamError{reason: "弹幕接口地址无效"}
	}
	req.Header.Set("User-Agent", "EcoHub-Client/3.0")
	req.Header.Set("Accept", "application/json")
	signDandanplayRequest(req, cfg.AppID, cfg.AppSecret, apiPath)

	client, err := danmakuHTTPClient(6 * time.Second)
	if err != nil {
		log.Printf("[Danmaku] 读取网络代理配置失败 err=%v", err)
		return dandanMatch{}, errProxyConfig
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Danmaku] 请求弹幕搜索失败 title=%s err=%v", title, err)
		return dandanMatch{}, requestFailure(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Danmaku] 弹幕源返回状态码 status=%d (若使用官方接口请在后台配置 AppId 与 AppSecret，或使用自建免鉴权 danmu-api)", resp.StatusCode)
		return dandanMatch{}, statusFailure(resp.StatusCode, strings.TrimSpace(cfg.ApiURL) == "")
	}

	body, err := readLimitedDanmakuBody(resp.Body)
	if err != nil {
		return dandanMatch{}, requestFailure(err)
	}

	var res struct {
		Success      bool          `json:"success"`
		ErrorMessage string        `json:"errorMessage"`
		Animes       []dandanAnime `json:"animes"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return dandanMatch{}, &danmakuUpstreamError{reason: "弹幕接口返回的内容无法解析"}
	}
	if !res.Success && strings.TrimSpace(res.ErrorMessage) != "" {
		return dandanMatch{}, &danmakuUpstreamError{reason: "弹幕接口报错：" + truncateRunes(res.ErrorMessage, 60)}
	}
	if len(res.Animes) == 0 {
		return dandanMatch{}, nil
	}

	// 1. 各候选剧集标题精确匹配集号
	for _, anime := range res.Animes {
		for _, ep := range anime.Episodes {
			if parseEpisodeNumber(ep.EpisodeTitle) == targetEpisode {
				return dandanMatch{EpisodeID: ep.EpisodeID, AnimeTitle: anime.AnimeTitle, EpisodeTitle: ep.EpisodeTitle}, nil
			}
		}
	}

	// 2. 兜底：第一个条目按序取
	if len(res.Animes[0].Episodes) > 0 {
		idx := targetEpisode - 1
		if idx < 0 {
			idx = 0
		}
		if idx >= len(res.Animes[0].Episodes) {
			idx = len(res.Animes[0].Episodes) - 1
		}
		ep := res.Animes[0].Episodes[idx]
		return dandanMatch{EpisodeID: ep.EpisodeID, AnimeTitle: res.Animes[0].AnimeTitle, EpisodeTitle: ep.EpisodeTitle}, nil
	}
	return dandanMatch{}, nil
}

func truncateRunes(s string, n int) string {
	r := []rune(strings.TrimSpace(s))
	if len(r) <= n {
		return string(r)
	}
	return string(r[:n]) + "…"
}

func loadDandanplayComments(cfg model.DanmakuConfig, baseURL string, episodeID int64) ([]model.DanmakuItem, error) {
	apiPath := fmt.Sprintf("/api/v2/comment/%d", episodeID)
	apiURL := fmt.Sprintf("%s/comment/%d?withRelated=true&chConvert=1", baseURL, episodeID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil, &danmakuUpstreamError{reason: "弹幕接口地址无效"}
	}
	req.Header.Set("User-Agent", "EcoHub-Client/3.0")
	req.Header.Set("Accept", "application/json")
	signDandanplayRequest(req, cfg.AppID, cfg.AppSecret, apiPath)

	client, err := danmakuHTTPClient(8 * time.Second)
	if err != nil {
		log.Printf("[Danmaku] 读取网络代理配置失败 err=%v", err)
		return nil, errProxyConfig
	}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Danmaku] 请求弹幕详情失败 episodeID=%d err=%v", episodeID, err)
		return nil, requestFailure(err)
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Danmaku] 获取弹幕内容失败 status=%d", resp.StatusCode)
		return nil, statusFailure(resp.StatusCode, strings.TrimSpace(cfg.ApiURL) == "")
	}

	body, err := readLimitedDanmakuBody(resp.Body)
	if err != nil {
		return nil, requestFailure(err)
	}

	var res struct {
		Comments []struct {
			P string `json:"p"` // "time,mode,color,user"
			M string `json:"m"` // 弹幕文字
		} `json:"comments"`
	}
	if err := json.Unmarshal(body, &res); err != nil {
		return nil, &danmakuUpstreamError{reason: "弹幕接口返回的内容无法解析"}
	}
	if len(res.Comments) == 0 {
		return nil, nil
	}

	const maxComments = 30000
	items := make([]model.DanmakuItem, 0, len(res.Comments))
	for _, c := range res.Comments {
		if c.M == "" {
			continue
		}
		parts := strings.Split(c.P, ",")
		if len(parts) < 3 {
			continue
		}
		t, _ := strconv.ParseFloat(parts[0], 64)
		modeVal, _ := strconv.Atoi(parts[1])
		colorInt, colorErr := strconv.ParseInt(strings.TrimSpace(parts[2]), 10, 64)

		mode := 0 // scroll
		if modeVal == 5 {
			mode = 1 // top
		} else if modeVal == 4 {
			mode = 2 // bottom
		}

		color := "#ffffff"
		if colorErr == nil && colorInt >= 0 && colorInt <= 0xffffff {
			color = fmt.Sprintf("#%06x", colorInt)
		}

		items = append(items, model.DanmakuItem{
			Text:  c.M,
			Time:  t,
			Color: color,
			Mode:  mode,
		})

		if len(items) >= maxComments {
			break
		}
	}
	return items, nil
}
