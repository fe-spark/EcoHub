package cache

import "testing"

func TestRedisGlobMatch(t *testing.T) {
	cases := []struct {
		pattern string
		key     string
		want    bool
	}{
		{"EcoHub:Film:Category:*", "EcoHub:Film:Category:vlive:1", true},
		{"EcoHub:Film:*", "EcoHub:TVBox:List:1", false},
		{"a?c", "abc", true},
		{"a?c", "abbc", false},
		{"pre/*/suf", "pre/mid/suf", true},
		{"exact", "exact", true},
		{"exact", "exact2", false},
	}
	for _, tc := range cases {
		if got := matchPattern(tc.pattern, tc.key); got != tc.want {
			t.Fatalf("match %q %q = %v, want %v", tc.pattern, tc.key, got, tc.want)
		}
	}
}
