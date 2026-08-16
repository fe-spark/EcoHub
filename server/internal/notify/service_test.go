package notify

import (
	"fmt"
	"strings"
	"testing"
	"time"
	"unicode/utf8"

	"server/internal/model"
	"server/internal/repository"
)

func TestIsTelegramGetUpdatesConflict(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("telegram api: Conflict: terminated by other getUpdates request; make sure that only one bot instance is running"), true},
		{fmt.Errorf("CONFLICT: terminated by other getUpdates request"), true},
		// webhook 未清除属另一类错误，不应误判为多实例冲突
		{fmt.Errorf("Conflict: can't use getUpdates method while webhook is active"), false},
		{fmt.Errorf("telegram api: Unauthorized"), false},
		{fmt.Errorf("connection timeout"), false},
	}
	for _, tc := range cases {
		if got := isTelegramGetUpdatesConflict(tc.err); got != tc.want {
			t.Fatalf("err=%v got=%v want=%v", tc.err, got, tc.want)
		}
	}
}

func TestIsTelegramWebhookActiveError(t *testing.T) {
	cases := []struct {
		err  error
		want bool
	}{
		{nil, false},
		{fmt.Errorf("Conflict: can't use getUpdates method while webhook is active"), true},
		{fmt.Errorf("CONFLICT: CAN'T USE GETUPDATES METHOD WHILE WEBHOOK IS ACTIVE"), true},
		// 多实例冲突不应误判为 webhook 错误
		{fmt.Errorf("telegram api: Conflict: terminated by other getUpdates request; make sure that only one bot instance is running"), false},
		{fmt.Errorf("telegram api: Unauthorized"), false},
	}
	for _, tc := range cases {
		if got := isTelegramWebhookActiveError(tc.err); got != tc.want {
			t.Fatalf("err=%v got=%v want=%v", tc.err, got, tc.want)
		}
	}
}

func TestMaskBotToken(t *testing.T) {
	if MaskBotToken("") != "" {
		t.Fatal("empty token")
	}
	masked := MaskBotToken("123456:ABCDEFGHIJKLMNOP")
	if !strings.Contains(masked, "***") {
		t.Fatalf("expected mask, got %s", masked)
	}
	if strings.Contains(masked, "ABCDEF") {
		t.Fatalf("should not leak middle: %s", masked)
	}
	if !IsMaskedToken(masked) {
		t.Fatalf("masked form should be detected: %s", masked)
	}
	if IsMaskedToken("123456:ABCDEFGHIJKLMNOP") {
		t.Fatal("real token should not be treated as masked")
	}
	if IsMaskedToken("foo***bar") {
		t.Fatal("arbitrary *** should not match mask shape")
	}
}

func TestSanitizeTelegramErr(t *testing.T) {
	token := "1234567890:AAFakeTokenForUnitTestOnly_xyz"
	raw := fmt.Errorf(`Post "https://api.telegram.org/bot%s/sendMessage": context deadline exceeded`, token)
	got := sanitizeTelegramErr(raw, token, "").Error()
	if strings.Contains(got, token) {
		t.Fatalf("token leaked: %s", got)
	}
	if strings.Contains(got, "AAFakeToken") {
		t.Fatalf("token fragment leaked: %s", got)
	}
	if !strings.Contains(got, "TG_PROXY") {
		t.Fatalf("expected proxy hint: %s", got)
	}
	got2 := sanitizeTelegramErr(raw, token, "http://127.0.0.1:7890").Error()
	if !strings.Contains(got2, "7890") || strings.Contains(got2, token) {
		t.Fatalf("proxy path: %s", got2)
	}
}

func TestValidateAndMergeUpdateKeepToken(t *testing.T) {
	old := model.NotifyConfig{
		BotToken: "123456:REALTOKENVALUE",
		ChatIDs:  []string{"111"},
	}
	incoming := model.NotifyConfig{
		Enabled:  true,
		BotToken: MaskBotToken(old.BotToken),
		ChatIDs:  []string{"111", "222"},
		Events: model.NotifyEventSwitches{
			CollectBatchSummary: true,
		},
		MaxFilmsInMessage: 15,
		MinIntervalSec:    60,
	}
	merged, err := ValidateAndMergeUpdate(old, incoming)
	if err != nil {
		t.Fatal(err)
	}
	if merged.BotToken != old.BotToken {
		t.Fatalf("token should be preserved, got %s", merged.BotToken)
	}
	if len(merged.ChatIDs) != 2 {
		t.Fatalf("chat ids: %v", merged.ChatIDs)
	}
	// Targets 应与 ChatIDs 对齐
	if len(merged.Targets) != 2 {
		t.Fatalf("targets should match chatIDs: %+v", merged.Targets)
	}
}

func TestValidateAndMergeUpdateSyncsTargetsWithChatIDs(t *testing.T) {
	old := model.NotifyConfig{
		BotToken: "123456:REALTOKENVALUE",
		ChatIDs:  []string{"A"},
		Targets: []model.NotifyTarget{
			{ChatID: "A", ThreadID: "10", Enabled: true, MinLevel: model.SeverityError, Name: "主题A"},
		},
	}
	// 前端只改 chatIds，不传 targets（或仍带陈旧 A）
	incoming := model.NotifyConfig{
		Enabled:           false,
		ChatIDs:           []string{"C"},
		Targets:           []model.NotifyTarget{{ChatID: "A", Enabled: true}}, // 陈旧
		MaxFilmsInMessage: 15,
		MinIntervalSec:    60,
	}
	merged, err := ValidateAndMergeUpdate(old, incoming)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged.ChatIDs) != 1 || merged.ChatIDs[0] != "C" {
		t.Fatalf("chatIDs: %v", merged.ChatIDs)
	}
	if len(merged.Targets) != 1 || merged.Targets[0].ChatID != "C" {
		t.Fatalf("stale target A must be dropped: %+v", merged.Targets)
	}

	// 保留仍在成员列表中的 Thread 元数据
	incoming2 := model.NotifyConfig{
		Enabled:           false,
		ChatIDs:           []string{"A"},
		MaxFilmsInMessage: 15,
		MinIntervalSec:    60,
	}
	merged2, err := ValidateAndMergeUpdate(old, incoming2)
	if err != nil {
		t.Fatal(err)
	}
	if len(merged2.Targets) != 1 || merged2.Targets[0].ThreadID != "10" || merged2.Targets[0].MinLevel != model.SeverityError {
		t.Fatalf("should preserve thread meta for A: %+v", merged2.Targets)
	}
}

func TestChatAllowedUsesEffectiveTargets(t *testing.T) {
	cfg := model.NotifyConfig{
		// ChatIDs 与 Targets 故意不一致：发送/鉴权应以 Targets 为准（effectiveTargets）
		ChatIDs: []string{"-100111"},
		Targets: []model.NotifyTarget{
			{ChatID: "-100999", Enabled: true},
			{ChatID: "@mychannel", Enabled: true},
			{ChatID: "-100000", Enabled: false},
		},
	}
	if !chatAllowed(cfg, "-100999", "") {
		t.Fatal("enabled target chat should allow")
	}
	if !chatAllowed(cfg, "-100456", "mychannel") {
		t.Fatal("username should match target")
	}
	if chatAllowed(cfg, "-100111", "") {
		t.Fatal("chat only in ChatIDs but not in Targets should not allow when Targets set")
	}
	if chatAllowed(cfg, "-100000", "") {
		t.Fatal("disabled target should not allow")
	}
}

func TestValidateChatID(t *testing.T) {
	old := model.NotifyConfig{}
	// 禁用状态下不校验 Chat ID 格式（允许保存残留无效 ID，便于清理配置）
	_, err := ValidateAndMergeUpdate(old, model.NotifyConfig{
		Enabled:           false,
		ChatIDs:           []string{"not valid!"},
		MaxFilmsInMessage: 15,
	})
	if err != nil {
		t.Fatalf("disabled config should skip chat id validation: %v", err)
	}
	// 启用状态下无效 Chat ID 必须报错
	_, err = ValidateAndMergeUpdate(old, model.NotifyConfig{
		Enabled:           true,
		ChatIDs:           []string{"not valid!"},
		MaxFilmsInMessage: 15,
	})
	if err == nil {
		t.Fatal("expected invalid chat id error when enabled")
	}
	cfg, err := ValidateAndMergeUpdate(old, model.NotifyConfig{
		Enabled:           false,
		ChatIDs:           []string{"-100123", "@mychannel"},
		MaxFilmsInMessage: 15,
	})
	if err != nil {
		t.Fatal(err)
	}
	if len(cfg.ChatIDs) != 2 {
		t.Fatalf("got %v", cfg.ChatIDs)
	}
}

func TestValidateMaxFilmsInMessageCap(t *testing.T) {
	old := model.NotifyConfig{}
	_, err := ValidateAndMergeUpdate(old, model.NotifyConfig{
		Enabled:           false,
		MaxFilmsInMessage: 21,
	})
	if err == nil {
		t.Fatal("expected maxFilmsInMessage over cap to fail")
	}
	cfg, err := ValidateAndMergeUpdate(old, model.NotifyConfig{
		Enabled:           false,
		MaxFilmsInMessage: 0,
	})
	if err != nil {
		t.Fatal(err)
	}
	if cfg.MaxFilmsInMessage != 15 {
		t.Fatalf("default want 15, got %d", cfg.MaxFilmsInMessage)
	}
}

func TestNormalizeChatIDsViaRepo(t *testing.T) {
	got := repository.NormalizeChatIDs([]string{" 111 ", "", "111", "222"})
	if len(got) != 2 || got[0] != "111" || got[1] != "222" {
		t.Fatalf("got %v", got)
	}
	if len(repository.NormalizeChatIDs(nil)) != 0 {
		t.Fatal("nil should yield empty slice")
	}
}

func TestMidAccumulatorCountsOnly(t *testing.T) {
	acc := NewMidAccumulator()
	// 无 DB 时 batch 为 nil，落库为空操作，计数仍工作
	acc.Add(nil, "src1", "源1", 1, 2, 2, 3)
	_, total, _ := acc.DrainSource("src1")
	if total != 3 {
		t.Fatalf("total=%d", total)
	}
	_, total2, _ := acc.DrainSource("src1")
	if total2 != 0 {
		t.Fatalf("after drain total=%d", total2)
	}
}

func TestFormatBatchOverview(t *testing.T) {
	payload := model.CollectBatchNotifyPayload{
		Trigger:        model.NotifyTriggerManual,
		SiteName:       "测试站",
		DurationSec:    90,
		SuccessSources: 1,
		FailedSources:  1,
		TotalFilms:     2,
		Sources: []model.SourceNotifyResult{
			{
				SourceName: "主站A",
				Grade:      0,
				Status:     "done",
				SuccessCnt: 2,
				FilmsTotal: 2,
			},
			{
				SourceName: "附属B",
				Grade:      1,
				Status:     "failed",
				Error:      "timeout <script>",
				FailedCnt:  3,
			},
		},
	}
	joined := formatBatchOverview(payload, 2, 15)
	if !strings.Contains(joined, "测试站") {
		t.Fatalf("missing site name: %s", joined)
	}
	if !strings.Contains(joined, "变更") || !strings.Contains(joined, "失败") || !strings.Contains(joined, "成功页") {
		t.Fatalf("expected source metrics: %s", joined)
	}
	if !strings.Contains(joined, "更新列表") {
		t.Fatalf("expected film list hint: %s", joined)
	}
	if strings.Contains(joined, "<script>") {
		t.Fatal("should escape html")
	}
	if !strings.Contains(joined, "timeout") {
		t.Fatal("missing error")
	}
	if strings.Contains(joined, "<pre>") {
		t.Fatalf("overview must not use pre/code block: %s", joined)
	}
	if !strings.Contains(joined, "主站A") || !strings.Contains(joined, "合计") {
		t.Fatalf("expected source list + total: %s", joined)
	}
}

func TestParsePageCallback(t *testing.T) {
	prev := sitePlayBaseURLFn
	sitePlayBaseURLFn = func() string { return "" }
	t.Cleanup(func() { sitePlayBaseURLFn = prev })

	sid, page, catIdx, cat, kind, ok := parsePagedCallback(callbackPrefix, "nfp:abc123:2")
	if !ok || sid != "abc123" || page != 2 || kind != "page" || cat != "" || catIdx != catIdxAll {
		t.Fatalf("parse page: %v %v %v %v %v %v", sid, page, catIdx, cat, kind, ok)
	}
	_, _, catIdx, _, kind, ok = parsePagedCallback(callbackPrefix, "nfp:abc123:open")
	if !ok || kind != "open" || catIdx != catIdxAll {
		t.Fatalf("parse open: %v %v %v", kind, catIdx, ok)
	}
	// 新格式：分类用下标，避免 callback_data 超长
	_, _, catIdx, _, kind, ok = parsePagedCallback(callbackPrefix, "nfp:abc123:openc1")
	if !ok || kind != "open" || catIdx != 1 {
		t.Fatalf("parse openc1: kind=%v catIdx=%v ok=%v", kind, catIdx, ok)
	}
	_, page, catIdx, _, kind, ok = parsePagedCallback(callbackPrefix, "nfp:abc123:3c0")
	if !ok || kind != "page" || page != 3 || catIdx != 0 {
		t.Fatalf("parse 3c0: page=%v catIdx=%v kind=%v ok=%v", page, catIdx, kind, ok)
	}
	// 兼容旧消息：open_{名称}
	_, _, catIdx, cat, kind, ok = parsePagedCallback(callbackPrefix, "nfp:abc123:open_动漫")
	if !ok || kind != "open" || cat != "动漫" || catIdx != catIdxAll {
		t.Fatalf("parse open_动漫: kind=%v cat=%v catIdx=%v ok=%v", kind, cat, catIdx, ok)
	}
	_, _, _, _, kind, ok = parsePagedCallback(callbackPrefix, "nfp:abc123:back")
	if !ok || kind != "back" {
		t.Fatalf("parse back: %v %v", kind, ok)
	}
	if got := formatOpenCallback(callbackPrefix, "abc123", 2); got != "nfp:abc123:openc2" {
		t.Fatalf("formatOpenCallback: %s", got)
	}
	if got := formatPageCallback(callbackPrefix, "abc123", 4, 1); got != "nfp:abc123:4c1" {
		t.Fatalf("formatPageCallback: %s", got)
	}
	ov := buildOverviewKeyboard("abc123")
	if ov == nil || len(ov.InlineKeyboard) != 1 || !strings.Contains(ov.InlineKeyboard[0][0].Text, "更新列表") {
		t.Fatalf("overview keyboard: %+v", ov)
	}
	kb := buildPagedKeyboard(callbackPrefix, "abc123", 2, 3, true)
	if kb == nil || len(kb.InlineKeyboard) < 2 {
		t.Fatalf("keyboard: %+v", kb)
	}
}

func TestIsAllowedChatUsernameMatch(t *testing.T) {
	// 无 Targets 时 effectiveTargets 由 ChatIDs 包装
	cfg := model.NotifyConfig{ChatIDs: []string{"-100123", "@mychannel"}}
	if !chatAllowed(cfg, "-100123", "") {
		t.Fatal("numeric chat id should match")
	}
	if !chatAllowed(cfg, "-100456", "mychannel") {
		t.Fatal("@username should match via chat username")
	}
	if !chatAllowed(cfg, "-100456", "@mychannel") {
		t.Fatal("@username with @ prefix should match")
	}
	if chatAllowed(cfg, "-100789", "") {
		t.Fatal("unknown chat should be rejected")
	}
	if chatAllowed(cfg, "-100789", "other") {
		t.Fatal("unknown username should be rejected")
	}
}

func TestParseBotCommand(t *testing.T) {
	cmd, args := parseBotCommand("/search 流浪地球")
	if cmd != "search" || args != "流浪地球" {
		t.Fatalf("got %q %q", cmd, args)
	}
	cmd, args = parseBotCommand("/search@MyBot 关键词")
	if cmd != "search" || args != "关键词" {
		t.Fatalf("got %q %q", cmd, args)
	}
	cmd, args = parseBotCommand("hello")
	if cmd != "" || args != "" {
		t.Fatalf("non-cmd: %q %q", cmd, args)
	}
	cmd, args = parseBotCommand("/daily")
	if cmd != "daily" || args != "" {
		t.Fatalf("daily: %q %q", cmd, args)
	}
	cmd, args = parseBotCommand("/updates")
	if cmd != "updates" || args != "" {
		t.Fatalf("updates: %q %q", cmd, args)
	}

	// 群聊客户端常把 @bot 追加在关键词后，或插在指令与关键词之间
	cmd, args = parseBotCommand("/search 仙逆 @fe_spark_eco_server_bot")
	if cmd != "search" || args != "仙逆" {
		t.Fatalf("trailing mention: %q %q", cmd, args)
	}
	cmd, args = parseBotCommand("/search @fe_spark_eco_server_bot 仙逆")
	if cmd != "search" || args != "仙逆" {
		t.Fatalf("leading mention: %q %q", cmd, args)
	}
	cmd, args = parseBotCommand("/search 仙逆@fe_spark_eco_server_bot")
	if cmd != "search" || args != "仙逆" {
		t.Fatalf("glued mention: %q %q", cmd, args)
	}
	cmd, args = parseBotCommand("/search@fe_spark_eco_server_bot 仙逆 @fe_spark_eco_server_bot")
	if cmd != "search" || args != "仙逆" {
		t.Fatalf("head+tail mention: %q %q", cmd, args)
	}
	cmd, args = parseBotCommand("/daily @fe_spark_eco_server_bot")
	if cmd != "daily" || args != "" {
		t.Fatalf("daily mention-only args: %q %q", cmd, args)
	}
	cmd, args = parseBotCommand("/search 仙逆 第一季")
	if cmd != "search" || args != "仙逆 第一季" {
		t.Fatalf("multi-word keyword: %q %q", cmd, args)
	}
}

func TestRolling24hWindow(t *testing.T) {
	loc := notifyCST()
	now := time.Date(2026, 8, 13, 10, 0, 0, 0, loc)
	from, to := Rolling24hWindow(now)
	if !to.Equal(now) {
		t.Fatalf("to should be query time, got %v", to)
	}
	wantFrom := now.Add(-24 * time.Hour)
	if !from.Equal(wantFrom) {
		t.Fatalf("from want %v got %v", wantFrom, from)
	}
	// 23:59 的更新在次日上午仍落在窗内
	at2359 := time.Date(2026, 8, 12, 23, 59, 0, 0, loc)
	if at2359.Before(from) || at2359.After(to) {
		t.Fatalf("23:59 yesterday should be inside window [%v, %v]", from, to)
	}
}

func TestMenuButtonAndReplyKeyboard(t *testing.T) {
	if !isMenuButtonText(btnDailyUpdate) || !isMenuButtonText(btnSearchQuery) || !isMenuButtonText(btnHelp) {
		t.Fatal("menu buttons should be recognized")
	}
	if isMenuButtonText("随便说") || isMenuButtonText("/search") {
		t.Fatal("non-menu text should not match")
	}
	kb := mainReplyKeyboard()
	if kb == nil || len(kb.Keyboard) != 2 {
		t.Fatalf("keyboard rows: %+v", kb)
	}
	if kb.Keyboard[0][0].Text != btnDailyUpdate || kb.Keyboard[0][1].Text != btnSearchQuery {
		t.Fatalf("row0: %+v", kb.Keyboard[0])
	}
	if kb.Keyboard[1][0].Text != btnHelp {
		t.Fatalf("row1: %+v", kb.Keyboard[1])
	}
	if !kb.ResizeKeyboard || !kb.IsPersistent {
		t.Fatalf("keyboard flags: resize=%v persistent=%v", kb.ResizeKeyboard, kb.IsPersistent)
	}
	if replyMarkupPayload(kb) == nil {
		t.Fatal("reply keyboard payload should be non-nil")
	}
	if replyMarkupPayload((*InlineKeyboardMarkup)(nil)) != nil {
		t.Fatal("nil inline markup should yield nil payload")
	}
	if replyMarkupPayload(&InlineKeyboardMarkup{}) != nil {
		t.Fatal("empty inline markup should yield nil payload")
	}
	if !isPrivateChat("private") || !isPrivateChat("PRIVATE") {
		t.Fatal("private chat should match")
	}
	if isPrivateChat("group") || isPrivateChat("supergroup") || isPrivateChat("") {
		t.Fatal("non-private should not get reply keyboard")
	}
	if menuKeyboardForChat("private") == nil {
		t.Fatal("private chat should receive reply keyboard")
	}
	if menuKeyboardForChat("supergroup") != nil || menuKeyboardForChat("group") != nil {
		t.Fatal("group chats should not receive reply keyboard")
	}
}

func TestSetSearchAwaitRequiresRedis(t *testing.T) {
	if err := setSearchAwait("123", 1); err == nil {
		t.Fatal("expected error when redis is unavailable")
	}
}

func TestFormatDailyCategoryPrompt(t *testing.T) {
	got := formatDailyCategoryPrompt(dailySession{
		SiteName:   "测试站",
		AllMids:    []int64{1, 2, 3},
		FromLabel:  "08-13 10:00",
		UntilLabel: "08-14 10:00",
		Cats: []CategoryCountItem{{
			CategoryName: "电影",
			Count:        2,
		}},
	})
	if !strings.Contains(got, "每日更新") || !strings.Contains(got, "08-13 10:00") {
		t.Fatalf("prompt: %s", got)
	}
	if strings.Contains(got, "电影") || strings.Contains(got, "部") {
		t.Fatalf("prompt should not list category or count: %s", got)
	}
}

func TestFormatDailyListPageTitle(t *testing.T) {
	sess := dailySession{SiteName: "测试站", PageSize: 10}
	got := formatDailyListPage(sess, catIdxAll, 1, nil, 0, 0, 0)
	if !strings.Contains(got, "每日更新列表") {
		t.Fatalf("want 每日更新列表, got %s", got)
	}
	if strings.Contains(got, "本次更新列表") {
		t.Fatalf("should not use collect-batch title: %s", got)
	}
	collect := formatFilmListPageWithChunkCategory(FilmPageSession{SiteName: "测试站"}, 1, nil, 0, 0, 0, "", "")
	if !strings.Contains(collect, "本次更新列表") {
		t.Fatalf("collect list should keep default title: %s", collect)
	}
}

func TestFormatFilmLine(t *testing.T) {
	prev := sitePlayBaseURLFn
	t.Cleanup(func() { sitePlayBaseURLFn = prev })

	sitePlayBaseURLFn = func() string { return "" }
	line := formatFilmLine(model.FilmNotifyItem{Mid: 42, Name: "测试片"})
	if !strings.Contains(line, "测试片") || !strings.Contains(line, "#42") {
		t.Fatalf("unexpected film line: %s", line)
	}
	if strings.Contains(line, "href=") {
		t.Fatalf("empty siteUrl should not link: %s", line)
	}

	sitePlayBaseURLFn = func() string { return "https://demo.example.com" }
	linked := formatFilmLine(model.FilmNotifyItem{Mid: 42, Name: "测试片", SourceName: "红牛资源"})
	if !strings.Contains(linked, `href="https://demo.example.com/play?id=42"`) {
		t.Fatalf("expected play link: %s", linked)
	}
	if !strings.Contains(linked, ">测试片<") {
		t.Fatalf("name should be link text: %s", linked)
	}
	if !strings.Contains(linked, "[红牛资源]") {
		t.Fatalf("expected source name suffix: %s", linked)
	}
}

func TestFormatProgressStale(t *testing.T) {
	msgs := formatProgressStale("站", "源A", "sid", "age=1m", time.Now())
	joined := strings.Join(msgs, "\n")
	if !strings.Contains(joined, "采集进度超时") {
		t.Fatalf("expected stale title: %s", joined)
	}
}

func TestBuildBatchPayloadCountsStopped(t *testing.T) {
	payload := BuildBatchPayload(nil, model.NotifyTriggerManual, []model.SourceNotifyResult{
		{Status: "done", FilmsTotal: 1},
		{Status: "stopped"},
		{Status: "failed"},
	}, time.Now().Add(-time.Minute), time.Now(), "")
	if payload.SuccessSources != 1 || payload.FailedSources != 2 {
		t.Fatalf("success=%d failed=%d", payload.SuccessSources, payload.FailedSources)
	}
}

func TestRateLimiter(t *testing.T) {
	r := &rateLimiter{last: make(map[string]time.Time)}
	if !r.allow("k", time.Minute) {
		t.Fatal("first should allow")
	}
	if r.allow("k", time.Minute) {
		t.Fatal("second should block")
	}
	if !r.allow("other", time.Minute) {
		t.Fatal("other key should allow")
	}
}

func TestFormatSearchListPage(t *testing.T) {
	prev := sitePlayBaseURLFn
	sitePlayBaseURLFn = func() string { return "" }
	t.Cleanup(func() { sitePlayBaseURLFn = prev })

	mids := make([]int64, 0, 15)
	for i := 1; i <= 15; i++ {
		mids = append(mids, int64(i))
	}
	sess := searchSession{
		SiteName: "搜站",
		PageSize: 10,
		Mids:     mids,
		Keyword:  "片",
		HitTotal: 15,
	}
	p1 := formatSearchListPage(sess, 1)
	if !strings.Contains(p1, "1/2") || !strings.Contains(p1, "#1") {
		t.Fatalf("page1: %s", p1)
	}
	kb := buildPagedKeyboard(searchCallbackPrefix, "sid", 1, 2, false)
	if kb == nil || len(kb.InlineKeyboard) != 1 || !strings.Contains(kb.InlineKeyboard[0][2].Text, "下一页") {
		t.Fatalf("search kb: %+v", kb)
	}
}

func TestFormatSourceConfigsChangedPaging(t *testing.T) {
	// 构造足够多条目，确保超过 4096 触发分页
	items := make([]SourceConfigChangeItem, 0, 80)
	for i := 0; i < 80; i++ {
		items = append(items, SourceConfigChangeItem{
			SourceName: fmt.Sprintf("采集源名称-%03d-较长一点便于占位", i),
			SourceID:   fmt.Sprintf("source-id-%03d-abcdefghijklmnopqrstuvwxyz", i),
			Changes:    []string{"启用状态: 已启用 → 已停用", "请求间隔: 100ms → 200ms"},
		})
	}
	at := time.Date(2026, 8, 8, 12, 0, 0, 0, time.FixedZone("CST", 8*3600))
	parts := formatSourceConfigsChanged("测试站", items, at)
	if len(parts) < 2 {
		t.Fatalf("expected multi-page messages, got %d parts", len(parts))
	}
	joined := strings.Join(parts, "\n")
	if !strings.Contains(parts[0], "· 1/") {
		t.Fatalf("page1 header missing: %s", truncateForTest(parts[0], 120))
	}
	if !strings.Contains(joined, "批量 80 个") {
		t.Fatalf("missing total count: %s", truncateForTest(joined, 200))
	}
	// 时间戳只在末页
	if strings.Contains(parts[0], "🕒 时间") {
		t.Fatal("timestamp should only appear on last page")
	}
	if !strings.Contains(parts[len(parts)-1], "🕒 时间") {
		t.Fatal("last page should include timestamp")
	}
	for i, p := range parts {
		if n := utf8.RuneCountInString(p); n > telegramMaxMessageLen {
			t.Fatalf("part %d exceeds telegram limit: %d", i, n)
		}
	}
	// 不应丢源：最后一页仍含高序号源
	if !strings.Contains(joined, "source-id-079") {
		t.Fatal("expected last source id retained across pages")
	}
	if strings.Contains(joined, "<script>") {
		t.Fatal("should escape html if present")
	}
}

func TestFormatSourceConfigsChangedEscape(t *testing.T) {
	parts := formatSourceConfigsChanged("站", []SourceConfigChangeItem{{
		SourceName: `A<script>`,
		SourceID:   "id1",
		Changes:    []string{`x <y> & z`},
	}}, time.Time{})
	if len(parts) != 1 {
		t.Fatalf("want 1 part, got %d", len(parts))
	}
	if strings.Contains(parts[0], "<script>") || strings.Contains(parts[0], "<y>") {
		t.Fatalf("unescaped html: %s", parts[0])
	}
	if !strings.Contains(parts[0], "批量 1 个") || strings.Contains(parts[0], "· 1/") {
		t.Fatalf("single page should not show page index: %s", parts[0])
	}
}

func TestSourceConfigBatchRateKey(t *testing.T) {
	a := []SourceConfigChangeItem{
		{SourceID: "b", SourceName: "B"},
		{SourceID: "a", SourceName: "A"},
	}
	b := []SourceConfigChangeItem{
		{SourceID: "a", SourceName: "A"},
		{SourceID: "b", SourceName: "B"},
	}
	// 同一源集合（顺序无关）应得到相同 key
	if sourceConfigBatchRateKey(a) != sourceConfigBatchRateKey(b) {
		t.Fatal("same source set should share rate key")
	}
	c := []SourceConfigChangeItem{{SourceID: "c", SourceName: "C"}}
	if sourceConfigBatchRateKey(a) == sourceConfigBatchRateKey(c) {
		t.Fatal("different source sets must not share rate key")
	}
	if !strings.HasPrefix(sourceConfigBatchRateKey(a), model.NotifyEventSourceConfigChanged+":batch:") {
		t.Fatalf("unexpected key prefix: %s", sourceConfigBatchRateKey(a))
	}
}

func TestLevelAndCategoryFiltering(t *testing.T) {
	if !isLevelAllowed(model.SeverityWarn, model.SeverityError) {
		t.Fatal("ERROR should be allowed when target min level is WARN")
	}
	if isLevelAllowed(model.SeverityError, model.SeverityInfo) {
		t.Fatal("INFO should be blocked when target min level is ERROR")
	}

	if !isCategorySubscribed([]string{"collect"}, "collect.batch_summary") {
		t.Fatal("collect.batch_summary should match collect category subscription")
	}
	if isCategorySubscribed([]string{"cron"}, "collect.batch_summary") {
		t.Fatal("collect.batch_summary should not match cron category subscription")
	}
}

func TestIsInQuietHours(t *testing.T) {
	qh := model.NotifyQuietHours{
		Enabled: true,
		Start:   "23:00",
		End:     "07:00",
	}

	// 23:30 应该在免打扰时段内
	night := time.Date(2026, 8, 11, 23, 30, 0, 0, time.Local)
	if !isInQuietHours(qh, night) {
		t.Fatal("23:30 should be in quiet hours [23:00-07:00]")
	}

	// 12:00 应该不在免打扰时段内
	noon := time.Date(2026, 8, 11, 12, 0, 0, 0, time.Local)
	if isInQuietHours(qh, noon) {
		t.Fatal("12:00 should not be in quiet hours [23:00-07:00]")
	}
}

func TestOnlyNotifyOnUpdateFiltering(t *testing.T) {
	// 与 sendBatchSummary 一致：有更新看 listN，有失败看 FailedSources / FinalizeError
	listN := 0
	payload := model.CollectBatchNotifyPayload{
		TotalFilms:     0,
		FailedSources:  0,
		SuccessSources: 2,
		FinalizeError:  "",
	}
	hasChanges := listN > 0
	hasFailures := payload.FailedSources > 0 || strings.TrimSpace(payload.FinalizeError) != ""
	if hasChanges || hasFailures {
		t.Fatal("zero listN and no failures should mute when OnlyNotifyOnUpdate")
	}

	listN = 5
	hasChanges = listN > 0
	if !hasChanges {
		t.Fatal("listN>0 must count as changes")
	}

	payload.FailedSources = 1
	listN = 0
	hasChanges = listN > 0
	hasFailures = payload.FailedSources > 0 || strings.TrimSpace(payload.FinalizeError) != ""
	if hasChanges || !hasFailures {
		t.Fatal("failures alone must still notify")
	}
}

func TestShouldMuteByQuietHours(t *testing.T) {
	cfg := model.NotifyConfig{
		QuietHours: model.NotifyQuietHours{
			Enabled:     true,
			Start:       "00:00",
			End:         "23:59",
			AllowLevels: []model.Severity{model.SeverityError, model.SeverityCritical},
		},
	}
	// 几乎全天免打扰：INFO 应静音，ERROR 可穿透
	if !shouldMuteByQuietHours(cfg, model.SeverityInfo) {
		t.Fatal("INFO should be muted in quiet hours")
	}
	if shouldMuteByQuietHours(cfg, model.SeverityError) {
		t.Fatal("ERROR should pierce quiet hours")
	}
	cfg.QuietHours.Enabled = false
	if shouldMuteByQuietHours(cfg, model.SeverityInfo) {
		t.Fatal("disabled quiet hours should not mute")
	}
}

func truncateForTest(s string, n int) string {
	if len(s) <= n {
		return s
	}
	return s[:n]
}
