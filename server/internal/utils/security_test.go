package utils

import (
	"net"
	"strings"
	"testing"
)

func TestPasswordEncryptAndVerify(t *testing.T) {
	hash := PasswordEncrypt("secret-pass", "salt-1")
	if !strings.HasPrefix(hash, "$2") {
		t.Fatalf("expected bcrypt hash, got %q", hash)
	}
	ok, needsUpgrade := VerifyPassword("secret-pass", "salt-1", hash)
	if !ok || needsUpgrade {
		t.Fatalf("bcrypt verify = (%v, %v), want (true, false)", ok, needsUpgrade)
	}
	if ok, _ := VerifyPassword("wrong", "salt-1", hash); ok {
		t.Fatal("wrong password should not verify")
	}

	legacy := legacyMD5PasswordEncrypt("legacy-pass", "salt-2")
	ok, needsUpgrade = VerifyPassword("legacy-pass", "salt-2", legacy)
	if !ok || !needsUpgrade {
		t.Fatalf("legacy MD5 verify = (%v, %v), want (true, true)", ok, needsUpgrade)
	}

	// 超过 72 字节密码走 SHA-256 预哈希，区分第 73 字节差异。
	longA := strings.Repeat("a", 72) + "X"
	longB := strings.Repeat("a", 72) + "Y"
	longHash := PasswordEncrypt(longA, "s")
	if ok, _ := VerifyPassword(longA, "s", longHash); !ok {
		t.Fatal("long password should verify")
	}
	if ok, _ := VerifyPassword(longB, "s", longHash); ok {
		t.Fatal("long password differing past byte 72 must not match")
	}
}

func TestIsPublicOutboundIP(t *testing.T) {
	blocked := []string{"127.0.0.1", "10.0.0.1", "172.16.0.5", "192.168.1.1", "169.254.169.254", "100.64.0.1", "100.127.255.254", "::1", "fc00::1", "fe80::1"}
	for _, s := range blocked {
		if IsPublicOutboundIP(net.ParseIP(s)) {
			t.Fatalf("expected %s to be blocked", s)
		}
	}
	allowed := []string{"8.8.8.8", "1.1.1.1", "2001:4860:4860::8888"}
	for _, s := range allowed {
		if !IsPublicOutboundIP(net.ParseIP(s)) {
			t.Fatalf("expected %s to be allowed", s)
		}
	}
}
