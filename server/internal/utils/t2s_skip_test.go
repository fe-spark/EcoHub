package utils

import "testing"

func TestTraditionalToSimplifiedMatchesOpenCC(t *testing.T) {
	if t2s == nil {
		t.Fatal("opencc 未初始化")
	}
	samples := []string{
		"",
		"流浪地球",
		"慶餘年",
		"一目瞭然",
		"不瞭解",
		"Hello 2024",
		"後發裡臺",
	}
	for _, s := range samples {
		got := TraditionalToSimplified(s)
		want, err := t2s.Convert(s)
		if err != nil {
			t.Fatal(err)
		}
		if got != want {
			t.Fatalf("TraditionalToSimplified(%q)=%q want %q", s, got, want)
		}
	}
	if containsTraditionalRune("流浪地球") {
		t.Fatal("简体片名不应走繁转简")
	}
	if !containsTraditionalRune("慶餘年") {
		t.Fatal("繁体片名应走繁转简")
	}
}
