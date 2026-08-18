package service

import "testing"

func TestLatestImageRef(t *testing.T) {
	cases := []struct {
		in, want string
	}{
		{"", "ghcr.io/fe-spark/ecohub:latest"},
		{"ghcr.io/fe-spark/ecohub:v2.0.4", "ghcr.io/fe-spark/ecohub:latest"},
		{"ghcr.io/fe-spark/ecohub:latest", "ghcr.io/fe-spark/ecohub:latest"},
		{"ghcr.io/fe-spark/ecohub", "ghcr.io/fe-spark/ecohub:latest"},
		{"ghcr.io/fe-spark/ecohub@sha256:abc", "ghcr.io/fe-spark/ecohub:latest"},
	}
	for _, c := range cases {
		if got := latestImageRef(c.in); got != c.want {
			t.Fatalf("latestImageRef(%q)=%q want %q", c.in, got, c.want)
		}
	}
}

func TestParseContainerIDCandidates(t *testing.T) {
	id := "a1b2c3d4e5f60718293a4b5c6d7e8f90123456789abcdeffedcba9876543210f"
	layer := "ffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffffff"
	raw := "/var/lib/docker/overlay2/" + layer + "/merged / /containers/" + id + "/hostname"
	got := parseContainerIDCandidates(raw)
	if len(got) != 1 || got[0] != id {
		t.Fatalf("parseContainerIDCandidates=%v want [%s]", got, id)
	}
	scope := parseContainerIDCandidates("0::/system.slice/docker-" + id + ".scope")
	if len(scope) != 1 || scope[0] != id {
		t.Fatalf("scope=%v", scope)
	}
}
