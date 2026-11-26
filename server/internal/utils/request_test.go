package utils

import (
	"net/http"
	"testing"
)

func TestGetOrCreateProxyTransportHTTP(t *testing.T) {
	tr := GetOrCreateProxyTransport("http://127.0.0.1:7890")
	if tr == sharedTransport {
		t.Fatal("http proxy reused the direct transport")
	}
	if tr.Proxy == nil {
		t.Fatal("http proxy transport has no Proxy")
	}
	if tr.DialContext == nil || tr.TLSHandshakeTimeout <= 0 {
		t.Fatal("http proxy transport should configure dial and TLS handshake timeouts")
	}
	req, err := http.NewRequest(http.MethodGet, "https://example.com/vod", nil)
	if err != nil {
		t.Fatal(err)
	}
	got, err := tr.Proxy(req)
	if err != nil {
		t.Fatal(err)
	}
	if got == nil || got.Host != "127.0.0.1:7890" {
		t.Fatalf("proxy url = %v", got)
	}
}

func TestGetOrCreateProxyTransportSOCKS5(t *testing.T) {
	tr := GetOrCreateProxyTransport("socks5://user:pass@127.0.0.1:1080")
	if tr == sharedTransport {
		t.Fatal("socks5 proxy reused the direct transport")
	}
	if tr.Proxy != nil {
		t.Fatal("socks5 must not use HTTP CONNECT Proxy")
	}
	if tr.DialContext == nil {
		t.Fatal("socks5 transport has no dialer")
	}
}

func TestGetOrCreateProxyTransportRejectsUnknownScheme(t *testing.T) {
	tr := GetOrCreateProxyTransport("ftp://127.0.0.1:21")
	if tr == sharedTransport {
		t.Fatal("invalid proxy fell back to a direct connection")
	}
	_, err := tr.DialContext(t.Context(), "tcp", "example.com:443")
	if err == nil {
		t.Fatal("expected dial error for unsupported proxy scheme")
	}
	proxyTransportsMu.RLock()
	_, cached := proxyTransports["ftp://127.0.0.1:21"]
	proxyTransportsMu.RUnlock()
	if cached {
		t.Fatal("failed proxy transport must not be cached")
	}
}

func TestResetProxyTransportsClearsPool(t *testing.T) {
	_ = GetOrCreateProxyTransport("http://127.0.0.1:17890")
	ResetProxyTransports()
	proxyTransportsMu.RLock()
	n := len(proxyTransports)
	proxyTransportsMu.RUnlock()
	if n != 0 {
		t.Fatalf("expected proxyTransports to be cleared, got %d", n)
	}
}

func TestApiGet_NilRequestSafe(t *testing.T) {
	ApiGet(nil)
}

func TestApiGet_InvalidProxyWithSkipTLSVerifyNotCached(t *testing.T) {
	ResetProxyTransports()
	r := &RequestInfo{
		Uri:           "https://127.0.0.1:1",
		ProxyURL:      "ftp://127.0.0.1:21",
		SkipTLSVerify: true,
	}
	ApiGet(r)
	if r.Err == "" {
		t.Fatal("expected error for invalid proxy scheme")
	}
	proxyInsecureTransportsMu.RLock()
	_, cached := proxyInsecureTransports["ftp://127.0.0.1:21"]
	proxyInsecureTransportsMu.RUnlock()
	if cached {
		t.Fatal("failed proxy transport must not be cached in proxyInsecureTransports")
	}
}
