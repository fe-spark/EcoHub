package danmaku

import (
	"log"
	"net/http"
	"sort"
	"strconv"
	"strings"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repobase"

	"github.com/gin-gonic/gin"
)

type DanmakuHandler struct{}

var DanmakuHd = new(DanmakuHandler)

// GetDanmaku 获取指定影片剧集的弹幕列表（合并本地与自动嗅探）
func (h *DanmakuHandler) GetDanmaku(c *gin.Context) {
	basic, _ := repobase.GetSiteBasic()
	if !basic.Danmaku.Enabled {
		respondDanmaku(c, []model.DanmakuItem{}, danmakuMeta{NetworkStatus: networkDisabled, NetworkReason: "弹幕功能已关闭"}, "弹幕功能已关闭")
		return
	}

	mid, _ := strconv.ParseInt(c.Query("mid"), 10, 64)
	episode, _ := strconv.Atoi(c.DefaultQuery("episode", "1"))
	if episode <= 0 {
		episode = 1
	}
	title := strings.TrimSpace(c.Query("title"))

	items := make([]model.DanmakuItem, 0)

	// 1. 读取本地数据库用户发送的弹幕（限制单集最多 2000 条防全量拉取与 OOM）
	if db.Mdb != nil && mid > 0 {
		var locals []model.DanmakuRecord
		if err := db.Mdb.Where("mid = ? AND episode = ?", mid, episode).
			Order("time ASC").
			Limit(2000).
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

	meta := danmakuMeta{Local: len(items)}

	// 2. 自动嗅探外部弹幕（全网弹弹play生态）
	switch {
	case !basic.Danmaku.AutoSniff:
		meta.NetworkStatus = networkDisabled
		meta.NetworkReason = "未开启全网弹幕"
	case title == "":
		meta.NetworkStatus = networkEmpty
		meta.NetworkReason = "缺少片名，无法匹配"
	case len([]rune(title)) > 120 || episode > 10000:
		meta.NetworkStatus = networkEmpty
		meta.NetworkReason = "片名或集数超出匹配范围"
	default:
		res := fetchSniffedDanmaku(basic.Danmaku, title, episode, true)
		items = append(items, res.Items...)
		meta.Network = len(res.Items)
		meta.NetworkStatus = res.Status
		meta.NetworkReason = res.Reason
		meta.NetworkCached = res.Cached
	}

	// 按出现时间升序排序
	sort.Slice(items, func(i, j int) bool {
		return items[i].Time < items[j].Time
	})

	respondDanmaku(c, items, meta, "获取弹幕成功")
}

// 全网弹幕状态
const (
	networkDisabled = "disabled" // 没开全网弹幕
	networkOK       = "ok"       // 拿到了全网弹幕
	networkEmpty    = "empty"    // 没匹配到片，或这一集没有弹幕
	networkError    = "error"    // 接口请求失败、超时、鉴权失败、返回无法解析
)

// danmakuMeta 弹幕列表的来源统计，挂在响应顶层的 meta 字段。
// data 仍是弹幕数组，老客户端（鸿蒙端等）不受影响。
type danmakuMeta struct {
	Local         int    `json:"local"`                   // 本站用户发送的条数
	Network       int    `json:"network"`                 // 全网弹幕条数
	NetworkStatus string `json:"networkStatus"`           // disabled | ok | empty | error
	NetworkReason string `json:"networkReason,omitempty"` // 给人看的原因，不含地址和密钥
	NetworkCached bool   `json:"networkCached"`           // 全网弹幕来自缓存
}

func respondDanmaku(c *gin.Context, items []model.DanmakuItem, meta danmakuMeta, msg string) {
	c.JSON(http.StatusOK, gin.H{
		"code": dto.SUCCESS,
		"data": items,
		"msg":  msg,
		"meta": meta,
	})
}

// PostDanmaku 发送弹幕入库
func (h *DanmakuHandler) PostDanmaku(c *gin.Context) {
	basic, _ := repobase.GetSiteBasic()
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
	if req.Text == "" || len([]rune(req.Text)) > 200 {
		dto.Failed("弹幕内容不能为空且不能超过200字", c)
		return
	}
	color, valErr := validateDanmakuPostInput(req.Mid, req.Time, req.Color, req.Mode)
	if valErr != "" {
		dto.Failed(valErr, c)
		return
	}
	req.Color = color
	if req.Episode <= 0 || req.Episode > 10000 {
		req.Episode = 1
	}
	if !allowDanmakuPost(c.ClientIP()) {
		dto.Failed("发送弹幕过于频繁，请稍后再试", c)
		return
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
		log.Printf("[Danmaku] 保存弹幕失败 mid=%d err=%v", req.Mid, err)
		dto.Failed("保存弹幕失败", c)
		return
	}
	dto.Success(nil, "发送成功", c)
}
