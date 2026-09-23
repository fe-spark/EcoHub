package spider

import (
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/model"
	"server/internal/utils"
)

const sampleMacCMSXML = `<?xml version="1.0" encoding="utf-8"?>
<rss version="5.1">
    <class>
        <ty id="1">电影</ty>
        <ty id="2">电视剧</ty>
        <ty id="5" pid="1">动作片</ty>
        <ty id="6" pid="1">喜剧片</ty>
    </class>
    <list page="1" pagecount="42" pagesize="20" recordcount="840">
        <video>
            <last>2026-03-20 12:00:00</last>
            <id>1001</id>
            <tid>5</tid>
            <name><![CDATA[流浪地球3]]></name>
            <type>动作片</type>
            <pic>https://example.com/pic.jpg</pic>
            <lang>国语</lang>
            <area>中国大陆</area>
            <year>2026</year>
            <state>0</state>
            <note><![CDATA[4K超清]]></note>
            <actor><![CDATA[吴京, 刘德华]]></actor>
            <director><![CDATA[郭帆]]></director>
            <des><![CDATA[太阳即将毁灭，人类开启新征程。]]></des>
            <dl>
                <dd flag="m3u8"><![CDATA[正片$https://example.com/1.m3u8#花絮$https://example.com/2.m3u8]]></dd>
                <dd flag="yun"><![CDATA[备用正片$https://example.com/backup.m3u8]]></dd>
            </dl>
        </video>
    </list>
</rss>`

func TestXmlCollect_GetPageCount(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(sampleMacCMSXML))
	}))
	defer server.Close()

	xc := &XmlCollect{}
	r := utils.RequestInfo{Uri: server.URL}
	pageCount, err := xc.GetPageCount(r)
	if err != nil {
		t.Fatalf("GetPageCount failed: %v", err)
	}
	if pageCount != 42 {
		t.Fatalf("expected pagecount 42, got %d", pageCount)
	}
}

func TestXmlCollect_GetCategoryTree(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(sampleMacCMSXML))
	}))
	defer server.Close()

	xc := &XmlCollect{}
	r := utils.RequestInfo{Uri: server.URL}
	tree, err := xc.GetCategoryTree(r)
	if err != nil {
		t.Fatalf("GetCategoryTree failed: %v", err)
	}
	if tree == nil {
		t.Fatal("expected non-nil category tree")
	}

	foundMovie := false
	for _, child := range tree.Children {
		if child.Id == 1 && child.Name == "电影" {
			foundMovie = true
			if len(child.Children) != 2 {
				t.Fatalf("expected 2 children under 电影, got %d", len(child.Children))
			}
		}
	}
	if !foundMovie {
		t.Fatal("expected to find root category 电影 (id=1)")
	}
}

func TestXmlCollect_GetFilmDetail(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(sampleMacCMSXML))
	}))
	defer server.Close()

	xc := &XmlCollect{}
	r := utils.RequestInfo{Uri: server.URL}
	details, err := xc.GetFilmDetail(r)
	if err != nil {
		t.Fatalf("GetFilmDetail failed: %v", err)
	}
	if len(details) != 1 {
		t.Fatalf("expected 1 movie detail, got %d", len(details))
	}

	movie := details[0]
	if movie.Id != 1001 {
		t.Errorf("expected movie id 1001, got %d", movie.Id)
	}
	if movie.Name != "流浪地球3" {
		t.Errorf("expected movie name 流浪地球3, got %s", movie.Name)
	}
	if movie.CName != "动作片" {
		t.Errorf("expected movie category 动作片, got %s", movie.CName)
	}
	if movie.Picture != "https://example.com/pic.jpg" {
		t.Errorf("expected movie pic, got %s", movie.Picture)
	}
	if movie.Actor != "吴京, 刘德华" {
		t.Errorf("expected actor, got %s", movie.Actor)
	}
	if len(movie.PlayFrom) != 2 {
		t.Fatalf("expected 2 play sources, got %d (%v)", len(movie.PlayFrom), movie.PlayFrom)
	}
	if movie.PlayFrom[0] != "m3u8" || movie.PlayFrom[1] != "yun" {
		t.Errorf("unexpected play sources: %v", movie.PlayFrom)
	}
	if len(movie.PlayList) != 2 {
		t.Fatalf("expected 2 play lists, got %d", len(movie.PlayList))
	}
	if len(movie.PlayList[0]) != 2 {
		t.Fatalf("expected 2 episodes in first play list, got %d", len(movie.PlayList[0]))
	}
	if movie.PlayList[0][0].Episode != "正片" || movie.PlayList[0][0].Link != "https://example.com/1.m3u8" {
		t.Errorf("unexpected episode 0: %+v", movie.PlayList[0][0])
	}
}

func TestResolveCollector(t *testing.T) {
	cJson := ResolveCollector("")
	if _, ok := cJson.(*JsonCollect); !ok {
		t.Fatalf("expected *JsonCollect for empty format, got %T", cJson)
	}
	cJson2 := ResolveCollector("json")
	if _, ok := cJson2.(*JsonCollect); !ok {
		t.Fatalf("expected *JsonCollect for json format, got %T", cJson2)
	}
	cXml := ResolveCollector("xml")
	if _, ok := cXml.(*XmlCollect); !ok {
		t.Fatalf("expected *XmlCollect for xml format, got %T", cXml)
	}
}

func TestCollectApiTestWithTimeout_XML(t *testing.T) {
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(sampleMacCMSXML))
	}))
	defer server.Close()

	// 1. XML 站点访问 XML 接口，应成功
	sXml := model.FilmSource{
		Uri:    server.URL,
		Format: model.SourceFormatXML,
	}
	if err := CollectApiTest(sXml); err != nil {
		t.Fatalf("expected xml test to pass, got err: %v", err)
	}

	// 2. JSON 站点访问 XML 接口，应报错格式不一致
	sJson := model.FilmSource{
		Uri:    server.URL,
		Format: model.SourceFormatJSON,
	}
	err := CollectApiTest(sJson)
	if err == nil {
		t.Fatal("expected json test against xml endpoint to fail")
	}
	if !strings.Contains(err.Error(), "接口返回为 XML 格式，与所选的 JSON 格式不一致") {
		t.Fatalf("unexpected error message: %v", err)
	}

	// 3. XML 站点访问 JSON 接口，应报错格式不一致
	jsonServer := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json; charset=utf-8")
		_, _ = w.Write([]byte(`{"code":1,"msg":"数据列表","page":1,"pagecount":1,"limit":20,"total":0,"list":[]}`))
	}))
	defer jsonServer.Close()

	sXmlAgainstJson := model.FilmSource{
		Uri:    jsonServer.URL,
		Format: model.SourceFormatXML,
	}
	err = CollectApiTest(sXmlAgainstJson)
	if err == nil {
		t.Fatal("expected xml test against json endpoint to fail")
	}
	if !strings.Contains(err.Error(), "接口返回为 JSON 格式，与所选的 XML 格式不一致") {
		t.Fatalf("unexpected error message: %v", err)
	}
}

func TestXmlCollect_EmptyDDAndMultipleDL(t *testing.T) {
	xmlWithEmptyDD := `<?xml version="1.0" encoding="utf-8"?>
<rss version="5.1">
    <list page="1" pagecount="1" pagesize="20" recordcount="1">
        <video>
            <id>2001</id>
            <name>测试电影</name>
            <dl>
                <dd flag="empty"></dd>
                <dd flag="m3u8"><![CDATA[01$https://example.com/1.m3u8]]></dd>
            </dl>
            <dl>
                <dd flag="yun"><![CDATA[01$https://example.com/backup.m3u8]]></dd>
            </dl>
        </video>
    </list>
</rss>`

	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(xmlWithEmptyDD))
	}))
	defer server.Close()

	xc := &XmlCollect{}
	r := utils.RequestInfo{Uri: server.URL}
	details, err := xc.GetFilmDetail(r)
	if err != nil {
		t.Fatalf("GetFilmDetail failed: %v", err)
	}
	if len(details) != 1 {
		t.Fatalf("expected 1 detail, got %d", len(details))
	}
	movie := details[0]
	// empty dd 必须被跳过，PlayFrom 与 PlayList 长度必须完全一致为 2
	if len(movie.PlayFrom) != 2 {
		t.Fatalf("expected 2 playFrom sources (empty skipped), got %d (%v)", len(movie.PlayFrom), movie.PlayFrom)
	}
	if len(movie.PlayList) != 2 {
		t.Fatalf("expected 2 playList entries, got %d", len(movie.PlayList))
	}
	if movie.PlayFrom[0] != "m3u8" || movie.PlayFrom[1] != "yun" {
		t.Errorf("expected [m3u8, yun], got %v", movie.PlayFrom)
	}
}

