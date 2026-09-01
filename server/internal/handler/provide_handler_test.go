package handler

import (
	"testing"
)

func TestNormalizeMediaURL(t *testing.T) {
	tests := []struct {
		name    string
		raw     string
		baseURL string
		want    string
	}{
		{
			name:    "empty raw",
			raw:     "",
			baseURL: "https://example.com",
			want:    "",
		},
		{
			name:    "empty baseURL",
			raw:     "/path/to/pic.jpg",
			baseURL: "",
			want:    "/path/to/pic.jpg",
		},
		{
			name:    "absolute http url",
			raw:     "http://img.test.com/pic.jpg",
			baseURL: "https://example.com",
			want:    "http://img.test.com/pic.jpg",
		},
		{
			name:    "absolute https url",
			raw:     "https://img.test.com/pic.jpg",
			baseURL: "https://example.com",
			want:    "https://img.test.com/pic.jpg",
		},
		{
			name:    "protocol-relative url with https base",
			raw:     "//img.doubanio.com/view/photo/p123.jpg",
			baseURL: "https://example.com",
			want:    "https://img.doubanio.com/view/photo/p123.jpg",
		},
		{
			name:    "protocol-relative url with http base",
			raw:     "//img.doubanio.com/view/photo/p123.jpg",
			baseURL: "http://example.com",
			want:    "http://img.doubanio.com/view/photo/p123.jpg",
		},
		{
			name:    "root-relative url",
			raw:     "/api/upload/pic/poster/abc.jpg",
			baseURL: "https://example.com",
			want:    "https://example.com/api/upload/pic/poster/abc.jpg",
		},
		{
			name:    "relative url without leading slash",
			raw:     "api/upload/pic/poster/abc.jpg",
			baseURL: "https://example.com",
			want:    "https://example.com/api/upload/pic/poster/abc.jpg",
		},
	}

	for _, tt := range tests {
		t.Run(tt.name, func(t *testing.T) {
			got := normalizeMediaURL(tt.raw, tt.baseURL)
			if got != tt.want {
				t.Errorf("normalizeMediaURL(%q, %q) = %q, want %q", tt.raw, tt.baseURL, got, tt.want)
			}
		})
	}
}
