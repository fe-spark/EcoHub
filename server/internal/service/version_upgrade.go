package service

import (
	"context"
	"encoding/json"
	"fmt"
	"log"
	"os"
	"regexp"
	"sort"
	"strings"
	"sync/atomic"
	"time"
)

const defaultAllInOneImage = "ghcr.io/fe-spark/ecohub:latest"

var (
	reContainerPath = regexp.MustCompile(`/containers/([0-9a-f]{64})(?:/|\b)`)
	reDockerScope   = regexp.MustCompile(`docker-([0-9a-f]{64})\.scope`)
	reDockerCgroup  = regexp.MustCompile(`/docker/([0-9a-f]{64})(?:/|\b)`)
	upgrading       atomic.Bool
	upgradePhase    atomic.Value // string
	upgradeErr      atomic.Value // string
)

func init() {
	upgradePhase.Store("idle")
	upgradeErr.Store("")
}

type upgradeState struct {
	Phase string `json:"phase"`
	Error string `json:"error"`
}

func currentUpgradeState() upgradeState {
	phase, _ := upgradePhase.Load().(string)
	errMsg, _ := upgradeErr.Load().(string)
	return upgradeState{Phase: phase, Error: errMsg}
}

func setUpgradeState(phase, errMsg string) {
	upgradePhase.Store(phase)
	upgradeErr.Store(errMsg)
}

func dockerSockAvailable() bool {
	_, err := os.Stat(dockerSock)
	return err == nil
}

func (s *VersionService) CanOnlineUpgrade() bool {
	if !dockerSockAvailable() {
		return false
	}
	if _, err := os.Stat("/.dockerenv"); err != nil {
		return false
	}
	return true
}

func (s *VersionService) StartUpgrade() error {
	if !s.CanOnlineUpgrade() {
		return fmt.Errorf("未挂载 Docker socket，或当前不是发布版容器")
	}
	if !upgrading.CompareAndSwap(false, true) {
		return fmt.Errorf("升级正在进行中")
	}
	engine, err := newDockerEngine()
	if err != nil {
		upgrading.Store(false)
		return err
	}
	insp, err := resolveSelfContainer(engine)
	if err != nil {
		upgrading.Store(false)
		return fmt.Errorf("无法识别当前容器，仅发布版 All-in-One 支持在线升级")
	}
	info := s.GetAppVersion(true)
	if !info.HasUpdate || info.Latest == "" {
		upgrading.Store(false)
		return fmt.Errorf("没有可升级版本")
	}
	image := taggedImage(configImageFromInspect(insp), info.Latest)
	setUpgradeState("pulling", "")
	go runContainerUpgrade(engine, insp, image)
	return nil
}

func taggedImage(current, tag string) string {
	repo, _ := splitImageTag(latestImageRef(current))
	tag = strings.TrimSpace(tag)
	if tag == "" {
		return repo + ":latest"
	}
	return repo + ":" + tag
}

func configImageFromInspect(insp containerInspect) string {
	var cfg struct {
		Image string `json:"Image"`
	}
	if json.Unmarshal(insp.Config, &cfg) == nil && strings.TrimSpace(cfg.Image) != "" {
		return cfg.Image
	}
	return defaultAllInOneImage
}

func runContainerUpgrade(engine *dockerEngine, old containerInspect, image string) {
	defer upgrading.Store(false)
	ctx, cancel := context.WithTimeout(context.Background(), 12*time.Minute)
	defer cancel()

	log.Printf("[Upgrade] 拉取 %s", image)
	setUpgradeState("pulling", "")
	if err := engine.pull(ctx, image); err != nil {
		log.Printf("[Upgrade] 拉取失败: %v", err)
		setUpgradeState("failed", err.Error())
		return
	}

	setUpgradeState("recreating", "")
	name := strings.TrimPrefix(old.Name, "/")
	if name == "" {
		name = "Eco-hub"
	}
	oldBackup := fmt.Sprintf("%s-old-%d", name, time.Now().Unix())
	if err := handoffContainer(ctx, engine, old, name, oldBackup, image); err != nil {
		log.Printf("[Upgrade] 重建失败: %v", err)
		setUpgradeState("failed", err.Error())
		return
	}
	setUpgradeState("done", "")
}

func handoffContainer(ctx context.Context, engine *dockerEngine, old containerInspect, name, backup, image string) error {
	body, extraNets, err := buildReplacementBody(old, image)
	if err != nil {
		return err
	}
	if err := engine.rename(ctx, old.ID, backup); err != nil {
		return fmt.Errorf("重命名旧容器失败: %w", err)
	}
	newID, err := engine.create(ctx, name, body)
	if err != nil {
		_ = engine.rename(ctx, old.ID, name)
		return fmt.Errorf("创建新容器失败: %w", err)
	}
	for netName, ep := range extraNets {
		if netErr := engine.connectNetwork(ctx, netName, newID, ep); netErr != nil {
			log.Printf("[Upgrade] 连接附加网络 %s 失败: %v", netName, netErr)
		}
	}
	sockBind := dockerSockBind(old.HostConfig)
	helperName := name + "-upg-helper"
	if err := startUpgradeHelper(ctx, engine, image, helperName, old.ID, newID, name, sockBind); err != nil {
		_ = engine.remove(ctx, newID)
		_ = engine.rename(ctx, old.ID, name)
		return fmt.Errorf("启动升级助手失败: %w", err)
	}
	// 助手在本容器退出后再 start 新容器，避免端口冲突
	if err := engine.stop(ctx, old.ID); err != nil {
		log.Printf("[Upgrade] 停止本容器请求返回: %v（助手将兜底确保旧容器退出）", err)
	}
	return nil
}

func dockerSockBind(rawHostConfig json.RawMessage) string {
	var hc struct {
		Binds []string `json:"Binds"`
	}
	if json.Unmarshal(rawHostConfig, &hc) == nil {
		for _, b := range hc.Binds {
			parts := strings.Split(b, ":")
			if len(parts) >= 2 && (parts[1] == dockerSock || parts[1] == "/run/docker.sock") {
				return b
			}
		}
	}
	return dockerSock + ":" + dockerSock
}

func buildReplacementBody(old containerInspect, image string) (map[string]any, map[string]any, error) {
	var cfg map[string]any
	if err := json.Unmarshal(old.Config, &cfg); err != nil {
		return nil, nil, err
	}
	cfg["Image"] = image
	delete(cfg, "Hostname")

	var hostConfig any = old.HostConfig
	var hc map[string]any
	if json.Unmarshal(old.HostConfig, &hc) == nil {
		if mounts, ok := hc["Mounts"].([]any); ok && len(mounts) > 0 {
			delete(hc, "Binds")
		}
		hostConfig = hc
	}

	var primaryNet string
	if netMode, ok := hc["NetworkMode"].(string); ok && netMode != "" {
		if _, exists := old.NetworkSettings.Networks[netMode]; exists {
			primaryNet = netMode
		}
	}
	netNames := make([]string, 0, len(old.NetworkSettings.Networks))
	for netName := range old.NetworkSettings.Networks {
		netNames = append(netNames, netName)
	}
	sort.Strings(netNames)
	if primaryNet != "" {
		for i, name := range netNames {
			if name == primaryNet {
				netNames = append([]string{primaryNet}, append(netNames[:i], netNames[i+1:]...)...)
				break
			}
		}
	}

	endpoints := map[string]any{}
	extraEndpoints := map[string]any{}
	for _, netName := range netNames {
		raw := old.NetworkSettings.Networks[netName]
		var ep map[string]any
		if json.Unmarshal(raw, &ep) != nil {
			continue
		}
		delete(ep, "NetworkID")
		delete(ep, "EndpointID")
		delete(ep, "IPAddress")
		delete(ep, "IPPrefixLen")
		delete(ep, "Gateway")
		delete(ep, "MacAddress")
		delete(ep, "IPv6Gateway")
		delete(ep, "GlobalIPv6Address")
		delete(ep, "GlobalIPv6PrefixLen")
		delete(ep, "DNSNames")
		if len(endpoints) == 0 {
			endpoints[netName] = ep
		} else {
			extraEndpoints[netName] = ep
		}
	}

	cfg["HostConfig"] = hostConfig
	cfg["NetworkingConfig"] = map[string]any{
		"EndpointsConfig": endpoints,
	}
	return cfg, extraEndpoints, nil
}

func startUpgradeHelper(ctx context.Context, engine *dockerEngine, image, helperName, oldID, newID, originalName, sockBind string) error {
	_ = engine.remove(ctx, helperName)
	if sockBind == "" {
		sockBind = dockerSock + ":" + dockerSock
	}
	body := map[string]any{
		"Image":      image,
		"Entrypoint": []string{"/app/server/main"},
		"Cmd":        []string{"upgrade-helper", "--old", oldID, "--new", newID, "--name", originalName},
		"Env": []string{
			"ECOHUB_UPGRADE_HELPER=1",
			"ECOHUB_UPGRADE_OLD=" + oldID,
			"ECOHUB_UPGRADE_NEW=" + newID,
			"ECOHUB_UPGRADE_NAME=" + originalName,
		},
		"HostConfig": map[string]any{
			"Binds":         []string{sockBind},
			"AutoRemove":    true,
			"NetworkMode":   "none",
			"RestartPolicy": map[string]any{"Name": "no"},
		},
	}
	id, err := engine.create(ctx, helperName, body)
	if err != nil {
		return err
	}
	if err := engine.start(ctx, id); err != nil {
		_ = engine.remove(ctx, id)
		return err
	}
	return nil
}

func resolveSelfContainer(engine *dockerEngine) (containerInspect, error) {
	var candidates []string
	if data, err := os.ReadFile("/proc/self/cgroup"); err == nil {
		candidates = append(candidates, parseContainerIDCandidates(string(data))...)
	}
	if data, err := os.ReadFile("/proc/self/mountinfo"); err == nil {
		candidates = append(candidates, parseContainerIDCandidates(string(data))...)
	}
	if host, err := os.Hostname(); err == nil && host != "" {
		candidates = append(candidates, host)
	}
	candidates = append(candidates, "Eco-hub")
	ctx, cancel := context.WithTimeout(context.Background(), 8*time.Second)
	defer cancel()
	seen := map[string]bool{}
	for _, id := range candidates {
		if seen[id] {
			continue
		}
		seen[id] = true
		insp, err := engine.inspect(ctx, id)
		if err == nil && insp.ID != "" {
			return insp, nil
		}
	}
	return containerInspect{}, fmt.Errorf("无法识别当前容器")
}

func parseContainerIDCandidates(raw string) []string {
	seen := map[string]bool{}
	var out []string
	add := func(id string) {
		if id == "" || seen[id] {
			return
		}
		seen[id] = true
		out = append(out, id)
	}
	for _, m := range reContainerPath.FindAllStringSubmatch(raw, -1) {
		add(m[1])
	}
	for _, m := range reDockerScope.FindAllStringSubmatch(raw, -1) {
		add(m[1])
	}
	for _, m := range reDockerCgroup.FindAllStringSubmatch(raw, -1) {
		add(m[1])
	}
	return out
}
