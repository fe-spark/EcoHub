package model

import (
	"encoding/json"
	"strings"
	"unicode/utf8"
)

func defaultTipLabel(key string) string {
	switch key {
	case TipChannelWeChat:
		return "微信"
	case TipChannelAlipay:
		return "支付宝"
	default:
		return "自定义"
	}
}

func truncateRunes(raw string, max int) string {
	if max <= 0 || utf8.RuneCountInString(raw) <= max {
		return raw
	}
	return string([]rune(raw)[:max])
}

func normalizeTipLink(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") {
		return truncateRunes(raw, MaxTipLinkLen)
	}
	if strings.Contains(raw, ":") || strings.HasPrefix(raw, "//") {
		return ""
	}
	if strings.Contains(raw, ".") && !strings.Contains(raw, " ") {
		return truncateRunes("https://"+strings.TrimLeft(raw, "/"), MaxTipLinkLen)
	}
	return ""
}

func normalizeTipImage(raw string) string {
	raw = strings.TrimSpace(raw)
	if raw == "" {
		return ""
	}
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") || strings.HasPrefix(lower, "https://") || strings.HasPrefix(raw, "/") {
		return truncateRunes(raw, MaxTipImageLen)
	}
	return ""
}

func isTipChannelKey(key string) bool {
	return key == TipChannelWeChat || key == TipChannelAlipay || key == TipChannelCustom
}

func normalizeTipChannelKey(raw string) string {
	key := strings.TrimSpace(raw)
	if isTipChannelKey(key) {
		return key
	}
	return TipChannelCustom
}

// NormalizeTipConfig 清洗赞赏配置：补默认文案、限制渠道数、丢弃无效项。
func NormalizeTipConfig(tip TipConfig) TipConfig {
	title := strings.TrimSpace(tip.Title)
	if title == "" {
		title = DefaultTipTitle
	}
	title = truncateRunes(title, MaxTipTitleLen)
	message := strings.TrimSpace(tip.Message)
	if message == "" {
		message = DefaultTipMessage
	}
	message = truncateRunes(message, MaxTipMessageLen)

	channels := make([]TipChannel, 0, len(tip.Channels))
	for _, ch := range tip.Channels {
		key := normalizeTipChannelKey(ch.Key)
		rawLabel := strings.TrimSpace(ch.Label)
		label := rawLabel
		if label == "" {
			label = defaultTipLabel(key)
		}
		label = truncateRunes(label, MaxTipLabelLen)
		qr := normalizeTipImage(ch.QrImage)
		link := normalizeTipLink(ch.Link)
		if qr == "" && link == "" && key == TipChannelCustom && rawLabel == "" {
			continue
		}
		channels = append(channels, TipChannel{
			Key:     key,
			Label:   label,
			QrImage: qr,
			Link:    link,
		})
		if len(channels) >= MaxTipChannels {
			break
		}
	}
	if len(channels) == 0 {
		channels = DefaultTipConfig().Channels
	}

	return TipConfig{
		Enabled:  tip.Enabled,
		Title:    title,
		Message:  message,
		Channels: channels,
	}
}

// EncodeTipJSON 将赞赏配置编码为持久化 JSON。
func EncodeTipJSON(tip TipConfig) string {
	raw, err := json.Marshal(NormalizeTipConfig(tip))
	if err != nil {
		return ""
	}
	return string(raw)
}

// DecodeTipJSON 解析赞赏配置 JSON，非法或空值回退默认。
func DecodeTipJSON(raw string) TipConfig {
	if strings.TrimSpace(raw) == "" {
		return DefaultTipConfig()
	}
	var tip TipConfig
	if err := json.Unmarshal([]byte(raw), &tip); err != nil {
		return DefaultTipConfig()
	}
	return NormalizeTipConfig(tip)
}
