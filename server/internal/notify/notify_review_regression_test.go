package notify

import (
	"context"
	"strings"
	"testing"
	"time"
)

func TestValidateBotToken(t *testing.T) {
	valid := []string{"", "123456:ABC-DEF_1234567890"}
	for _, tok := range valid {
		if err := ValidateBotToken(tok); err != nil {
			t.Fatalf("合法 Token %q 不应报错: %v", tok, err)
		}
	}
	invalid := []string{
		"123:abc/def",
		"123:abc?x=1",
		"123:abc#frag",
		"123:abc\ndef",
		"123:abc def",
	}
	for _, tok := range invalid {
		if err := ValidateBotToken(tok); err == nil {
			t.Fatalf("非法 Token %q 应被拒绝", tok)
		}
	}
}

func TestCallTelegramJSON_RedactsTokenOnNewRequestError(t *testing.T) {
	origBase := tgAPIBaseURL
	// 使用含控制字符的 BaseURL 触发 http.NewRequestWithContext 返回 *url.Error
	tgAPIBaseURL = "https://api.telegram.org\x7f"
	defer func() { tgAPIBaseURL = origBase }()

	secretToken := "999888:SUPER_SECRET_BOT_TOKEN"
	_, err := callTelegramJSON(context.Background(), secretToken, "sendMessage", map[string]any{"text": "hi"}, time.Second)
	if err == nil {
		t.Fatal("非法 BaseURL 应返回错误")
	}
	if strings.Contains(err.Error(), secretToken) {
		t.Fatalf("错误信息泄露了 Bot Token: %v", err)
	}
}
