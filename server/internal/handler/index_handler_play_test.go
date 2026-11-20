package handler

import (
	"testing"
)

func TestIsWebPlayableVideo(t *testing.T) {
	tests := []struct {
		url  string
		want bool
	}{
		{"https://cdn.example.com/live/index.m3u8", true},
		{"https://cdn.example.com/video.mp4?auth=xyz", true},
		{"http://cdn.example.com/stream.flv", true},
		{"https://cdn.example.com/video.webm", true},
		{"/media/video.mp4", true},
		{"https://jx.player.com/?url=https://cdn.example.com/1.m3u8", false},
		{"https://jx.player.com/?url=http://v.qq.com/x/cover/123.html", false},
		{"https://vip.example.com/share/32917", false},
		{"https://vip.example.com/play/1001.html", false},
		{"", false},
	}

	for _, tt := range tests {
		if got := isWebPlayableVideo(tt.url); got != tt.want {
			t.Errorf("isWebPlayableVideo(%q) = %v, want %v", tt.url, got, tt.want)
		}
	}
}
