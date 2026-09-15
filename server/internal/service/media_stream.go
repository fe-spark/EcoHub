package service

import (
	"crypto/hmac"
	"crypto/sha256"
	"encoding/base64"
	"encoding/hex"
	"fmt"
	"net/url"
	"os"
	"path/filepath"
	"strconv"
	"strings"

	"server/internal/model"
)

const WdvSchemePrefix = "wdv://"


// GetMediaStreamSecret 获取媒体流签名密钥。
// 优先读取 MEDIA_STREAM_SECRET；若为空，则基于 JWT_SECRET 派生 SHA256 独立密钥，避免直接暴露或耦合登录密钥。
func GetMediaStreamSecret() string {
	if secret := strings.TrimSpace(os.Getenv("MEDIA_STREAM_SECRET")); secret != "" {
		return secret
	}
	jwtSecret := strings.TrimSpace(os.Getenv("JWT_SECRET"))
	if jwtSecret == "" {
		jwtSecret = "ecohub_default_media_secret"
	}
	sum := sha256.Sum256([]byte("ecohub-media-stream:" + jwtSecret))
	return hex.EncodeToString(sum[:])
}

// GenerateMediaStreamSign 生成媒体网关 HMAC-SHA256 签名。
func GenerateMediaStreamSign(sourceID int64, canonicalRelPath string) string {
	secret := GetMediaStreamSecret()
	mac := hmac.New(sha256.New, []byte(secret))
	mac.Write([]byte(fmt.Sprintf("%d\n%s", sourceID, canonicalRelPath)))
	return hex.EncodeToString(mac.Sum(nil))
}

// VerifyMediaStreamSign 校验媒体网关 HMAC-SHA256 签名。
func VerifyMediaStreamSign(sourceID int64, canonicalRelPath string, sign string) bool {
	if sign == "" {
		return false
	}
	expected := GenerateMediaStreamSign(sourceID, canonicalRelPath)
	return hmac.Equal([]byte(expected), []byte(sign))
}

// BuildMediaStreamURL 构建带出口签名的媒体流访问 URL。
func BuildMediaStreamURL(streamBase string, sourceID int64, canonicalRelPath string) string {
	ext := strings.ToLower(strings.TrimPrefix(filepath.Ext(canonicalRelPath), "."))
	sign := GenerateMediaStreamSign(sourceID, canonicalRelPath)
	p := base64.RawURLEncoding.EncodeToString([]byte(canonicalRelPath))

	query := url.Values{}
	query.Set("sid", strconv.FormatInt(sourceID, 10))
	query.Set("p", p)
	if ext != "" {
		query.Set("ext", ext)
	}
	query.Set("sign", sign)

	base := strings.TrimRight(strings.TrimSpace(streamBase), "/")
	if base == "" {
		return "/api/media/stream?" + query.Encode()
	}
	return base + "/api/media/stream?" + query.Encode()
}

// ParseWdvLink 从 wdv://{sourceId}/{encodedRelPath} 解析出 sourceId 与规范化相对路径。
func ParseWdvLink(link string) (int64, string, bool) {
	if !strings.HasPrefix(link, WdvSchemePrefix) {
		return 0, "", false
	}
	rest := strings.TrimPrefix(link, WdvSchemePrefix)
	parts := strings.SplitN(rest, "/", 2)
	if len(parts) != 2 {
		return 0, "", false
	}
	sid, err := strconv.ParseInt(parts[0], 10, 64)
	if err != nil || sid <= 0 {
		return 0, "", false
	}
	rawBytes, err := base64.RawURLEncoding.DecodeString(parts[1])
	if err != nil {
		rawBytes, err = base64.URLEncoding.DecodeString(parts[1])
		if err != nil {
			return 0, "", false
		}
	}
	canonical, err := CanonicalizeWebDAVRelPath(string(rawBytes))
	if err != nil || canonical == "" {
		return 0, "", false
	}
	return sid, canonical, true
}

// SignWdvLink 将 wdv:// 开头的存储链路转换为网关签名链路；非 wdv:// 链路（如公网 m3u8）原样返回。
func SignWdvLink(streamBase string, link string) string {
	sid, relPath, ok := ParseWdvLink(link)
	if !ok {
		return link
	}
	return BuildMediaStreamURL(streamBase, sid, relPath)
}

// SignWdvLinks 对详情/播放列表里的所有 wdv:// 链路进行出口签名转换，深拷贝防止污染原结构。
func SignWdvLinks(streamBase string, list []model.PlayLinkVo) []model.PlayLinkVo {
	if len(list) == 0 {
		return list
	}
	res := make([]model.PlayLinkVo, len(list))
	for i, group := range list {
		res[i] = group
		res[i].LinkList = make([]model.MovieUrlInfo, len(group.LinkList))
		for j, item := range group.LinkList {
			res[i].LinkList[j] = item
			if strings.HasPrefix(item.Link, WdvSchemePrefix) {
				res[i].LinkList[j].Link = SignWdvLink(streamBase, item.Link)
			}
		}
	}
	return res
}
