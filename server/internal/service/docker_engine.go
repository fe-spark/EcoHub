package service

import (
	"context"
	"encoding/json"
	"fmt"
	"io"
	"net"
	"net/http"
	"net/url"
	"os"
	"strings"
	"time"
)

const dockerSock = "/var/run/docker.sock"
const dockerAPI = "http://docker/v1.43"

type dockerEngine struct {
	http *http.Client
}

func newDockerEngine() (*dockerEngine, error) {
	if _, err := os.Stat(dockerSock); err != nil {
		return nil, fmt.Errorf("未挂载 Docker socket，无法在线升级")
	}
	transport := &http.Transport{
		DialContext: func(ctx context.Context, _, _ string) (net.Conn, error) {
			var d net.Dialer
			return d.DialContext(ctx, "unix", dockerSock)
		},
	}
	return &dockerEngine{
		http: &http.Client{Transport: transport, Timeout: 10 * time.Minute},
	}, nil
}

type containerInspect struct {
	ID              string          `json:"Id"`
	Name            string          `json:"Name"`
	Image           string          `json:"Image"`
	Config          json.RawMessage `json:"Config"`
	HostConfig      json.RawMessage `json:"HostConfig"`
	NetworkSettings struct {
		Networks map[string]json.RawMessage `json:"Networks"`
	} `json:"NetworkSettings"`
}

func (d *dockerEngine) inspect(ctx context.Context, id string) (containerInspect, error) {
	var out containerInspect
	if err := d.get(ctx, "/containers/"+url.PathEscape(id)+"/json", &out); err != nil {
		return out, err
	}
	return out, nil
}

func (d *dockerEngine) pull(ctx context.Context, image string) error {
	repo, tag := splitImageTag(image)
	q := url.Values{}
	q.Set("fromImage", repo)
	q.Set("tag", tag)
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dockerAPI+"/images/create?"+q.Encode(), nil)
	if err != nil {
		return err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode != http.StatusOK {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("拉取镜像失败: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	dec := json.NewDecoder(resp.Body)
	for {
		var line map[string]any
		if err := dec.Decode(&line); err != nil {
			if err == io.EOF {
				return nil
			}
			return err
		}
		if msg, _ := line["error"].(string); msg != "" {
			return fmt.Errorf("拉取镜像失败: %s", msg)
		}
	}
}

func (d *dockerEngine) rename(ctx context.Context, id, name string) error {
	q := url.Values{}
	q.Set("name", name)
	return d.post(ctx, "/containers/"+url.PathEscape(id)+"/rename?"+q.Encode(), nil, nil)
}

func (d *dockerEngine) create(ctx context.Context, name string, body any) (string, error) {
	q := url.Values{}
	q.Set("name", name)
	var out struct {
		ID string `json:"Id"`
	}
	if err := d.post(ctx, "/containers/create?"+q.Encode(), body, &out); err != nil {
		return "", err
	}
	if out.ID == "" {
		return "", fmt.Errorf("创建容器未返回 ID")
	}
	return out.ID, nil
}

func (d *dockerEngine) start(ctx context.Context, id string) error {
	return d.post(ctx, "/containers/"+url.PathEscape(id)+"/start", nil, nil)
}

func (d *dockerEngine) stop(ctx context.Context, id string) error {
	return d.post(ctx, "/containers/"+url.PathEscape(id)+"/stop?t=15", nil, nil)
}

func (d *dockerEngine) isRunning(ctx context.Context, id string) (bool, error) {
	var out struct {
		State struct {
			Running bool `json:"Running"`
		} `json:"State"`
	}
	if err := d.get(ctx, "/containers/"+url.PathEscape(id)+"/json", &out); err != nil {
		return false, err
	}
	return out.State.Running, nil
}

func (d *dockerEngine) remove(ctx context.Context, id string) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodDelete, dockerAPI+"/containers/"+url.PathEscape(id)+"?force=true", nil)
	if err != nil {
		return err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	if resp.StatusCode >= 300 {
		body, _ := io.ReadAll(io.LimitReader(resp.Body, 4096))
		return fmt.Errorf("删除容器失败: %s %s", resp.Status, strings.TrimSpace(string(body)))
	}
	return nil
}

func (d *dockerEngine) get(ctx context.Context, path string, out any) error {
	req, err := http.NewRequestWithContext(ctx, http.MethodGet, dockerAPI+path, nil)
	if err != nil {
		return err
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("docker %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if out == nil {
		return nil
	}
	return json.Unmarshal(body, out)
}

func (d *dockerEngine) post(ctx context.Context, path string, payload, out any) error {
	var reader io.Reader
	if payload != nil {
		b, err := json.Marshal(payload)
		if err != nil {
			return err
		}
		reader = strings.NewReader(string(b))
	}
	req, err := http.NewRequestWithContext(ctx, http.MethodPost, dockerAPI+path, reader)
	if err != nil {
		return err
	}
	if payload != nil {
		req.Header.Set("Content-Type", "application/json")
	}
	resp, err := d.http.Do(req)
	if err != nil {
		return err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 8<<20))
	if err != nil {
		return err
	}
	if resp.StatusCode == http.StatusNotModified {
		return nil
	}
	if resp.StatusCode >= 300 {
		return fmt.Errorf("docker %s: %s", resp.Status, strings.TrimSpace(string(body)))
	}
	if out == nil || len(body) == 0 {
		return nil
	}
	return json.Unmarshal(body, out)
}

func splitImageTag(image string) (repo, tag string) {
	image = strings.TrimSpace(image)
	if i := strings.Index(image, "@"); i >= 0 {
		image = image[:i]
	}
	tag = "latest"
	if i := strings.LastIndex(image, ":"); i >= 0 && !strings.Contains(image[i:], "/") {
		return image[:i], image[i+1:]
	}
	return image, tag
}

func latestImageRef(current string) string {
	current = strings.TrimSpace(current)
	if current == "" {
		return "ghcr.io/fe-spark/ecohub:latest"
	}
	if i := strings.Index(current, "@"); i >= 0 {
		current = current[:i]
	}
	if i := strings.LastIndex(current, ":"); i >= 0 && !strings.Contains(current[i:], "/") {
		return current[:i] + ":latest"
	}
	return current + ":latest"
}
