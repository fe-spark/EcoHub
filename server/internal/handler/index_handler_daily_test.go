package handler

import "testing"

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
