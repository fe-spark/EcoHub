package model

import (
	"strings"
	"testing"
)

func TestNormalizeTipConfigDefaults(t *testing.T) {
	got := NormalizeTipConfig(TipConfig{})
	if got.Enabled {
		t.Fatal("empty tip should stay disabled")
	}
	if got.Title != DefaultTipTitle {
		t.Fatalf("title = %q, want %q", got.Title, DefaultTipTitle)
	}
	if got.Message != DefaultTipMessage {
		t.Fatalf("message = %q, want %q", got.Message, DefaultTipMessage)
	}
	if len(got.Channels) != 2 {
		t.Fatalf("channels len = %d, want 2", len(got.Channels))
	}
	if got.Channels[0].Key != TipChannelWeChat || got.Channels[1].Key != TipChannelAlipay {
		t.Fatalf("preset channels = %+v", got.Channels)
	}
}

func TestNormalizeTipConfigDropsInvalidAndCaps(t *testing.T) {
	got := NormalizeTipConfig(TipConfig{
		Enabled: true,
		Title:   "  请喝咖啡  ",
		Channels: []TipChannel{
			{Key: "paypal", Label: "PayPal", Link: "https://example.com"},
			{Key: "custom", Label: "", QrImage: "", Link: ""},
			{Key: "wechat", Label: "  ", QrImage: " /qr-wechat.png ", Link: "not-a-url"},
			{Key: "alipay", Label: "支付宝", Link: "https://afdian.com/a"},
			{Key: "custom", Label: "爱发电", Link: "https://afdian.com/b"},
			{Key: "custom", Label: "第四条", Link: "https://example.com/d"},
			{Key: "custom", Label: "第五条应被截断", Link: "https://example.com/e"},
		},
	})
	if !got.Enabled {
		t.Fatal("enabled should stay true")
	}
	if got.Title != "请喝咖啡" {
		t.Fatalf("title = %q", got.Title)
	}
	if len(got.Channels) != 4 {
		t.Fatalf("channels len = %d, want 4; %+v", len(got.Channels), got.Channels)
	}
	if got.Channels[0].Key != TipChannelCustom || got.Channels[0].Label != "PayPal" {
		t.Fatalf("unknown key should become custom: %+v", got.Channels[0])
	}
	if got.Channels[1].Key != TipChannelWeChat || got.Channels[1].Label != "微信" {
		t.Fatalf("wechat channel = %+v", got.Channels[1])
	}
	if got.Channels[1].Link != "" {
		t.Fatalf("invalid wechat link should be dropped, got %q", got.Channels[1].Link)
	}
	if got.Channels[1].QrImage != "/qr-wechat.png" {
		t.Fatalf("qr = %q", got.Channels[1].QrImage)
	}
	if got.Channels[2].Link != "https://afdian.com/a" {
		t.Fatalf("alipay link = %q", got.Channels[2].Link)
	}
	last := got.Channels[len(got.Channels)-1]
	if last.Label != "爱发电" {
		t.Fatalf("last kept channel = %+v", last)
	}
}

func TestNormalizeTipLinkAndLimits(t *testing.T) {
	got := NormalizeTipConfig(TipConfig{
		Enabled: true,
		Title:   strings.Repeat("赞", MaxTipTitleLen+8),
		Message: strings.Repeat("文", MaxTipMessageLen+8),
		Channels: []TipChannel{
			{Key: "custom", Label: "爱发电", Link: "afdian.com/a"},
			{Key: "custom", Label: "坏协议", Link: "javascript:alert(1)"},
			{Key: "custom", Label: "相对协议", Link: "//evil.example"},
			{Key: "wechat", QrImage: "javascript:alert(1)"},
		},
	})
	if got.Title != strings.Repeat("赞", MaxTipTitleLen) {
		t.Fatalf("title len = %d, value %q", len([]rune(got.Title)), got.Title)
	}
	if got.Message != strings.Repeat("文", MaxTipMessageLen) {
		t.Fatalf("message len = %d", len([]rune(got.Message)))
	}
	if len(got.Channels) != 4 {
		t.Fatalf("channels = %+v", got.Channels)
	}
	if got.Channels[0].Link != "https://afdian.com/a" {
		t.Fatalf("bare domain should gain https: %q", got.Channels[0].Link)
	}
	if got.Channels[1].Link != "" {
		t.Fatalf("javascript link should drop, got %q", got.Channels[1].Link)
	}
	if got.Channels[2].Link != "" {
		t.Fatalf("protocol-relative link should drop, got %q", got.Channels[2].Link)
	}
	if got.Channels[3].QrImage != "" {
		t.Fatalf("javascript qr should drop, got %q", got.Channels[3].QrImage)
	}
}

func TestDecodeTipJSON(t *testing.T) {
	if got := DecodeTipJSON(""); got.Title != DefaultTipTitle {
		t.Fatalf("empty json title = %q", got.Title)
	}
	if got := DecodeTipJSON("{not-json"); got.Title != DefaultTipTitle {
		t.Fatalf("invalid json title = %q", got.Title)
	}
	raw := EncodeTipJSON(TipConfig{
		Enabled: true,
		Title:   "支持一下",
		Channels: []TipChannel{
			{Key: "wechat", QrImage: "https://cdn.example/wx.png"},
		},
	})
	if !strings.Contains(raw, "wechat") {
		t.Fatalf("encode missing wechat: %s", raw)
	}
	got := DecodeTipJSON(raw)
	if !got.Enabled || got.Title != "支持一下" || got.Channels[0].QrImage != "https://cdn.example/wx.png" {
		t.Fatalf("roundtrip = %+v", got)
	}
}
