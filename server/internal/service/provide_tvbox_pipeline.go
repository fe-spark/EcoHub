package service

import (
	"fmt"
	"net/url"
	"regexp"
	"sort"
	"strings"

	"server/internal/model"
)

var (
	reBrokenSchemeHttps = regexp.MustCompile(`(?i)^(?:.*?)(https://)`)
	reBrokenSchemeHttp  = regexp.MustCompile(`(?i)^(?:.*?)(http://)`)
	reTyposSchemeHttps  = regexp.MustCompile(`(?i)^(?:[^:/]*(?:htps|htpps|ttps)|(?:\d+p)?(?:tps|ps|s))://`)
	reTailJunkPunct     = regexp.MustCompile(`[$\s、。，,;；]+$`)
)

// SanitizeTVBoxPlayURL 专门针对 TVBox 终端清洗单集播放地址。
// 剔除前置修饰符、修正畸形协议头、修复常见编码错误、剥离尾部杂质字符。
func SanitizeTVBoxPlayURL(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	// 去除换行和制表符
	raw = strings.ReplaceAll(raw, "\r", "")
	raw = strings.ReplaceAll(raw, "\n", "")
	raw = strings.ReplaceAll(raw, "\t", "")

	// 1. 去除前置垃圾符号（常见有 `$http`, `#http`, `\http` 等）
	raw = strings.TrimLeft(raw, "$#\\/ \t")

	// 2. 协议头修复（已具备合法 http:// 或 https:// 前缀时不再向后截断）
	cleaned := raw
	hasHTTPPrefix := len(cleaned) >= 7 && strings.EqualFold(cleaned[:7], "http://")
	hasHTTPSPrefix := len(cleaned) >= 8 && strings.EqualFold(cleaned[:8], "https://")
	if !hasHTTPPrefix && !hasHTTPSPrefix {
		if loc := reBrokenSchemeHttps.FindStringSubmatchIndex(cleaned); loc != nil && !strings.Contains(cleaned[:loc[2]], "://") {
			// 截取从 https:// 开始的内容，修正诸如 "httphttps://", "$https://", "720Phttps://"
			cleaned = "https://" + cleaned[loc[3]:]
		} else if loc := reBrokenSchemeHttp.FindStringSubmatchIndex(cleaned); loc != nil && !strings.Contains(cleaned[:loc[2]], "://") {
			// 截取从 http:// 开始的内容，修正诸如 "hthttp://", "720Phttp://"
			cleaned = "http://" + cleaned[loc[3]:]
		} else if loc := reTyposSchemeHttps.FindStringIndex(cleaned); loc != nil {
			// 修正诸如 "htps://", "tps://", "s://", "720Phtps://"
			cleaned = "https://" + cleaned[loc[1]:]
		}
	}

	// 必须具备合法的 http/https 协议
	if !strings.HasPrefix(strings.ToLower(cleaned), "http://") && !strings.HasPrefix(strings.ToLower(cleaned), "https://") {
		return ""
	}

	// 3. 剥离尾部非法符号（如 .m3u8$ / .m3u8、、、 / .m3u8。）
	cleaned = reTailJunkPunct.ReplaceAllString(cleaned, "")

	// 4. 基础合法性检验：能够解析为标准 URL
	parsed, err := url.Parse(cleaned)
	if err != nil || parsed.Host == "" {
		return ""
	}

	// 5. 将 URL 内部可能干扰 TVBox 协议拆分的 $ 与 # 进行安全替换
	// 避免在 TVBox 中破坏「集名$链接」或「集与集#」的边界
	cleaned = strings.NewReplacer("$", "%24", "#", "%23").Replace(cleaned)

	return cleaned
}

// SanitizeTVBoxEpisode 针对 TVBox 终端清洗剧集名称。
func SanitizeTVBoxEpisode(raw string, defaultIndex int) string {
	ep := strings.TrimSpace(raw)
	ep = strings.ReplaceAll(ep, "\r", "")
	ep = strings.ReplaceAll(ep, "\n", "")
	ep = strings.ReplaceAll(ep, "\t", "")

	// 将 $ 与 # 替换为全角字符，防止干扰 TVBox 分隔符解析
	ep = strings.NewReplacer("$", "＄", "#", "＃").Replace(ep)
	if ep == "" {
		if defaultIndex >= 0 {
			return fmt.Sprintf("第%d集", defaultIndex+1)
		}
		return "正片"
	}
	return ep
}

// isRisk403Source 判断是否为已知经常触发 403 / 区域拦截的高风险源
func isRisk403Source(sourceID string) bool {
	switch sourceID {
	case "2898990914", "4194624554": // 非凡(FF)、U酷(UKU) 等常见机房/终端阻断源
		return true
	default:
		return false
	}
}

// SortTVBoxPlaySources TVBox 专属保活排序策略。
// 区别于 Web 端依靠客户端并发探活（playSourceProbe），TVBox 盲信服务端第 1 条线路。
// 优先保障排在首位的线路具备极高的直接可用性，防止进入播放页直接黑屏。
func SortTVBoxPlaySources(sources []model.PlayLinkVo) []model.PlayLinkVo {
	if len(sources) <= 1 {
		return sources
	}

	cloned := append([]model.PlayLinkVo(nil), sources...)

	// 计算权重：基础优先级 + 存活与特征加权
	getWeight := func(vo model.PlayLinkVo) int {
		score := 0
		if vo.IsManual || vo.SourceId == "manual" {
			score += 1000000 // 自定义手动线路最优先
		}

		// 已知高风险 403 阻断源扣分，在有多线路时不占据首位
		if isRisk403Source(vo.SourceId) {
			score -= 50000
		} else {
			score += 10000
		}

		// 有效集数加权
		if len(vo.LinkList) > 0 {
			score += 1000
			firstLink := strings.ToLower(vo.LinkList[0].Link)
			// 首集为 https 格式加权
			if strings.HasPrefix(firstLink, "https://") {
				score += 500
			}
			// 首集为标准 m3u8/mp4 加权
			if strings.Contains(firstLink, ".m3u8") || strings.Contains(firstLink, ".mp4") {
				score += 500
			}
		}

		return score
	}

	sort.SliceStable(cloned, func(i, j int) bool {
		wi, wj := getWeight(cloned[i]), getWeight(cloned[j])
		if wi != wj {
			return wi > wj // 得分高的排前面
		}
		return false
	})

	return cloned
}

// FormatTVBoxFilmPlayLists 组装 TVBox 专用的 vod_play_from 与 vod_play_url。
// 过滤无效空线路，深度清洗每一集，执行专属保活重排。
func FormatTVBoxFilmPlayLists(sources []model.PlayLinkVo) (string, string) {
	if len(sources) == 0 {
		return "", ""
	}

	// 1. 深度清洗各线路链接，过滤无可用链接的空线路
	validSources := make([]model.PlayLinkVo, 0, len(sources))
	for _, src := range sources {
		cleanedLinks := make([]model.MovieUrlInfo, 0, len(src.LinkList))
		for idx, linkItem := range src.LinkList {
			urlCleaned := SanitizeTVBoxPlayURL(linkItem.Link)
			if urlCleaned == "" {
				continue
			}
			epCleaned := SanitizeTVBoxEpisode(linkItem.Episode, idx)
			cleanedLinks = append(cleanedLinks, model.MovieUrlInfo{
				Episode:    epCleaned,
				Link:       urlCleaned,
				SourceId:   linkItem.SourceId,
				SourceName: linkItem.SourceName,
			})
		}
		// 若整条线路清洗后没有一条有效播放地址，剔除该线路，绝不下发空线路给 TVBox
		if len(cleanedLinks) > 0 {
			src.LinkList = cleanedLinks
			validSources = append(validSources, src)
		}
	}

	if len(validSources) == 0 {
		return "", ""
	}

	// 2. TVBox 专属首线保活与可用性排序
	sortedSources := SortTVBoxPlaySources(validSources)

	// 3. 序列化为 TVBox 标准规范格式
	playFromList := make([]string, 0, len(sortedSources))
	playUrlList := make([]string, 0, len(sortedSources))

	for _, s := range sortedSources {
		playFromList = append(playFromList, strings.TrimSpace(s.Name))
		var epStrs []string
		for _, ep := range s.LinkList {
			epStrs = append(epStrs, ep.Episode+"$"+ep.Link)
		}
		playUrlList = append(playUrlList, strings.Join(epStrs, "#"))
	}

	return strings.Join(playFromList, "$$$"), strings.Join(playUrlList, "$$$")
}
