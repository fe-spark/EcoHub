package handler

import (
	"strconv"
	"strings"
	"testing"
)

func TestParseDailyUpdatePid(t *testing.T) {
	if parseDailyUpdatePid("") != 0 {
		t.Fatal("empty should be 0")
	}
	if parseDailyUpdatePid("abc") != 0 {
		t.Fatal("invalid should be 0")
	}
	if parseDailyUpdatePid("-1") != -1 {
		t.Fatal("other pid should be -1")
	}
	if parseDailyUpdatePid("12") != 12 {
		t.Fatal("want 12")
	}
}

func TestParseQueryBool(t *testing.T) {
	if !parseQueryBool("1") || !parseQueryBool("true") || !parseQueryBool("TRUE") {
		t.Fatal("true values")
	}
	if parseQueryBool("") || parseQueryBool("0") || parseQueryBool("false") {
		t.Fatal("false values")
	}
}

func TestParseQueryInt(t *testing.T) {
	if parseQueryInt("", 21) != 21 {
		t.Fatal("fallback")
	}
	if parseQueryInt("x", 21) != 21 {
		t.Fatal("invalid fallback")
	}
	if parseQueryInt("3", 21) != 3 {
		t.Fatal("want 3")
	}
}

func TestParseHotKeywordsLimit(t *testing.T) {
	if parseHotKeywordsLimit("") != hotKeywordsDefaultLimit {
		t.Fatal("empty")
	}
	if parseHotKeywordsLimit("x") != hotKeywordsDefaultLimit {
		t.Fatal("invalid")
	}
	if parseHotKeywordsLimit("0") != hotKeywordsDefaultLimit {
		t.Fatal("zero")
	}
	if parseHotKeywordsLimit("8") != 8 {
		t.Fatal("want 8")
	}
	if parseHotKeywordsLimit("20") != 20 {
		t.Fatal("want 20")
	}
	if parseHotKeywordsLimit("99999") != hotKeywordsMaxLimit {
		t.Fatal("clamp max")
	}
}

func TestParseDailyUpdateExcludeCap(t *testing.T) {
	if parseDailyUpdateExclude("") != nil {
		t.Fatal("empty")
	}
	got := parseDailyUpdateExclude("1,2,2,abc,0,-3,4")
	if len(got) != 3 || got[0] != 1 || got[1] != 2 || got[2] != 4 {
		t.Fatalf("dedup/skip: %v", got)
	}

	parts := make([]string, dailyUpdateExcludeCap+20)
	for i := range parts {
		parts[i] = strconv.Itoa(i + 1)
	}
	got = parseDailyUpdateExclude(strings.Join(parts, ","))
	if len(got) != dailyUpdateExcludeCap {
		t.Fatalf("cap: want %d got %d", dailyUpdateExcludeCap, len(got))
	}
	if got[0] != 1 || got[len(got)-1] != int64(dailyUpdateExcludeCap) {
		t.Fatalf("cap range: first=%d last=%d", got[0], got[len(got)-1])
	}
}
