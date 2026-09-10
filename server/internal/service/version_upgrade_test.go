package service

import (
	"encoding/json"
	"testing"
)

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

func TestParseHelperArgs(t *testing.T) {
	oldID, newID := parseHelperArgs([]string{"upgrade-helper", "--old", "container-old-123", "--new", "container-new-456"})
	if oldID != "container-old-123" || newID != "container-new-456" {
		t.Fatalf("parseHelperArgs got old=%s new=%s", oldID, newID)
	}

	// 测试环境变量 fallback
	t.Setenv("ECOHUB_UPGRADE_OLD", "env-old")
	t.Setenv("ECOHUB_UPGRADE_NEW", "env-new")
	oldEnv, newEnv := parseHelperArgs([]string{"upgrade-helper"})
	if oldEnv != "env-old" || newEnv != "env-new" {
		t.Fatalf("parseHelperArgs fallback got old=%s new=%s", oldEnv, newEnv)
	}
}

func TestBuildReplacementBody(t *testing.T) {
	insp := containerInspect{
		ID:   "old123",
		Name: "/Eco-hub",
		Config: []byte(`{
			"Image": "ghcr.io/fe-spark/ecohub:v2.6.0",
			"Hostname": "abcdef123456",
			"Env": ["PORT=8080"]
		}`),
		HostConfig: []byte(`{
			"Binds": ["/data:/data"],
			"Mounts": [{"Type": "volume"}]
		}`),
		NetworkSettings: struct {
			Networks map[string]json.RawMessage `json:"Networks"`
		}{
			Networks: map[string]json.RawMessage{
				"Eco-network": []byte(`{
					"IPAddress": "172.18.0.5",
					"DNSNames": ["Eco-hub", "old123"],
					"Aliases": ["Eco-hub"]
				}`),
			},
		},
	}

	body, err := buildReplacementBody(insp, "ghcr.io/fe-spark/ecohub:v2.6.1")
	if err != nil {
		t.Fatalf("buildReplacementBody err: %v", err)
	}

	if body["Image"] != "ghcr.io/fe-spark/ecohub:v2.6.1" {
		t.Fatalf("expected updated image, got %v", body["Image"])
	}
	if _, exists := body["Hostname"]; exists {
		t.Fatalf("expected Hostname to be removed")
	}

	// 检查 Mounts 存在时 Binds 被清理
	hc, ok := body["HostConfig"].(map[string]any)
	if !ok {
		t.Fatalf("expected HostConfig map")
	}
	if _, exists := hc["Binds"]; exists {
		t.Fatalf("expected Binds removed when Mounts present")
	}

	// 检查 inspect 只读字段被清理
	netCfg, ok := body["NetworkingConfig"].(map[string]any)
	if !ok {
		t.Fatalf("expected NetworkingConfig")
	}
	endpoints, ok := netCfg["EndpointsConfig"].(map[string]any)
	if !ok {
		t.Fatalf("expected EndpointsConfig")
	}
	ecoNet, ok := endpoints["Eco-network"].(map[string]any)
	if !ok {
		t.Fatalf("expected Eco-network config")
	}
	if _, exists := ecoNet["IPAddress"]; exists {
		t.Fatalf("expected IPAddress removed")
	}
	if _, exists := ecoNet["DNSNames"]; exists {
		t.Fatalf("expected DNSNames removed")
	}
}

