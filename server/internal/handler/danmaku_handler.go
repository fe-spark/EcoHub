package handler

import (
	"crypto/sha256"
	"encoding/base64"
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"regexp"
	"sort"
	"strconv"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"

	"github.com/gin-gonic/gin"
)

type DanmakuHandler struct{}

var DanmakuHd = new(DanmakuHandler)

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
		req.Header.Set("X-AppSecret", appSecret)
		timestamp := time.Now().Unix()
		toSign := fmt.Sprintf("%s%d%s%s", appID, timestamp, apiPath, appSecret)
		hash := sha256.Sum256([]byte(toSign))
		sig := base64.StdEncoding.EncodeToString(hash[:])

		req.Header.Set("X-Timestamp", strconv.FormatInt(timestamp, 10))
		req.Header.Set("X-Signature", sig)
	}
}

// GetDanmaku 获取指定影片剧集的弹幕列表（合并本地与自动嗅探）
func (h *DanmakuHandler) GetDanmaku(c *gin.Context) {
	basic, _ := repository.GetSiteBasic()
	if !basic.Danmaku.Enabled {
		dto.Success([]model.DanmakuItem{}, "弹幕功能已关闭", c)
		return
	}

	mid, _ := strconv.ParseInt(c.Query("mid"), 10, 64)
	episode, _ := strconv.Atoi(c.DefaultQuery("episode", "1"))
	if episode <= 0 {
		episode = 1
	}
	title := strings.TrimSpace(c.Query("title"))

	items := make([]model.DanmakuItem, 0)

	// 1. 读取本地数据库用户发送的弹幕
	if db.Mdb != nil && mid > 0 {
		var locals []model.DanmakuRecord
		if err := db.Mdb.Where("mid = ? AND episode = ?", mid, episode).
			Order("time ASC").
			Find(&locals).Error; err == nil {
			for _, r := range locals {
				items = append(items, model.DanmakuItem{
					Text:  r.Text,
					Time:  r.Time,
					Color: r.Color,
					Mode:  r.Mode,
				})
			}
		}
	}

	// 2. 自动嗅探外部弹幕（全网弹弹play生态）
	if basic.Danmaku.AutoSniff && title != "" {
		sniffed := fetchSniffedDanmaku(basic.Danmaku, title, episode)
		items = append(items, sniffed...)
	}

	// 按出现时间升序排序
	sort.Slice(items, func(i, j int) bool {
		return items[i].Time < items[j].Time
	})

	dto.Success(items, "获取弹幕成功", c)
}

// PostDanmaku 发送弹幕入库
func (h *DanmakuHandler) PostDanmaku(c *gin.Context) {
	basic, _ := repository.GetSiteBasic()
	if !basic.Danmaku.Enabled {
		dto.Failed("弹幕功能已关闭", c)
		return
	}

	var req struct {
		Mid     int64   `json:"mid"`
		Episode int     `json:"episode"`
		Time    float64 `json:"time"`
		Text    string  `json:"text"`
		Color   string  `json:"color"`
		Mode    int     `json:"mode"`
	}
	if err := c.ShouldBindJSON(&req); err != nil {
		dto.Failed("请求参数无效", c)
		return
	}
	req.Text = strings.TrimSpace(req.Text)
	if req.Text == "" || len(req.Text) > 200 {
		dto.Failed("弹幕内容不能为空且不能超过200字", c)
		return
	}
	if req.Color == "" {
		req.Color = "#ffffff"
	}
	if req.Episode <= 0 {
		req.Episode = 1
	}

	if db.Mdb == nil {
		dto.Failed("保存弹幕失败", c)
		return
	}
	record := model.DanmakuRecord{
		Mid:     req.Mid,
		Episode: req.Episode,
		Time:    req.Time,
		Text:    req.Text,
		Color:   req.Color,
		Mode:    req.Mode,
		IP:      c.ClientIP(),
	}
	if err := db.Mdb.Create(&record).Error; err != nil {
		dto.Failed("保存弹幕失败: "+err.Error(), c)
		return
	}
	dto.Success(nil, "发送成功", c)
}

func resolveDanmuBaseURL(cfg model.DanmakuConfig) string {
	raw := strings.TrimSpace(cfg.ApiURL)
	if raw != "" {
		return strings.TrimRight(raw, "/")
	}
	return "https://api.dandanplay.net/api/v2"
}

// fetchSniffedDanmaku 从公开弹幕库嗅探对应剧集弹幕
func fetchSniffedDanmaku(cfg model.DanmakuConfig, title string, episode int) []model.DanmakuItem {
	cacheKey := fmt.Sprintf("%s:Danmaku:%s:%d", config.RedisKeyPrefix, title, episode)
	if db.Rdb != nil {
		if data, err := db.Rdb.Get(db.Cxt, cacheKey).Result(); err == nil && data != "" {
			var cached []model.DanmakuItem
			if json.Unmarshal([]byte(data), &cached) == nil {
				return cached
			}
		}
	}

	baseURL := resolveDanmuBaseURL(cfg)
	episodeID := searchDandanplayEpisode(cfg, baseURL, title, episode)
	if episodeID <= 0 {
		return nil
	}

	items := loadDandanplayComments(cfg, baseURL, episodeID)
	if len(items) > 0 && db.Rdb != nil {
		if data, err := json.Marshal(items); err == nil {
			db.Rdb.Set(db.Cxt, cacheKey, string(data), 7*24*time.Hour)
		}
	}
	return items
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

func searchDandanplayEpisode(cfg model.DanmakuConfig, baseURL, rawTitle string, targetEpisode int) int64 {
	title := cleanSearchTitle(rawTitle)
	apiPath := "/api/v2/search/episodes"
	apiURL := fmt.Sprintf("%s/search/episodes?anime=%s", baseURL, url.QueryEscape(title))
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return 0
	}
	req.Header.Set("User-Agent", "EcoHub-Client/3.0")
	req.Header.Set("Accept", "application/json")
	signDandanplayRequest(req, cfg.AppID, cfg.AppSecret, apiPath)

	client := &http.Client{Timeout: 6 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Danmaku] 请求弹幕搜索失败 title=%s err=%v", title, err)
		return 0
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Danmaku] 弹幕源返回状态码 status=%d (若使用官方接口请在后台配置 AppId 与 AppSecret，或使用自建免鉴权 danmu-api)", resp.StatusCode)
		return 0
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return 0
	}

	var res struct {
		Success bool          `json:"success"`
		Animes  []dandanAnime `json:"animes"`
	}
	if err := json.Unmarshal(body, &res); err != nil || len(res.Animes) == 0 {
		return 0
	}

	// 1. 各候选剧集标题精确匹配集号
	for _, anime := range res.Animes {
		for _, ep := range anime.Episodes {
			if parseEpisodeNumber(ep.EpisodeTitle) == targetEpisode {
				return ep.EpisodeID
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
		return res.Animes[0].Episodes[idx].EpisodeID
	}
	return 0
}

func loadDandanplayComments(cfg model.DanmakuConfig, baseURL string, episodeID int64) []model.DanmakuItem {
	apiPath := fmt.Sprintf("/api/v2/comment/%d", episodeID)
	apiURL := fmt.Sprintf("%s/comment/%d?withRelated=true&chConvert=1", baseURL, episodeID)
	req, err := http.NewRequest("GET", apiURL, nil)
	if err != nil {
		return nil
	}
	req.Header.Set("User-Agent", "EcoHub-Client/3.0")
	req.Header.Set("Accept", "application/json")
	signDandanplayRequest(req, cfg.AppID, cfg.AppSecret, apiPath)

	client := &http.Client{Timeout: 8 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		log.Printf("[Danmaku] 请求弹幕详情失败 episodeID=%d err=%v", episodeID, err)
		return nil
	}
	defer resp.Body.Close()

	if resp.StatusCode != http.StatusOK {
		log.Printf("[Danmaku] 获取弹幕内容失败 status=%d", resp.StatusCode)
		return nil
	}

	body, err := io.ReadAll(resp.Body)
	if err != nil {
		return nil
	}

	var res struct {
		Comments []struct {
			P string `json:"p"` // "time,mode,color,user"
			M string `json:"m"` // 弹幕文字
		} `json:"comments"`
	}
	if err := json.Unmarshal(body, &res); err != nil || len(res.Comments) == 0 {
		return nil
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
		colorInt, _ := strconv.ParseInt(parts[2], 10, 64)

		mode := 0 // scroll
		if modeVal == 5 {
			mode = 1 // top
		} else if modeVal == 4 {
			mode = 2 // bottom
		}

		color := "#ffffff"
		if colorInt > 0 {
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
	return items
}
