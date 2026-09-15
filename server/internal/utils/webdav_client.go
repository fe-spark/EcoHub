package utils

import (
	"context"
	"crypto/tls"
	"encoding/xml"
	"errors"
	"fmt"
	"io"
	"log"
	"net/http"
	"net/url"
	"path"
	"strings"
	"time"
)

// CanonicalizeWebDAVRelPath 规范化 WebDAV 内部相对路径：
// 1. 强制去除前导 /（避免 path.Join("/dav/media", "/foo.mkv") 产生 "/foo.mkv" 丢弃 RootPath 的陷阱）
// 2. 清理多余路径符，拒绝包含 ..、:// 等非法跳转
func CanonicalizeWebDAVRelPath(rel string) (string, error) {
	rel = strings.TrimSpace(rel)
	if rel == "" {
		return "", errors.New("相对路径不能为空")
	}
	if strings.Contains(rel, "://") {
		return "", errors.New("相对路径禁止包含协议标识")
	}
	rel = strings.ReplaceAll(rel, "\\", "/")
	rel = strings.TrimPrefix(rel, "/")
	cleaned := path.Clean(rel)
	if cleaned == "." || cleaned == "/" || strings.HasPrefix(cleaned, "../") || cleaned == ".." {
		return "", errors.New("非法路径，禁止越级访问")
	}
	return strings.TrimPrefix(cleaned, "/"), nil
}

// NormalizeWebDAVUri 规范化合成 WebDAV 唯一 URI：
// 格式：webdav|{normalizedServer}|{normalizedRoot}
// 1. url.Parse(ServerURL)，小写 host、去默认端口（80/443）、path 去尾 /
// 2. RootPath 保证以 / 开头、无尾 /（根路径即为 /）
// 3. 总长度超 255 字符返回错误
func NormalizeWebDAVUri(serverURL, rootPath string) (string, error) {
	serverURL = strings.TrimSpace(serverURL)
	if serverURL == "" {
		return "", errors.New("WebDAV 地址不能为空")
	}

	u, err := url.Parse(serverURL)
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return "", errors.New("WebDAV 地址必须以 http:// 或 https:// 开头")
	}

	host := strings.ToLower(u.Hostname())
	port := u.Port()
	if (u.Scheme == "http" && port == "80") || (u.Scheme == "https" && port == "443") {
		port = ""
	}
	hostPort := host
	if port != "" {
		hostPort = host + ":" + port
	}

	serverPath := strings.TrimRight(path.Clean(u.Path), "/")
	if serverPath == "." || serverPath == "/" {
		serverPath = ""
	}
	normalizedServer := fmt.Sprintf("%s://%s%s", u.Scheme, hostPort, serverPath)

	rootPath = strings.TrimSpace(rootPath)
	if rootPath == "" || rootPath == "/" {
		rootPath = "/"
	} else {
		cleaned := path.Clean("/" + strings.Trim(rootPath, "/"))
		if cleaned == "." || cleaned == "" {
			rootPath = "/"
		} else {
			rootPath = cleaned
		}
	}

	uri := fmt.Sprintf("webdav|%s|%s", normalizedServer, rootPath)
	if len(uri) > 255 {
		return "", fmt.Errorf("WebDAV 地址与根路径组合过长（%d 字符），超过系统上限 255 字符", len(uri))
	}
	return uri, nil
}

// BuildWebDAVCollectionURL 计算本次 WebDAV 请求的目标绝对 URL
func BuildWebDAVCollectionURL(serverURL, rootPath string) (*url.URL, error) {
	u, err := url.Parse(strings.TrimSpace(serverURL))
	if err != nil || (u.Scheme != "http" && u.Scheme != "https") {
		return nil, errors.New("无效的 WebDAV 服务地址")
	}

	trimmedRoot := strings.Trim(strings.TrimSpace(rootPath), "/")
	var combinedPath string
	if trimmedRoot == "" {
		combinedPath = u.Path
		if combinedPath == "" {
			combinedPath = "/"
		}
	} else {
		combinedPath = path.Clean(u.Path + "/" + trimmedRoot)
	}

	target := *u
	target.Path = combinedPath
	target.RawPath = ""
	return &target, nil
}

// BuildWebDAVFileURL 在 collection 下拼接文件相对路径，使用 URL Path 而非 path.Join，避免中文/空格被编错。
func BuildWebDAVFileURL(serverURL, rootPath, relPath string) (*url.URL, error) {
	collection, err := BuildWebDAVCollectionURL(serverURL, rootPath)
	if err != nil {
		return nil, err
	}
	rel, err := CanonicalizeWebDAVRelPath(relPath)
	if err != nil {
		return nil, err
	}
	basePath := strings.TrimSuffix(collection.Path, "/")
	joined := basePath + "/" + rel
	if !strings.HasPrefix(joined, "/") {
		joined = "/" + joined
	}
	out := *collection
	out.Path = path.Clean(joined)
	out.RawPath = ""
	out.RawQuery = ""
	out.Fragment = ""
	return &out, nil
}

// WebDAVInsecureTLS 私有 NAS 常用自签证书，扫描与播放共用同一策略。
func WebDAVInsecureTLS() *tls.Config {
	return &tls.Config{InsecureSkipVerify: true} //nolint:gosec
}

// NewWebDAVHttpClient 创建独立的 WebDAV HTTP 客户端，支持自签 TLS 并配置合理超时
func NewWebDAVHttpClient() *http.Client {
	return &http.Client{
		Timeout: 20 * time.Second,
		Transport: &http.Transport{
			TLSClientConfig: WebDAVInsecureTLS(),
		},
	}
}

// TestWebDAVConnection 测试 WebDAV 连通性（PROPFIND Depth: 0）
func annotateWebDAVConnError(serverURL string, err error) error {
	if err == nil {
		return nil
	}
	u, parseErr := url.Parse(strings.TrimSpace(serverURL))
	if parseErr != nil || u.Hostname() == "" {
		return err
	}
	host := strings.ToLower(u.Hostname())
	if host != "127.0.0.1" && host != "localhost" && host != "::1" {
		return err
	}
	msg := strings.ToLower(err.Error())
	if !strings.Contains(msg, "connection refused") &&
		!strings.Contains(msg, "i/o timeout") &&
		!strings.Contains(msg, "no such host") &&
		!strings.Contains(msg, "connect:") {
		return err
	}
	return fmt.Errorf("%w。地址是 127.0.0.1/localhost：宿主机 go run 访问 Docker WebDAV 时，请确认容器已映射该端口（如 -p 5678:5678）；若 EcoHub 也在 Docker 里，请改用 host.docker.internal 或宿主机局域网 IP", err)
}

func TestWebDAVConnection(serverURL, rootPath, username, password string) error {
	targetURL, err := BuildWebDAVCollectionURL(serverURL, rootPath)
	if err != nil {
		return err
	}

	req, err := http.NewRequest("PROPFIND", targetURL.String(), nil)
	if err != nil {
		return fmt.Errorf("构造 PROPFIND 请求失败: %w", err)
	}

	req.Header.Set("Depth", "0")
	if username != "" || password != "" {
		req.SetBasicAuth(username, password)
	}

	client := NewWebDAVHttpClient()
	resp, err := client.Do(req)
	if err != nil {
		return annotateWebDAVConnError(serverURL, fmt.Errorf("连接 WebDAV 服务器失败: %w", err))
	}
	defer resp.Body.Close()

	if resp.StatusCode == http.StatusOK || resp.StatusCode == http.StatusMultiStatus {
		return nil
	}

	if resp.StatusCode == http.StatusUnauthorized {
		authHeader := resp.Header.Get("WWW-Authenticate")
		if strings.Contains(strings.ToLower(authHeader), "digest") {
			return errors.New("暂不支持 Digest 认证，请在 NAS/Alist 改为 Basic，或走 Alist 的 WebDAV")
		}
		return errors.New("认证失败，请检查用户名和密码")
	}

	if resp.StatusCode == http.StatusForbidden {
		return errors.New("访问被拒绝 (403)，请检查用户权限")
	}

	if resp.StatusCode == http.StatusNotFound {
		return errors.New("WebDAV 根路径不存在 (404)，请确认路径配置")
	}

	return fmt.Errorf("WebDAV 测试未通过 (HTTP %d: %s)", resp.StatusCode, resp.Status)
}

// WebdavFileInfo WebDAV 单个视频文件信息
type WebdavFileInfo struct {
	RelPath string    // 内部相对路径（无前导 /，已解码）
	Size    int64     // 文件字节大小
	ETag    string    // 资源 ETag
	ModTime time.Time // 最后修改时间
}

var VideoExtMap = map[string]struct{}{
	".mp4":  {},
	".mkv":  {},
	".avi":  {},
	".mov":  {},
	".ts":   {},
	".m2ts": {},
	".flv":  {},
	".m4v":  {},
	".webm": {},
	".iso":  {},
}

var IgnoredDirMap = map[string]struct{}{
	"@eadir":       {},
	"#recycle":     {},
	"lost+found":   {},
	"$recycle.bin": {},
	".ds_store":    {},
	".trash":       {},
	".thumbnails":  {},
}

// PROPFIND XML 结构支持任何 namespace 前缀（如 <D:multistatus> 或 <d:multistatus>）
type propfindMultiStatus struct {
	XMLName   xml.Name           `xml:"multistatus"`
	Responses []propfindResponse `xml:"response"`
}

type propfindResponse struct {
	Href     string             `xml:"href"`
	Propstat []propfindPropstat `xml:"propstat"`
}

type propfindPropstat struct {
	Prop   propfindProp `xml:"prop"`
	Status string       `xml:"status"`
}

type propfindProp struct {
	ResourceType  propfindResourceType `xml:"resourcetype"`
	ContentLength *int64               `xml:"getcontentlength"`
	LastModified  string               `xml:"getlastmodified"`
	ETag          string               `xml:"getetag"`
}

type propfindResourceType struct {
	Collection *struct{} `xml:"collection"`
}

// WebDAVListFileLimit 单次扫描最多收录的视频文件数，达到后 truncated=true。
var WebDAVListFileLimit = 5000

// ListWebDAVFiles 通过 PROPFIND Depth:1 BFS 遍历 WebDAV 目录下的视频文件：
// 1. 达到 WebDAVListFileLimit 即截断 (truncated = true)；
// 2. 根目录列举失败立即阻断；子目录 404/超时等跳过该分支，避免单个坏目录拖垮整次扫描；
// 3. 忽略隐藏目录及 NAS 系统目录（如 @eaDir, #recycle 等）；
// 4. minFileBytes>0 时忽略小于该阈值的文件（.iso 除外）；<=0 表示不过滤大小。
type webdavQueueItem struct {
	rel    string
	reqURL string
}

func resolveWebDAVChildURL(reqBase, collection *url.URL, rawHref string) string {
	base := collection
	if reqBase != nil {
		base = reqBase
	}
	ref, err := url.Parse(strings.TrimSpace(rawHref))
	if err != nil {
		return ""
	}
	resolved := base.ResolveReference(ref)
	out := resolved.String()
	if !strings.HasSuffix(out, "/") {
		out += "/"
	}
	return out
}

func ListWebDAVFiles(ctx context.Context, serverURL, rootPath, username, password string, minFileBytes int64) ([]WebdavFileInfo, bool, error) {
	return ListWebDAVFilesProgress(ctx, serverURL, rootPath, username, password, minFileBytes, nil)
}

func ListWebDAVFilesProgress(ctx context.Context, serverURL, rootPath, username, password string, minFileBytes int64, onListed func(int)) ([]WebdavFileInfo, bool, error) {
	targetURL, err := BuildWebDAVCollectionURL(serverURL, rootPath)
	if err != nil {
		return nil, false, err
	}

	collectionPath := path.Clean(targetURL.Path)
	if collectionPath == "." || collectionPath == "" {
		collectionPath = "/"
	}

	client := NewWebDAVHttpClient()
	client.Timeout = 60 * time.Second
	rootReq := *targetURL
	rootReq.Path = collectionPath
	rootReqStr := rootReq.String()
	if !strings.HasSuffix(rootReqStr, "/") {
		rootReqStr += "/"
	}
	queue := []webdavQueueItem{{rel: "", reqURL: rootReqStr}}
	visitedDirs := make(map[string]struct{})
	var results []WebdavFileInfo
	truncated := false

BFS:
	for len(queue) > 0 {
		select {
		case <-ctx.Done():
			return nil, false, ctx.Err()
		default:
		}

		current := queue[0]
		queue = queue[1:]
		currentRel := current.rel
		reqURLStr := current.reqURL
		if reqURLStr == "" {
			reqURLStr = rootReqStr
		}

		reqBase, _ := url.Parse(reqURLStr)
		dirPath := collectionPath
		if reqBase != nil && reqBase.Path != "" {
			unescaped, err := url.PathUnescape(reqBase.Path)
			if err != nil {
				unescaped = reqBase.Path
			}
			dirPath = path.Clean(unescaped)
		} else if currentRel != "" {
			dirPath = path.Clean(collectionPath + "/" + currentRel)
		}

		req, err := http.NewRequestWithContext(ctx, "PROPFIND", reqURLStr, nil)
		if err != nil {
			return nil, false, fmt.Errorf("构造 PROPFIND 请求失败: %w", err)
		}
		req.Header.Set("Depth", "1")
		if username != "" || password != "" {
			req.SetBasicAuth(username, password)
		}

		resp, err := client.Do(req)
		if err != nil {
			wrapped := annotateWebDAVConnError(serverURL, fmt.Errorf("WebDAV PROPFIND 连接失败 (%s): %w", currentRel, err))
			if currentRel != "" {
				log.Printf("[WebDAV] 跳过子目录 %s: %v", currentRel, wrapped)
				continue
			}
			return nil, false, wrapped
		}

		if resp.StatusCode != http.StatusOK && resp.StatusCode != http.StatusMultiStatus {
			resp.Body.Close()
			if resp.StatusCode == http.StatusUnauthorized {
				authHeader := resp.Header.Get("WWW-Authenticate")
				if strings.Contains(strings.ToLower(authHeader), "digest") {
					return nil, false, errors.New("暂不支持 Digest 认证，请在 NAS/Alist 改为 Basic，或走 Alist 的 WebDAV")
				}
				return nil, false, errors.New("认证失败，请检查用户名和密码")
			}
			if currentRel != "" {
				log.Printf("[WebDAV] 跳过子目录 %s: HTTP %d", currentRel, resp.StatusCode)
				continue
			}
			if resp.StatusCode == http.StatusForbidden {
				return nil, false, errors.New("访问被拒绝 (403)，请检查用户权限")
			}
			if resp.StatusCode == http.StatusNotFound {
				return nil, false, fmt.Errorf("目录不存在 (404): %s", currentRel)
			}
			return nil, false, fmt.Errorf("WebDAV PROPFIND 返回异常状态 (HTTP %d: %s)", resp.StatusCode, resp.Status)
		}

		body, err := io.ReadAll(resp.Body)
		resp.Body.Close()
		if err != nil {
			if currentRel != "" {
				log.Printf("[WebDAV] 跳过子目录 %s: 读取响应失败: %v", currentRel, err)
				continue
			}
			return nil, false, fmt.Errorf("读取 WebDAV 响应失败: %w", err)
		}

		var ms propfindMultiStatus
		if err := xml.Unmarshal(body, &ms); err != nil {
			if currentRel != "" {
				log.Printf("[WebDAV] 跳过子目录 %s: 解析 XML 失败: %v", currentRel, err)
				continue
			}
			return nil, false, fmt.Errorf("解析 WebDAV XML 失败: %w", err)
		}

		for _, item := range ms.Responses {
			rawHref := strings.TrimSpace(item.Href)
			if rawHref == "" {
				continue
			}

			u, err := url.Parse(rawHref)
			if err != nil {
				continue
			}

			unescapedPath, err := url.PathUnescape(u.Path)
			if err != nil {
				unescapedPath = u.Path
			}
			cleanHrefPath := path.Clean(unescapedPath)

			// 忽略当前目录本身
			if cleanHrefPath == dirPath || cleanHrefPath == collectionPath {
				continue
			}

			// 必须在本次 collection 范围内
			var relFromCollection string
			if collectionPath == "/" {
				relFromCollection = strings.TrimPrefix(cleanHrefPath, "/")
			} else {
				prefix := collectionPath + "/"
				if !strings.HasPrefix(cleanHrefPath, prefix) {
					continue
				}
				relFromCollection = strings.TrimPrefix(cleanHrefPath, prefix)
			}

			canonicalRel, err := CanonicalizeWebDAVRelPath(relFromCollection)
			if err != nil {
				continue
			}

			isDir := false
			for _, ps := range item.Propstat {
				if ps.Prop.ResourceType.Collection != nil {
					isDir = true
					break
				}
			}
			if !isDir && (strings.HasSuffix(rawHref, "/") || strings.HasSuffix(unescapedPath, "/")) {
				isDir = true
			}

			if isDir {
				baseDir := strings.ToLower(path.Base(canonicalRel))
				if strings.HasPrefix(baseDir, ".") {
					continue
				}
				if _, ignored := IgnoredDirMap[baseDir]; ignored {
					continue
				}
				if _, visited := visitedDirs[canonicalRel]; !visited {
					childURL := resolveWebDAVChildURL(reqBase, targetURL, rawHref)
					if childURL == "" {
						continue
					}
					visitedDirs[canonicalRel] = struct{}{}
					queue = append(queue, webdavQueueItem{rel: canonicalRel, reqURL: childURL})
				}
			} else {
				baseFile := path.Base(canonicalRel)
				if strings.HasPrefix(baseFile, ".") || strings.HasPrefix(baseFile, "._") {
					continue
				}
				ext := strings.ToLower(path.Ext(baseFile))
				if _, ok := VideoExtMap[ext]; !ok {
					continue
				}

				var size int64
				var modTime time.Time
				var etag string
				for _, ps := range item.Propstat {
					if ps.Prop.ContentLength != nil && *ps.Prop.ContentLength > 0 {
						size = *ps.Prop.ContentLength
					}
					if ps.Prop.ETag != "" {
						etag = strings.Trim(ps.Prop.ETag, "\"")
					}
					if ps.Prop.LastModified != "" {
						if t, err := http.ParseTime(ps.Prop.LastModified); err == nil {
							modTime = t
						} else if t, err := time.Parse(time.RFC3339, ps.Prop.LastModified); err == nil {
							modTime = t
						}
					}
				}

				if minFileBytes > 0 && ext != ".iso" && size < minFileBytes {
					continue
				}

				results = append(results, WebdavFileInfo{
					RelPath: canonicalRel,
					Size:    size,
					ETag:    etag,
					ModTime: modTime,
				})
				if onListed != nil {
					onListed(len(results))
				}

				if WebDAVListFileLimit > 0 && len(results) >= WebDAVListFileLimit {
					truncated = true
					break BFS
				}
			}
		}
	}

	return results, truncated, nil
}
