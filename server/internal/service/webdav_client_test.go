package service

import (
	"context"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/model"
	"server/internal/utils"
)

func TestBuildWebDAVFileURL_ChinesePath(t *testing.T) {
	u, err := BuildWebDAVFileURL("http://127.0.0.1:5678/dav", "/每日更新/动漫", "FATE剧场版/命运之夜.mkv")
	if err != nil {
		t.Fatalf("BuildWebDAVFileURL: %v", err)
	}
	got := u.String()
	if !strings.Contains(got, "/dav/") || !strings.Contains(got, ".mkv") {
		t.Fatalf("unexpected url %s", got)
	}
	if strings.Contains(got, "\\") {
		t.Fatalf("url must not contain backslash: %s", got)
	}
}

func TestCanonicalizeWebDAVRelPath(t *testing.T) {
	tests := []struct {
		input   string
		want    string
		wantErr bool
	}{
		{"foo.mkv", "foo.mkv", false},
		{"/foo.mkv", "foo.mkv", false},
		{"///a/b/c.mp4", "a/b/c.mp4", false},
		{"a/../b/c.mp4", "b/c.mp4", false},
		{"..", "", true},
		{"../etc/passwd", "", true},
		{"http://evil.com/a.mkv", "", true},
		{"", "", true},
		{"   ", "", true},
	}

	for _, tt := range tests {
		got, err := CanonicalizeWebDAVRelPath(tt.input)
		if (err != nil) != tt.wantErr {
			t.Errorf("CanonicalizeWebDAVRelPath(%q) error = %v, wantErr %v", tt.input, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("CanonicalizeWebDAVRelPath(%q) = %q, want %q", tt.input, got, tt.want)
		}
	}
}

func TestNormalizeWebDAVUri(t *testing.T) {
	tests := []struct {
		serverURL string
		rootPath  string
		want      string
		wantErr   bool
	}{
		{
			serverURL: "http://NAS:5005/dav/",
			rootPath:  "/media",
			want:      "webdav|http://nas:5005/dav|/media",
			wantErr:   false,
		},
		{
			serverURL: "http://host:80/dav",
			rootPath:  "/",
			want:      "webdav|http://host/dav|/",
			wantErr:   false,
		},
		{
			serverURL: "https://secure.nas.com:443/dav",
			rootPath:  "media/movies/",
			want:      "webdav|https://secure.nas.com/dav|/media/movies",
			wantErr:   false,
		},
		{
			serverURL: "ftp://nas/dav",
			rootPath:  "/",
			want:      "",
			wantErr:   true,
		},
		{
			serverURL: "http://host/" + strings.Repeat("a", 250),
			rootPath:  "/",
			want:      "",
			wantErr:   true,
		},
	}

	for _, tt := range tests {
		got, err := NormalizeWebDAVUri(tt.serverURL, tt.rootPath)
		if (err != nil) != tt.wantErr {
			t.Errorf("NormalizeWebDAVUri(%q, %q) error = %v, wantErr %v", tt.serverURL, tt.rootPath, err, tt.wantErr)
			continue
		}
		if got != tt.want {
			t.Errorf("NormalizeWebDAVUri(%q, %q) = %q, want %q", tt.serverURL, tt.rootPath, got, tt.want)
		}
	}
}

func TestTestWebDAVConnection(t *testing.T) {
	handlerFunc := http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Depth") != "0" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		switch r.URL.Path {
		case "/dav/ok207":
			w.WriteHeader(http.StatusMultiStatus)
		case "/dav/ok200":
			w.WriteHeader(http.StatusOK)
		case "/dav/digest":
			w.Header().Set("WWW-Authenticate", `Digest realm="test"`)
			w.WriteHeader(http.StatusUnauthorized)
		case "/dav/unauth":
			w.WriteHeader(http.StatusUnauthorized)
		case "/dav/forbidden":
			w.WriteHeader(http.StatusForbidden)
		case "/dav/notfound":
			w.WriteHeader(http.StatusNotFound)
		default:
			w.WriteHeader(http.StatusInternalServerError)
		}
	})

	ts := httptest.NewServer(handlerFunc)
	defer ts.Close()

	// 1. 测试 207 Multi-Status
	err := TestWebDAVConnection(model.WebdavConfig{
		ServerURL: ts.URL,
		RootPath:  "/dav/ok207",
	})
	if err != nil {
		t.Errorf("Expected success for ok207, got %v", err)
	}

	// 2. 测试 200 OK
	err = TestWebDAVConnection(model.WebdavConfig{
		ServerURL: ts.URL,
		RootPath:  "/dav/ok200",
	})
	if err != nil {
		t.Errorf("Expected success for ok200, got %v", err)
	}

	// 3. 测试 Digest 提示
	err = TestWebDAVConnection(model.WebdavConfig{
		ServerURL: ts.URL,
		RootPath:  "/dav/digest",
	})
	if err == nil || !strings.Contains(err.Error(), "Digest") {
		t.Errorf("Expected Digest error, got %v", err)
	}

	// 4. 测试 401 普通认证失败
	err = TestWebDAVConnection(model.WebdavConfig{
		ServerURL: ts.URL,
		RootPath:  "/dav/unauth",
	})
	if err == nil || !strings.Contains(err.Error(), "认证失败") {
		t.Errorf("Expected auth failure error, got %v", err)
	}

	// 5. 测试 403 权限拒绝
	err = TestWebDAVConnection(model.WebdavConfig{
		ServerURL: ts.URL,
		RootPath:  "/dav/forbidden",
	})
	if err == nil || !strings.Contains(err.Error(), "403") {
		t.Errorf("Expected 403 error, got %v", err)
	}

	// 6. 测试 404 路径不存在
	err = TestWebDAVConnection(model.WebdavConfig{
		ServerURL: ts.URL,
		RootPath:  "/dav/notfound",
	})
	if err == nil || !strings.Contains(err.Error(), "404") {
		t.Errorf("Expected 404 error, got %v", err)
	}
}

func TestListWebDAVFiles(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.Method != "PROPFIND" {
			w.WriteHeader(http.StatusMethodNotAllowed)
			return
		}
		if r.Header.Get("Depth") != "1" {
			w.WriteHeader(http.StatusBadRequest)
			return
		}

		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)

		switch r.URL.Path {
		case "/dav/media/":
			w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/dav/media/</D:href>
    <D:propstat>
      <D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/%E6%B5%81%E6%B5%AA%E5%9C%B0%E7%90%83.2019.1080p.mkv</D:href>
    <D:propstat>
      <D:prop>
        <D:getcontentlength>104857600</D:getcontentlength>
        <D:getetag>"etag123"</D:getetag>
        <D:getlastmodified>Mon, 12 Jan 2026 10:00:00 GMT</D:getlastmodified>
        <D:resourcetype/>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/small.mp4</D:href>
    <D:propstat>
      <D:prop>
        <D:getcontentlength>1048576</D:getcontentlength>
        <D:resourcetype/>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/.hidden.mp4</D:href>
    <D:propstat>
      <D:prop><D:getcontentlength>104857600</D:getcontentlength></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/image.iso</D:href>
    <D:propstat>
      <D:prop>
        <D:getcontentlength>10485760</D:getcontentlength>
        <D:resourcetype/>
      </D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/@eaDir/</D:href>
    <D:propstat>
      <D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/Season%201/</D:href>
    <D:propstat>
      <D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop>
      <D:status>HTTP/1.1 200 OK</D:status>
    </D:propstat>
  </D:response>
</D:multistatus>`))

		case "/dav/media/Season 1/":
			// 使用小写 d: namespace 验证 XML 容错
			w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<d:multistatus xmlns:d="DAV:">
  <d:response>
    <d:href>/dav/media/Season%201/</d:href>
    <d:propstat>
      <d:prop><d:resourcetype><d:collection/></d:resourcetype></d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
  <d:response>
    <d:href>/dav/media/Season%201/S01E01.mkv</d:href>
    <d:propstat>
      <d:prop>
        <d:getcontentlength>150000000</d:getcontentlength>
        <d:getetag>"etag_s01e01"</d:getetag>
        <d:getlastmodified>Tue, 13 Jan 2026 12:00:00 GMT</d:getlastmodified>
        <d:resourcetype/>
      </d:prop>
      <d:status>HTTP/1.1 200 OK</d:status>
    </d:propstat>
  </d:response>
</d:multistatus>`))
		default:
			w.WriteHeader(http.StatusNotFound)
		}
	}))
	defer ts.Close()

	files, truncated, err := ListWebDAVFiles(context.Background(), ts.URL, "/dav/media", "", "", 50*1024*1024)
	if err != nil {
		t.Fatalf("ListWebDAVFiles failed: %v", err)
	}
	if truncated {
		t.Errorf("Expected truncated = false, got true")
	}

	if len(files) != 3 {
		t.Fatalf("Expected 3 files, got %d: %+v", len(files), files)
	}

	// 验证包含的文件
	expectedPaths := map[string]bool{
		"流浪地球.2019.1080p.mkv": false,
		"image.iso":           false,
		"Season 1/S01E01.mkv": false,
	}

	for _, f := range files {
		if _, ok := expectedPaths[f.RelPath]; ok {
			expectedPaths[f.RelPath] = true
		} else {
			t.Errorf("Unexpected file in results: %s", f.RelPath)
		}
	}

	for p, found := range expectedPaths {
		if !found {
			t.Errorf("Expected file %s not found in results", p)
		}
	}
}

func TestListWebDAVFiles_FollowsHrefWithColonAndSpace(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		switch {
		case r.URL.Path == "/dav/media/" || r.URL.Path == "/dav/media":
			w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/dav/media/</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/FATE%E5%89%A7%E5%9C%BA%E7%89%88/%E5%91%BD%E8%BF%90%E4%B9%8B%E5%A4%9C%20%E5%A4%A9%E4%B9%8B%E6%9D%AFII%EF%BC%9A%E8%BF%B7%E5%A4%B1%E4%B9%8B%E8%9D%B6/</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
</D:multistatus>`))
		default:
			w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>` + r.URL.Path + `</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
  <D:response>
    <D:href>` + strings.TrimSuffix(r.URL.Path, "/") + `/movie.mkv</D:href>
    <D:propstat><D:prop><D:getcontentlength>104857600</D:getcontentlength><D:resourcetype/></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
</D:multistatus>`))
		}
	}))
	defer ts.Close()

	files, _, err := ListWebDAVFiles(context.Background(), ts.URL, "/dav/media", "", "", 1)
	if err != nil {
		t.Fatalf("ListWebDAVFiles: %v", err)
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file under colon/space dir, got %d %+v", len(files), files)
	}
}

func TestListWebDAVFiles_SkipsNested404(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		if r.URL.Path == "/dav/media/broken/" {
			w.WriteHeader(http.StatusNotFound)
			return
		}
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		if r.URL.Path == "/dav/media/" {
			w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/dav/media/</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/ok.mkv</D:href>
    <D:propstat><D:prop><D:getcontentlength>104857600</D:getcontentlength><D:resourcetype/></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/broken/</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
</D:multistatus>`))
			return
		}
		w.Write([]byte(`<?xml version="1.0"?><D:multistatus xmlns:D="DAV:"></D:multistatus>`))
	}))
	defer ts.Close()

	files, _, err := ListWebDAVFiles(context.Background(), ts.URL, "/dav/media", "", "", 1)
	if err != nil {
		t.Fatalf("nested 404 should be skipped, got %v", err)
	}
	if len(files) != 1 || files[0].RelPath != "ok.mkv" {
		t.Fatalf("expected only ok.mkv, got %+v", files)
	}
}

func TestListWebDAVFiles_ZeroMinBytesKeepsSmallFiles(t *testing.T) {
	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/dav/media/</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/small.mp4</D:href>
    <D:propstat><D:prop><D:getcontentlength>1048576</D:getcontentlength><D:resourcetype/></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
</D:multistatus>`))
	}))
	defer ts.Close()

	files, truncated, err := ListWebDAVFiles(context.Background(), ts.URL, "/dav/media", "", "", 0)
	if err != nil {
		t.Fatalf("ListWebDAVFiles failed: %v", err)
	}
	if truncated {
		t.Fatalf("expected truncated=false")
	}
	if len(files) != 1 || files[0].RelPath != "small.mp4" {
		t.Fatalf("minFileBytes=0 should keep small files, got %+v", files)
	}
}

func TestListWebDAVFiles_Truncated(t *testing.T) {
	prev := utils.WebDAVListFileLimit
	utils.WebDAVListFileLimit = 1
	t.Cleanup(func() { utils.WebDAVListFileLimit = prev })

	ts := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		w.WriteHeader(http.StatusMultiStatus)
		w.Write([]byte(`<?xml version="1.0" encoding="utf-8" ?>
<D:multistatus xmlns:D="DAV:">
  <D:response>
    <D:href>/dav/media/</D:href>
    <D:propstat><D:prop><D:resourcetype><D:collection/></D:resourcetype></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/a.mkv</D:href>
    <D:propstat><D:prop><D:getcontentlength>104857600</D:getcontentlength><D:resourcetype/></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
  <D:response>
    <D:href>/dav/media/b.mkv</D:href>
    <D:propstat><D:prop><D:getcontentlength>104857600</D:getcontentlength><D:resourcetype/></D:prop><D:status>HTTP/1.1 200 OK</D:status></D:propstat>
  </D:response>
</D:multistatus>`))
	}))
	defer ts.Close()

	files, truncated, err := ListWebDAVFiles(context.Background(), ts.URL, "/dav/media", "", "", 1)
	if err != nil {
		t.Fatalf("ListWebDAVFiles failed: %v", err)
	}
	if !truncated {
		t.Fatalf("expected truncated=true")
	}
	if len(files) != 1 {
		t.Fatalf("expected 1 file after cap, got %d", len(files))
	}
}
