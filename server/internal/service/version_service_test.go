package service

import "testing"

func TestIsNewerVersion(t *testing.T) {
	cases := []struct {
		latest, current string
		want            bool
	}{
		{"v2.1.0", "2.0.4", true},
		{"2.1.0", "v2.1.0", false},
		{"2.0.4", "2.1.0", false},
		{"v2.1.1", "2.1.0", true},
		{"v3.0.0", "2.9.9", true},
		{"v2.1.0", "2.1.0-beta.1", true},
		{"v2.1.0-beta.1", "2.1.0", false},
		{"v2.1.0-beta.2", "2.1.0-beta.1", true},
		{"v2.1.0-beta.1", "2.1.0-beta.2", false},
		{"v2.1.0-beta.2", "2.1.0-beta.2", false},
		{"v2.1.1-beta.1", "2.1.0-beta.9", true},
	}
	for _, c := range cases {
		if got := isNewerVersion(c.latest, c.current); got != c.want {
			t.Fatalf("isNewerVersion(%q, %q)=%v want %v", c.latest, c.current, got, c.want)
		}
	}
}

func TestIsPreRelease(t *testing.T) {
	cases := []struct {
		tag, name string
		flagged   bool
		want      bool
	}{
		{"v2.1.0", "v2.1.0", false, false},
		{"v2.1.0-beta.1", "v2.1.0-beta.1", false, true},
		{"v2.0.2-beta.2", "release", false, true},
		{"v2.1.0", "2.1.0 Beta", false, true},
		{"v2.1.0-rc.1", "", false, true},
		{"v2.1.0", "", true, true},
		{"v2.1.0", "March source", false, false},
	}
	for _, c := range cases {
		if got := isPreRelease(c.tag, c.name, c.flagged); got != c.want {
			t.Fatalf("isPreRelease(%q, %q, %v)=%v want %v", c.tag, c.name, c.flagged, got, c.want)
		}
	}
}
