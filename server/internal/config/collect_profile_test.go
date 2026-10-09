package config

import "testing"

func TestResolveCollectProfileUsesScarcerResource(t *testing.T) {
	const gib = uint64(1024 * 1024 * 1024)
	cases := []struct {
		cpu  int
		mem  uint64
		want string
	}{
		{cpu: 10, mem: 16 * gib, want: "high"},
		{cpu: 10, mem: 4 * gib, want: "standard"},
		{cpu: 10, mem: 2 * gib, want: "light"},
		{cpu: 2, mem: 32 * gib, want: "light"},
		{cpu: 4, mem: 16 * gib, want: "standard"},
		{cpu: 4, mem: 0, want: "standard"},
	}
	for _, tc := range cases {
		_, name := resolveCollectProfileFor(tc.cpu, tc.mem, "")
		if name != tc.want {
			t.Errorf("cpu=%d mem=%d got %s want %s", tc.cpu, tc.mem, name, tc.want)
		}
	}
}

func TestResolveCollectProfileExplicitOverride(t *testing.T) {
	_, name := resolveCollectProfileFor(2, 2<<30, "high")
	if name != "high" {
		t.Fatalf("explicit profile=%s", name)
	}
	_, name = resolveCollectProfileFor(16, 32<<30, "nope")
	if name != "high" {
		t.Fatalf("invalid profile fell back to %s", name)
	}
}

func TestParseMeminfoTotal(t *testing.T) {
	got := parseMeminfoTotal("MemTotal:       8040064 kB\nMemFree:  123 kB\n")
	if got != 8040064*1024 {
		t.Fatalf("bytes=%d", got)
	}
}
