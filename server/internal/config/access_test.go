package config

import "testing"

func TestParseTrustedProxies(t *testing.T) {
	got := ParseTrustedProxies("")
	if len(got) != 2 || got[0] != "127.0.0.1" || got[1] != "::1" {
		t.Fatalf("default: %v", got)
	}
	got = ParseTrustedProxies(" 172.17.0.1 , 10.0.0.1 ")
	if len(got) != 2 || got[0] != "172.17.0.1" || got[1] != "10.0.0.1" {
		t.Fatalf("custom: %v", got)
	}
	got = ParseTrustedProxies(" , ")
	if len(got) != 2 {
		t.Fatalf("blank fallback: %v", got)
	}
}
