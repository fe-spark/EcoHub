package service

import (
	"encoding/base64"
	"net/url"
	"os"
	"path"
	"strconv"
	"testing"

	"server/internal/model"
)


func TestWebDAVPathJoin_CollectionPathNotLost(t *testing.T) {
	// 验证 path.Join 在各种 collectionPath 下拼接 canonical relPath 时，不会丢失前缀 /dav/media
	tests := []struct {
		collectionPath string
		rel            string
		expected       string
	}{
		{"/dav/media", "season1/ep1.mkv", "/dav/media/season1/ep1.mkv"},
		{"/dav/media/", "season1/ep1.mkv", "/dav/media/season1/ep1.mkv"},
		{"/dav/media", "movie.mkv", "/dav/media/movie.mkv"},
		{"/", "ep1.mkv", "/ep1.mkv"},
		{"", "ep1.mkv", "ep1.mkv"},
	}

	for _, tc := range tests {
		canonicalRel, err := CanonicalizeWebDAVRelPath(tc.rel)
		if err != nil {
			t.Fatalf("unexpected error for %q: %v", tc.rel, err)
		}
		got := path.Join(tc.collectionPath, canonicalRel)
		if got != tc.expected {
			t.Errorf("path.Join(%q, Canonicalize(%q)) = %q, want %q", tc.collectionPath, tc.rel, got, tc.expected)
		}
	}
}

func TestMediaStreamSignAndVerify(t *testing.T) {
	os.Setenv("MEDIA_STREAM_SECRET", "test_stream_secret_123")
	defer os.Unsetenv("MEDIA_STREAM_SECRET")

	sourceID := int64(101)
	relPath := "anime/frieren/ep01.mkv"

	sign := GenerateMediaStreamSign(sourceID, relPath)
	if sign == "" {
		t.Fatal("expected non-empty sign")
	}

	// 校验正确签名
	if !VerifyMediaStreamSign(sourceID, relPath, sign) {
		t.Errorf("VerifyMediaStreamSign failed for valid sign")
	}

	// 校验篡改 sourceID
	if VerifyMediaStreamSign(102, relPath, sign) {
		t.Errorf("VerifyMediaStreamSign should fail for mismatched sourceID")
	}

	// 校验篡改 relPath
	if VerifyMediaStreamSign(sourceID, "anime/frieren/ep02.mkv", sign) {
		t.Errorf("VerifyMediaStreamSign should fail for mismatched relPath")
	}

	// 校验空签名
	if VerifyMediaStreamSign(sourceID, relPath, "") {
		t.Errorf("VerifyMediaStreamSign should fail for empty sign")
	}
}

func TestBuildMediaStreamURLAndSignWdvLinks(t *testing.T) {
	os.Setenv("MEDIA_STREAM_SECRET", "test_secret_abc")
	defer os.Unsetenv("MEDIA_STREAM_SECRET")

	sourceID := int64(88)
	relPath := "Movies/Inception (2010).mkv"
	encodedRel := base64.RawURLEncoding.EncodeToString([]byte(relPath))
	wdvLink := "wdv://" + strconv.FormatInt(sourceID, 10) + "/" + encodedRel

	// 1. 测试相对路径签名
	relativeSigned := SignWdvLink("", wdvLink)
	if parsed, err := url.Parse(relativeSigned); err != nil {
		t.Fatalf("failed to parse relativeSigned: %v", err)
	} else {
		if parsed.Path != "/api/media/stream" {
			t.Errorf("expected path /api/media/stream, got %s", parsed.Path)
		}
		q := parsed.Query()
		if q.Get("sid") != "88" || q.Get("ext") != "mkv" || q.Get("sign") == "" || q.Get("p") != encodedRel {
			t.Errorf("unexpected query params: %v", q)
		}
		if !VerifyMediaStreamSign(88, relPath, q.Get("sign")) {
			t.Errorf("signature verification failed on built url")
		}
	}

	// 2. 测试绝对路径签名（带 streamBase）
	streamBase := "http://192.168.1.50:18080"
	absoluteSigned := SignWdvLink(streamBase, wdvLink)
	if parsed, err := url.Parse(absoluteSigned); err != nil {
		t.Fatalf("failed to parse absoluteSigned: %v", err)
	} else {
		if parsed.Scheme != "http" || parsed.Host != "192.168.1.50:18080" || parsed.Path != "/api/media/stream" {
			t.Errorf("unexpected absolute URL: %s", absoluteSigned)
		}
	}

	// 3. 测试公网 m3u8 不动
	m3u8Link := "https://cdn.example.com/live/stream.m3u8"
	if got := SignWdvLink(streamBase, m3u8Link); got != m3u8Link {
		t.Errorf("SignWdvLink should not touch m3u8 link, got %s", got)
	}

	// 4. 测试 SignWdvLinks 批量签名深拷贝
	playGroups := []model.PlayLinkVo{
		{
			Id:       "webdav_group",
			SourceId: "88",
			Name:     "WebDAV 私有云",
			LinkList: []model.MovieUrlInfo{
				{Episode: "第1集", Link: wdvLink},
			},
		},
		{
			Id:       "maccms_group",
			SourceId: "1",
			Name:     "秒播线路",
			LinkList: []model.MovieUrlInfo{
				{Episode: "第1集", Link: m3u8Link},
			},
		},
	}

	signedGroups := SignWdvLinks("http://192.168.1.50:18080", playGroups)
	if len(signedGroups) != 2 {
		t.Fatalf("expected 2 groups, got %d", len(signedGroups))
	}
	if signedGroups[0].LinkList[0].Link == wdvLink {
		t.Errorf("expected WebDAV link to be converted, got %s", signedGroups[0].LinkList[0].Link)
	}
	if signedGroups[1].LinkList[0].Link != m3u8Link {
		t.Errorf("expected m3u8 link to remain untouched, got %s", signedGroups[1].LinkList[0].Link)
	}
	// 确认原结构未被篡改
	if playGroups[0].LinkList[0].Link != wdvLink {
		t.Errorf("original playGroups should not be mutated")
	}
}
