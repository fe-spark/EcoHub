package utils

import (
	"fmt"
	"strings"
)

// DomainReplaceRule 定义单个采集站播放链接域名替换规则
type DomainReplaceRule struct {
	FromScheme string // 匹配特定协议: "http", "https" 或 "" (任意协议)
	FromHost   string // 匹配 Host (小写, 可能含通配符如 "*.old.com" 或端口如 "old.com:8080")
	FromPath   string // 匹配路径前缀, 默认为 "" (如 "/vod")
	ToScheme   string // 替换协议: "http", "https" 或 "" (保持原协议)
	ToHost     string // 目标 Host (如 "new.com", "*.new.com" 或 "new.com:8443")
	ToPath     string // 目标路径前缀 (如 "/live")
}

// ParseDomainReplaceRules 解析多行文本配置为域名替换规则列表；无法解析的非注释行会被丢弃。
func ParseDomainReplaceRules(raw string) []DomainReplaceRule {
	rules, _ := parseDomainReplaceRuleLines(raw)
	return rules
}

// ValidateDomainReplaceRules 在保存配置时拒绝无法解析的非注释行。
func ValidateDomainReplaceRules(raw string) error {
	_, invalid := parseDomainReplaceRuleLines(raw)
	if len(invalid) == 0 {
		return nil
	}
	return fmt.Errorf("域名替换规则无法解析: %s", strings.Join(invalid, "；"))
}

func parseDomainReplaceRuleLines(raw string) (rules []DomainReplaceRule, invalid []string) {
	if strings.TrimSpace(raw) == "" {
		return nil, nil
	}
	lines := strings.Split(raw, "\n")
	rules = make([]DomainReplaceRule, 0, len(lines))
	for _, line := range lines {
		line = strings.TrimSpace(line)
		if line == "" || strings.HasPrefix(line, "#") || strings.HasPrefix(line, "//") || strings.HasPrefix(line, ";") {
			continue
		}
		rule, ok := parseSingleRule(line)
		if !ok {
			invalid = append(invalid, line)
			continue
		}
		rules = append(rules, rule)
	}
	return rules, invalid
}

func parseSingleRule(line string) (DomainReplaceRule, bool) {
	var fromRaw, toRaw string
	if strings.Contains(line, "=>") {
		parts := strings.SplitN(line, "=>", 2)
		fromRaw, toRaw = parts[0], parts[1]
	} else if strings.Contains(line, "->") {
		parts := strings.SplitN(line, "->", 2)
		fromRaw, toRaw = parts[0], parts[1]
	} else if strings.Contains(line, ",") {
		parts := strings.SplitN(line, ",", 2)
		fromRaw, toRaw = parts[0], parts[1]
	} else {
		fields := strings.Fields(line)
		if len(fields) == 2 {
			fromRaw, toRaw = fields[0], fields[1]
		}
	}

	fromRaw = strings.TrimSpace(fromRaw)
	toRaw = strings.TrimSpace(toRaw)
	if fromRaw == "" || toRaw == "" {
		return DomainReplaceRule{}, false
	}

	fromScheme, fromHost, fromPath := parseRuleEndpoint(fromRaw)
	toScheme, toHost, toPath := parseRuleEndpoint(toRaw)
	if fromHost == "" || toHost == "" {
		return DomainReplaceRule{}, false
	}

	return DomainReplaceRule{
		FromScheme: fromScheme,
		FromHost:   fromHost,
		FromPath:   fromPath,
		ToScheme:   toScheme,
		ToHost:     toHost,
		ToPath:     toPath,
	}, true
}

func parseRuleEndpoint(raw string) (scheme, host, path string) {
	raw = strings.TrimSpace(raw)
	lower := strings.ToLower(raw)
	if strings.HasPrefix(lower, "http://") {
		scheme = "http"
		raw = raw[7:]
	} else if strings.HasPrefix(lower, "https://") {
		scheme = "https"
		raw = raw[8:]
	} else if strings.HasPrefix(raw, "//") {
		raw = raw[2:]
	}

	idx := strings.Index(raw, "/")
	if idx != -1 {
		host = strings.TrimSpace(raw[:idx])
		path = strings.TrimRight(raw[idx:], "/")
	} else {
		host = strings.TrimSpace(raw)
		path = ""
	}
	host = strings.ToLower(host)
	return scheme, host, path
}

// ApplyDomainReplaceRules 应用域名替换规则于单个播放链接
func ApplyDomainReplaceRules(rawURL string, rules []DomainReplaceRule) string {
	rawURL = strings.TrimSpace(rawURL)
	if rawURL == "" || len(rules) == 0 {
		return rawURL
	}

	scheme, protoRelative, host, rest, ok := parseURLComponents(rawURL)
	if !ok {
		return applyOpaqueHostReplace(rawURL, rules)
	}

	userinfo := ""
	hostOnly := host
	if atIdx := strings.LastIndex(host, "@"); atIdx != -1 {
		userinfo = host[:atIdx+1]
		hostOnly = host[atIdx+1:]
	}

	hostname := hostOnly
	if colIdx := strings.LastIndex(hostOnly, ":"); colIdx != -1 {
		hostname = hostOnly[:colIdx]
	}

	for _, rule := range rules {
		if rule.FromScheme != "" && !strings.EqualFold(rule.FromScheme, scheme) {
			continue
		}

		matchedHost := false
		var targetHost string

		if strings.HasPrefix(rule.FromHost, "*.") {
			baseDomain := rule.FromHost[2:]
			if strings.HasSuffix(strings.ToLower(hostname), "."+baseDomain) {
				matchedHost = true
				subdomain := hostname[:len(hostname)-len(baseDomain)-1]
				if strings.HasPrefix(rule.ToHost, "*.") {
					targetHost = subdomain + "." + rule.ToHost[2:]
				} else {
					targetHost = rule.ToHost
				}
			}
		} else if strings.Contains(rule.FromHost, ":") {
			if strings.EqualFold(rule.FromHost, hostOnly) {
				matchedHost = true
				targetHost = rule.ToHost
			}
		} else {
			if strings.EqualFold(rule.FromHost, hostname) {
				matchedHost = true
				targetHost = rule.ToHost
			}
		}

		if !matchedHost {
			continue
		}

		if rule.FromPath != "" {
			if !hasExactPathPrefix(rest, rule.FromPath) {
				continue
			}
		}

		targetScheme := scheme
		if rule.ToScheme != "" {
			targetScheme = rule.ToScheme
		}

		targetRest := rest
		if rule.FromPath != "" {
			suffix := rest[len(rule.FromPath):]
			if strings.HasSuffix(rule.ToPath, "/") && strings.HasPrefix(suffix, "/") {
				targetRest = rule.ToPath + suffix[1:]
			} else if !strings.HasSuffix(rule.ToPath, "/") && !strings.HasPrefix(suffix, "/") && suffix != "" && !strings.HasPrefix(suffix, "?") && !strings.HasPrefix(suffix, "#") {
				targetRest = rule.ToPath + "/" + suffix
			} else {
				targetRest = rule.ToPath + suffix
			}
		}

		finalTargetHost := targetHost
		if !strings.Contains(targetHost, ":") && strings.Contains(hostOnly, ":") && !strings.Contains(rule.FromHost, ":") {
			finalTargetHost = targetHost + hostOnly[strings.LastIndex(hostOnly, ":"):]
		}

		finalHost := userinfo + finalTargetHost
		if targetScheme != "" {
			return targetScheme + "://" + finalHost + targetRest
		}
		if protoRelative {
			return "//" + finalHost + targetRest
		}
		return finalHost + targetRest
	}

	return rawURL
}

func hasExactPathPrefix(path, prefix string) bool {
	if !strings.HasPrefix(path, prefix) {
		return false
	}
	if len(path) == len(prefix) {
		return true
	}
	nextChar := path[len(prefix)]
	return nextChar == '/' || nextChar == '?' || nextChar == '#'
}

func parseURLComponents(raw string) (scheme string, protoRelative bool, host string, rest string, ok bool) {
	afterScheme := raw
	if strings.HasPrefix(raw, "//") {
		protoRelative = true
		afterScheme = raw[2:]
	} else {
		sep := strings.Index(raw, "://")
		if sep <= 0 {
			return "", false, "", raw, false
		}
		if strings.ContainsAny(raw[:sep], "/?#&=") {
			return "", false, "", raw, false
		}
		scheme = strings.ToLower(raw[:sep])
		afterScheme = raw[sep+3:]
	}

	idx := strings.IndexAny(afterScheme, "/?#")
	if idx == -1 {
		host = afterScheme
		rest = ""
	} else {
		host = afterScheme[:idx]
		rest = afterScheme[idx:]
	}
	return scheme, protoRelative, host, rest, true
}

func applyOpaqueHostReplace(rawURL string, rules []DomainReplaceRule) string {
	for _, rule := range rules {
		if replaced, ok := replaceHostAtBoundary(rawURL, rule.FromHost, rule.ToHost); ok {
			return replaced
		}
	}
	return rawURL
}

func replaceHostAtBoundary(raw, fromHost, toHost string) (string, bool) {
	if fromHost == "" {
		return raw, false
	}
	lower := strings.ToLower(raw)
	from := strings.ToLower(fromHost)
	searchFrom := 0
	for {
		idx := strings.Index(lower[searchFrom:], from)
		if idx < 0 {
			return raw, false
		}
		idx += searchFrom
		if hostBoundaryBefore(lower, idx) && hostBoundaryAfter(lower, idx+len(from)) {
			return raw[:idx] + toHost + raw[idx+len(from):], true
		}
		searchFrom = idx + 1
	}
}

func hostBoundaryBefore(s string, idx int) bool {
	if idx == 0 {
		return true
	}
	if idx >= 3 && s[idx-3:idx] == "://" {
		return true
	}
	return s[idx-1] == '@'
}

func hostBoundaryAfter(s string, idx int) bool {
	if idx >= len(s) {
		return true
	}
	switch s[idx] {
	case '/', ':', '?', '#', '|', '&':
		return true
	default:
		return false
	}
}
