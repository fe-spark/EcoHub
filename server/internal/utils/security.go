package utils

import (
	"crypto/md5"
	"crypto/sha256"
	"crypto/subtle"
	"encoding/hex"
	"fmt"
	"net"
	"net/netip"
	"strings"

	"server/internal/config"

	"golang.org/x/crypto/bcrypt"
)

func bcryptInput(password, salt string) []byte {
	raw := password + salt
	if len(raw) <= 72 {
		return []byte(raw)
	}
	sum := sha256.Sum256([]byte(raw))
	return []byte(hex.EncodeToString(sum[:]))
}

func legacyMD5PasswordEncrypt(password, salt string) string {
	b := fmt.Append(nil, password, salt)
	var r [16]byte
	for range 3 {
		r = md5.Sum(b)
		b = []byte(hex.EncodeToString(r[:]))
	}
	return hex.EncodeToString(r[:])
}

// PasswordEncrypt 使用 bcrypt（默认 cost 10）生成高强度密码哈希。
func PasswordEncrypt(password, salt string) string {
	hash, err := bcrypt.GenerateFromPassword(bcryptInput(password, salt), bcrypt.DefaultCost)
	if err != nil {
		return ""
	}
	return string(hash)
}

// VerifyPassword 校验明文密码；命中历史 3 轮 MD5 哈希时同时返回 needsUpgrade=true 以便登录时无感升级为 bcrypt。
func VerifyPassword(password, salt, hashed string) (matched bool, needsUpgrade bool) {
	if hashed == "" {
		return false, false
	}
	if strings.HasPrefix(hashed, "$2") {
		return bcrypt.CompareHashAndPassword([]byte(hashed), bcryptInput(password, salt)) == nil, false
	}
	legacy := legacyMD5PasswordEncrypt(password, salt)
	if subtle.ConstantTimeCompare([]byte(hashed), []byte(legacy)) == 1 {
		return true, true
	}
	return false, false
}

// IsTrustedForwardedPeer 仅当直连对端 TCP 地址匹配 config.TrustedProxies（或单测空地址）时才采信 X-Forwarded-* 请求头。
func IsTrustedForwardedPeer(remoteAddr string) bool {
	return config.IsTrustedProxyPeer(remoteAddr)
}

// IsPublicOutboundIP 校验 IP 是否为可安全访问的公网地址，拒绝环回、私网、链路本地与 CGNAT（100.64.0.0/10）网段。
func IsPublicOutboundIP(ip net.IP) bool {
	if ip == nil {
		return false
	}
	if ip4 := ip.To4(); ip4 != nil {
		if ip4[0] == 100 && ip4[1] >= 64 && ip4[1] <= 127 {
			return false
		}
		ip = ip4
	}
	return !ip.IsLoopback() && !ip.IsPrivate() && !ip.IsLinkLocalUnicast() && !ip.IsLinkLocalMulticast() && !ip.IsMulticast() && !ip.IsUnspecified()
}

// NormalizeClientIPKey 将客户端 IP 归一为限流统计键，IPv6 归约至 /64 前缀。
func NormalizeClientIPKey(ip string) string {
	ip = strings.TrimSpace(ip)
	if ip == "" {
		return "unknown"
	}
	addr, err := netip.ParseAddr(ip)
	if err != nil {
		return ip
	}
	addr = addr.Unmap()
	if addr.Is6() {
		if prefix, pErr := addr.Prefix(64); pErr == nil {
			return prefix.Masked().String()
		}
	}
	return addr.String()
}
