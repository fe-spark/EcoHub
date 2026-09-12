package service

import (
	"context"
	"encoding/json"
	"os"
	"testing"
	"time"
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
	oldID, newID, name := parseHelperArgs([]string{"upgrade-helper", "--old", "container-old-123", "--new", "container-new-456", "--name", "Eco-hub"})
	if oldID != "container-old-123" || newID != "container-new-456" || name != "Eco-hub" {
		t.Fatalf("parseHelperArgs got old=%s new=%s name=%s", oldID, newID, name)
	}

	// 测试环境变量 fallback
	t.Setenv("ECOHUB_UPGRADE_OLD", "env-old")
	t.Setenv("ECOHUB_UPGRADE_NEW", "env-new")
	t.Setenv("ECOHUB_UPGRADE_NAME", "env-name")
	oldEnv, newEnv, nameEnv := parseHelperArgs([]string{"upgrade-helper"})
	if oldEnv != "env-old" || newEnv != "env-new" || nameEnv != "env-name" {
		t.Fatalf("parseHelperArgs fallback got old=%s new=%s name=%s", oldEnv, newEnv, nameEnv)
	}
}

func TestDockerSockBind(t *testing.T) {
	// 默认回退
	if got := dockerSockBind(nil); got != "/var/run/docker.sock:/var/run/docker.sock" {
		t.Fatalf("dockerSockBind default = %s", got)
	}
	// 识别自定义宿主机路径
	hc := []byte(`{"Binds": ["/data:/data", "/run/user/1000/docker.sock:/var/run/docker.sock"]}`)
	if got := dockerSockBind(hc); got != "/run/user/1000/docker.sock:/var/run/docker.sock" {
		t.Fatalf("dockerSockBind custom = %s", got)
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
				"Proxy-network": []byte(`{
					"IPAddress": "172.19.0.2",
					"DNSNames": ["Eco-hub-proxy"]
				}`),
			},
		},
	}

	body, extraNets, err := buildReplacementBody(insp, "ghcr.io/fe-spark/ecohub:v2.6.1")
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

	// 检查多网络时：创建体中只保留 1 个网络，其余放入 extraNets 避免 Docker 创建报错
	netCfg, ok := body["NetworkingConfig"].(map[string]any)
	if !ok {
		t.Fatalf("expected NetworkingConfig")
	}
	endpoints, ok := netCfg["EndpointsConfig"].(map[string]any)
	if !ok {
		t.Fatalf("expected EndpointsConfig")
	}
	if len(endpoints) != 1 {
		t.Fatalf("expected exactly 1 endpoint in EndpointsConfig, got %d", len(endpoints))
	}
	if len(extraNets) != 1 {
		t.Fatalf("expected exactly 1 extra network, got %d", len(extraNets))
	}
}

func TestDockerEngineLiveSocket(t *testing.T) {
	if _, err := os.Stat(dockerSock); err != nil {
		t.Skip("Docker socket 不存在，跳过真实 Docker 连通性测试")
	}
	engine, err := newDockerEngine()
	if err != nil {
		t.Fatalf("newDockerEngine 失败: %v", err)
	}
	ctx, cancel := context.WithTimeout(context.Background(), 5*time.Second)
	defer cancel()

	// 真实连通性测试：检查运行中的 Eco-redis 容器
	running, err := engine.isRunning(ctx, "Eco-redis")
	if err != nil {
		t.Fatalf("调用 engine.isRunning('Eco-redis') 失败: %v", err)
	}
	if !running {
		t.Fatalf("预期 Eco-redis 处于运行状态，实际未运行")
	}

	// 测试容器真实元数据获取与版本替换解析
	insp, err := engine.inspect(ctx, "Eco-redis")
	if err != nil {
		t.Fatalf("engine.inspect('Eco-redis') 失败: %v", err)
	}
	if insp.ID == "" || insp.Name == "" {
		t.Fatalf("inspect 返回元数据不完整: id=%s name=%s", insp.ID, insp.Name)
	}

	// 验证 buildReplacementBody 在真实容器 inspect 数据上的生成行为
	newImage := "redis:7.4-alpine-test"
	body, extraNets, err := buildReplacementBody(insp, newImage)
	if err != nil {
		t.Fatalf("buildReplacementBody 失败: %v", err)
	}
	if body["Image"] != newImage {
		t.Fatalf("替换镜像不匹配: %v", body["Image"])
	}
	if _, hasHostname := body["Hostname"]; hasHostname {
		t.Fatalf("生产安全要求: Hostname 必须清除")
	}
	netCfg, ok := body["NetworkingConfig"].(map[string]any)
	if !ok {
		t.Fatalf("NetworkingConfig 缺失")
	}
	endpoints, ok := netCfg["EndpointsConfig"].(map[string]any)
	if !ok || len(endpoints) == 0 {
		t.Fatalf("主网络配置缺失: %v", netCfg)
	}
	t.Logf("真实 Docker 连通性验证成功: 宿主机 Docker 响应正常，成功检出容器 %s (ID: %s, 主网络数: %d, 附加网络数: %d)",
		insp.Name, insp.ID[:12], len(endpoints), len(extraNets))
}


