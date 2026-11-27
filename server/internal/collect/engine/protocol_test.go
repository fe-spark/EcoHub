package engine

import (
	"net/http"
	"net/http/httptest"
	"net/url"
	"testing"

	"golang.org/x/text/encoding/simplifiedchinese"
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

func TestResolveCollector(t *testing.T) {
	if _, ok := ResolveCollector("").(*JsonCollect); !ok {
		t.Fatal("empty format should resolve to JsonCollect")
	}
	if _, ok := ResolveCollector(model.SourceFormatJSON).(*JsonCollect); !ok {
		t.Fatal("json format should resolve to JsonCollect")
	}
	if _, ok := ResolveCollector(model.SourceFormatXML).(*XmlCollect); !ok {
		t.Fatal("xml format should resolve to XmlCollect")
	}
}

func TestJsonCollect_ProbePage1AndGetPageCount(t *testing.T) {
	var gotAc, gotPg string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAc = r.URL.Query().Get("ac")
		gotPg = r.URL.Query().Get("pg")
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"pagecount":7,"total":140,"list":[{"vod_id":10,"vod_name":"测试片","vod_play_from":"m3u8","vod_play_url":"正片$https://example.com/10.m3u8"}]}`))
	}))
	defer server.Close()

	jc := &JsonCollect{}
	pageCount, items, err := jc.ProbePage1(utils.RequestInfo{Uri: server.URL, Params: url.Values{}})
	if err != nil {
		t.Fatalf("ProbePage1 failed: %v", err)
	}
	if pageCount != 7 || gotAc != "detail" || gotPg != "1" {
		t.Fatalf("got pageCount=%d ac=%s pg=%s", pageCount, gotAc, gotPg)
	}
	if len(items) != 1 || items[0].Id != 10 || items[0].Name != "测试片" {
		t.Fatalf("unexpected pg1 items: %+v", items)
	}
}

func TestXmlCollect_ProbePage1AndGetDetail(t *testing.T) {
	var gotAc string
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		gotAc = r.URL.Query().Get("ac")
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(sampleMacCMSXML))
	}))
	defer server.Close()

	xc := &XmlCollect{}
	pageCount, items, err := xc.ProbePage1(utils.RequestInfo{Uri: server.URL})
	if err != nil {
		t.Fatalf("ProbePage1 failed: %v", err)
	}
	if pageCount != 42 || gotAc != "videolist" {
		t.Fatalf("pageCount=%d gotAc=%s", pageCount, gotAc)
	}
	if len(items) != 1 || items[0].Id != 1001 || len(items[0].PlayFrom) != 2 || len(items[0].PlayList) != 2 {
		t.Fatalf("unexpected xml pg1 items: %+v", items)
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

	details, err := (&XmlCollect{}).GetFilmDetail(utils.RequestInfo{Uri: server.URL})
	if err != nil {
		t.Fatalf("GetFilmDetail failed: %v", err)
	}
	if len(details) != 1 || len(details[0].PlayFrom) != 2 || details[0].PlayFrom[0] != "m3u8" || details[0].PlayFrom[1] != "yun" {
		t.Fatalf("unexpected details: %+v", details)
	}
}

func TestXmlCollect_GBKEncoding(t *testing.T) {
	gbkXMLText := `<?xml version="1.0" encoding="gb2312"?>
<rss version="5.1">
    <class><ty id="1">电影</ty></class>
    <list page="1" pagecount="5" pagesize="20" recordcount="100">
        <video>
            <id>5001</id>
            <name>测试GBK中文影片</name>
            <dl><dd flag="m3u8"><![CDATA[第01集$https://example.com/gbk.m3u8]]></dd></dl>
        </video>
    </list>
</rss>`
	gbkBytes, err := simplifiedchinese.GBK.NewEncoder().Bytes([]byte(gbkXMLText))
	if err != nil {
		t.Fatalf("encode GBK failed: %v", err)
	}
	server := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=gb2312")
		_, _ = w.Write(gbkBytes)
	}))
	defer server.Close()

	xc := &XmlCollect{}
	tree, _, err := xc.GetCategoryTree(utils.RequestInfo{Uri: server.URL})
	if err != nil || tree == nil || len(tree.Children) == 0 {
		t.Fatalf("GetCategoryTree failed: tree=%v err=%v", tree, err)
	}
	pageCount, details, err := xc.ProbePage1(utils.RequestInfo{Uri: server.URL})
	if err != nil || pageCount != 5 || len(details) != 1 || details[0].Name != "测试GBK中文影片" {
		t.Fatalf("ProbePage1 GBK failed: pageCount=%d details=%+v err=%v", pageCount, details, err)
	}
}

func TestGetPageCount_RejectsAbnormalPageCount(t *testing.T) {
	jsonSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/json")
		_, _ = w.Write([]byte(`{"code":1,"pagecount":2147483647,"total":999999999}`))
	}))
	defer jsonSrv.Close()

	if _, err := (&JsonCollect{}).GetPageCount(utils.RequestInfo{Uri: jsonSrv.URL, Params: url.Values{}}); err == nil {
		t.Fatal("JsonCollect.GetPageCount 应拒绝超大 pagecount")
	}

	xmlSrv := httptest.NewServer(http.HandlerFunc(func(w http.ResponseWriter, r *http.Request) {
		w.Header().Set("Content-Type", "application/xml; charset=utf-8")
		_, _ = w.Write([]byte(`<?xml version="1.0" encoding="utf-8"?><rss version="5.1"><list page="1" pagecount="9999999" pagesize="20" recordcount="100"></list></rss>`))
	}))
	defer xmlSrv.Close()

	if _, err := (&XmlCollect{}).GetPageCount(utils.RequestInfo{Uri: xmlSrv.URL}); err == nil {
		t.Fatal("XmlCollect.GetPageCount 应拒绝超大 pagecount")
	}
}
