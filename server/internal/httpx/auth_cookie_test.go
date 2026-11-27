package httpx

import "testing"

func TestForwardedProto(t *testing.T) {
	if got := forwardedProto("https, http"); got != "https" {
		t.Fatalf("got %q", got)
	}
	if got := forwardedProto("http"); got != "http" {
		t.Fatalf("got %q", got)
	}
	if got := forwardedProto("HTTPS"); got != "" {
		t.Fatalf("got %q", got)
	}
}
