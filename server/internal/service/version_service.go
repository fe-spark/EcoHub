package service

import (
	"encoding/json"
	"fmt"
	"io"
	"log"
	"net/http"
	"strconv"
	"strings"
	"time"

	"server/internal/config"
	"server/internal/infra/db"
)

type VersionService struct{}

var VersionSvc = new(VersionService)

type AppVersionInfo struct {
	Current      string `json:"current"`
	Latest       string `json:"latest"`
	HasUpdate    bool   `json:"hasUpdate"`
	ReleaseURL   string `json:"releaseUrl"`
	ReleaseName  string `json:"releaseName"`
	ReleaseNotes string `json:"releaseNotes"`
	Breaking     bool   `json:"breaking"`
}

type githubReleaseCache struct {
	TagName    string `json:"tagName"`
	HTMLURL    string `json:"htmlUrl"`
	Name       string `json:"name"`
	Body       string `json:"body"`
	Prerelease bool   `json:"prerelease"`
}

func (s *VersionService) GetAppVersion() AppVersionInfo {
	info := AppVersionInfo{Current: strings.TrimSpace(config.Version)}
	latest, err := s.loadLatestRelease()
	if err != nil || latest.TagName == "" {
		if err != nil {
			log.Printf("[Version] 获取 GitHub Release 失败: %v", err)
		}
		return info
	}
	if isPreRelease(latest.TagName, latest.Name, latest.Prerelease) {
		return info
	}
	info.Latest = latest.TagName
	info.ReleaseURL = latest.HTMLURL
	info.ReleaseName = latest.Name
	info.ReleaseNotes = latest.Body
	info.Breaking = strings.Contains(latest.Body, "破坏性改动")
	info.HasUpdate = isNewerVersion(latest.TagName, info.Current)
	return info
}

func (s *VersionService) loadLatestRelease() (githubReleaseCache, error) {
	if raw, err := db.Rdb.Get(db.Cxt, config.LatestReleaseCacheKey).Result(); err == nil && raw != "" {
		var cached githubReleaseCache
		if json.Unmarshal([]byte(raw), &cached) == nil && cached.TagName != "" {
			return cached, nil
		}
	}
	rel, err := fetchGithubLatestRelease()
	if err != nil {
		return githubReleaseCache{}, err
	}
	if b, err := json.Marshal(rel); err == nil {
		_ = db.Rdb.Set(db.Cxt, config.LatestReleaseCacheKey, b, config.LatestReleaseCacheTTL).Err()
	}
	return rel, nil
}

func fetchGithubLatestRelease() (githubReleaseCache, error) {
	url := fmt.Sprintf("https://api.github.com/repos/%s/releases?per_page=20", githubRepoPath())
	req, err := http.NewRequest(http.MethodGet, url, nil)
	if err != nil {
		return githubReleaseCache{}, err
	}
	req.Header.Set("Accept", "application/vnd.github+json")
	req.Header.Set("User-Agent", "EcoHub/"+config.Version)

	client := &http.Client{Timeout: 5 * time.Second}
	resp, err := client.Do(req)
	if err != nil {
		return githubReleaseCache{}, err
	}
	defer resp.Body.Close()
	body, err := io.ReadAll(io.LimitReader(resp.Body, 1<<20))
	if err != nil {
		return githubReleaseCache{}, err
	}
	if resp.StatusCode != http.StatusOK {
		return githubReleaseCache{}, fmt.Errorf("github %s", resp.Status)
	}
	var raw []struct {
		TagName    string `json:"tag_name"`
		HTMLURL    string `json:"html_url"`
		Name       string `json:"name"`
		Body       string `json:"body"`
		Draft      bool   `json:"draft"`
		Prerelease bool   `json:"prerelease"`
	}
	if err := json.Unmarshal(body, &raw); err != nil {
		return githubReleaseCache{}, err
	}
	for _, item := range raw {
		if item.Draft {
			continue
		}
		tag := strings.TrimSpace(item.TagName)
		name := strings.TrimSpace(item.Name)
		if isPreRelease(tag, name, item.Prerelease) {
			continue
		}
		return githubReleaseCache{
			TagName:    tag,
			HTMLURL:    strings.TrimSpace(item.HTMLURL),
			Name:       name,
			Body:       strings.TrimSpace(item.Body),
			Prerelease: false,
		}, nil
	}
	return githubReleaseCache{}, fmt.Errorf("无正式版 Release")
}

func isPreRelease(tag, name string, flagged bool) bool {
	if flagged {
		return true
	}
	blob := strings.ToLower(tag + " " + name)
	for _, token := range []string{"beta", "alpha", "rc", "preview", "snapshot"} {
		if containsPreToken(blob, token) {
			return true
		}
	}
	return false
}

func containsPreToken(s, token string) bool {
	idx := 0
	for {
		i := strings.Index(s[idx:], token)
		if i < 0 {
			return false
		}
		i += idx
		beforeOK := i == 0 || !isASCIILetter(s[i-1])
		after := i + len(token)
		afterOK := after >= len(s) || !isASCIILetter(s[after])
		if beforeOK && afterOK {
			return true
		}
		idx = i + len(token)
	}
}

func isASCIILetter(b byte) bool {
	return b >= 'a' && b <= 'z'
}

func githubRepoPath() string {
	u := strings.TrimSuffix(strings.TrimSpace(config.ProjectURL), ".git")
	u = strings.TrimSuffix(u, "/")
	const prefix = "https://github.com/"
	if strings.HasPrefix(u, prefix) {
		return strings.TrimPrefix(u, prefix)
	}
	return "fe-spark/EcoHub"
}

func isNewerVersion(latest, current string) bool {
	lm, ln, lp, lok := parseSemver(latest)
	cm, cn, cp, cok := parseSemver(current)
	if !lok || !cok {
		return normalizeVer(latest) != normalizeVer(current) && latest != ""
	}
	if lm != cm {
		return lm > cm
	}
	if ln != cn {
		return ln > cn
	}
	if lp != cp {
		return lp > cp
	}
	return !isPreRelease(latest, "", false) && isPreRelease(current, "", false)
}

func parseSemver(raw string) (major, minor, patch int, ok bool) {
	s := normalizeVer(raw)
	if i := strings.IndexAny(s, "-+"); i >= 0 {
		s = s[:i]
	}
	parts := strings.Split(s, ".")
	if len(parts) < 2 || len(parts) > 3 {
		return 0, 0, 0, false
	}
	nums := make([]int, 3)
	for i, p := range parts {
		n, err := strconv.Atoi(p)
		if err != nil {
			return 0, 0, 0, false
		}
		nums[i] = n
	}
	return nums[0], nums[1], nums[2], true
}

func normalizeVer(raw string) string {
	s := strings.TrimSpace(raw)
	s = strings.TrimPrefix(s, "v")
	s = strings.TrimPrefix(s, "V")
	return s
}
