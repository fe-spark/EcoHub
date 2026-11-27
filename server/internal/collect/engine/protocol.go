// Package engine 独立采集核心引擎：封装多协议解析、页 1 复用探测、AIMD 自适应并发节流与有界全局写池。
package engine

import (
	"bytes"
	"encoding/json"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"

	"golang.org/x/net/html/charset"
	"server/internal/film/converter"
	"server/internal/model"
	"server/internal/utils"
)

// MaxCollectPageCount 采集站允许的合理最大分页数，防止恶意超大 pagecount 触发 OOM 与无限抓取。
const MaxCollectPageCount = 50000

// Collector 采集引擎统一接口，支持 JSON 与 XML 等多协议接入。
type Collector interface {
	GetCategoryTree(r utils.RequestInfo) (*model.CategoryTree, int, error)
	ProbePage1(r utils.RequestInfo) (int, []model.MovieDetail, error)
	GetPageCount(r utils.RequestInfo) (int, error)
	GetFilmDetail(r utils.RequestInfo) ([]model.MovieDetail, error)
}

var (
	jsonCollector = &JsonCollect{}
	xmlCollector  = &XmlCollect{}
)

// ResolveCollector 根据源站配置的格式返回对应采集器实例，默认返回 JSON 采集器。
func ResolveCollector(format string) Collector {
	if format == model.SourceFormatXML {
		return xmlCollector
	}
	return jsonCollector
}

// NormalizeSourceDefaults 补全采集站默认窗口与格式。
func NormalizeSourceDefaults(source *model.FilmSource) {
	NormalizeCd(source)
	if source.Format == "" {
		source.Format = model.SourceFormatJSON
	}
}

// NormalizeCd 补全采集站默认采集小时数（24 小时）。
func NormalizeCd(source *model.FilmSource) {
	if source.Cd <= 0 {
		source.Cd = 24
	}
}

func ensureParams(r *utils.RequestInfo) {
	if r.Params == nil {
		r.Params = url.Values{}
	}
}

func emptyResponseErr(r utils.RequestInfo) error {
	if r.Err != "" {
		return errors.New(r.Err)
	}
	return errors.New("response is empty")
}

// ------------------------------------------------- JSON Collect -------------------------------------------------

// JsonCollect 处理返回值为 JSON 格式的采集数据。
type JsonCollect struct{}

// GetCategoryTree 获取分类树形数据。
func (jc *JsonCollect) GetCategoryTree(r utils.RequestInfo) (*model.CategoryTree, int, error) {
	ensureParams(&r)
	r.Params.Set("ac", "list")
	r.Params.Set("pg", "1")
	utils.ApiGet(&r)
	if len(r.Resp) == 0 {
		err := emptyResponseErr(r)
		log.Printf("filmListPage 数据获取异常: %v", err)
		return nil, 0, fmt.Errorf("filmListPage 数据获取异常: %w", err)
	}
	var filmListPage model.FilmListPage
	if err := json.Unmarshal(r.Resp, &filmListPage); err != nil {
		return nil, 0, fmt.Errorf("filmListPage JSON unmarshal error: %w", err)
	}
	if len(filmListPage.Class) == 0 {
		return &model.CategoryTree{}, filmListPage.PageCount, nil
	}
	return converter.GenCategoryTreeWithParentHints(filmListPage.Class, nil), filmListPage.PageCount, nil
}

// ProbePage1 一次请求同时探测总页数并解析第 1 页明细，避免采集阶段重复抓取第 1 页。
func (jc *JsonCollect) ProbePage1(r utils.RequestInfo) (int, []model.MovieDetail, error) {
	ensureParams(&r)
	if len(r.Params.Get("ac")) == 0 {
		r.Params.Set("ac", "detail")
	}
	r.Params.Set("pg", "1")
	ac := r.Params.Get("ac")
	h := r.Params.Get("h")
	utils.ApiGet(&r)
	if len(r.Resp) == 0 {
		return 0, nil, emptyResponseErr(r)
	}
	var detailPage model.FilmDetailLPage
	if err := json.Unmarshal(r.Resp, &detailPage); err != nil {
		return 0, nil, err
	}
	if detailPage.PageCount < 0 || detailPage.PageCount > MaxCollectPageCount {
		return 0, nil, fmt.Errorf("采集站返回异常总页数: %d", detailPage.PageCount)
	}
	log.Printf("[Spider] JSON 源探页完成 ac=%s h=%s pagecount=%d total=%d", ac, h, detailPage.PageCount, detailPage.Total)
	return detailPage.PageCount, converter.ConvertFilmDetails(detailPage.List), nil
}

// GetPageCount 获取分页总页数（ac=detail 与抓页保持一致）。
func (jc *JsonCollect) GetPageCount(r utils.RequestInfo) (int, error) {
	count, _, err := jc.ProbePage1(r)
	return count, err
}

// GetFilmDetail 获取并解析单页影片明细。
func (jc *JsonCollect) GetFilmDetail(r utils.RequestInfo) ([]model.MovieDetail, error) {
	ensureParams(&r)
	r.Params.Set("ac", "detail")
	utils.ApiGet(&r)
	if len(r.Resp) == 0 {
		return nil, emptyResponseErr(r)
	}
	var detailPage model.FilmDetailLPage
	if err := json.Unmarshal(r.Resp, &detailPage); err != nil {
		return nil, err
	}
	return converter.ConvertFilmDetails(detailPage.List), nil
}

// ------------------------------------------------- XML Collect -------------------------------------------------

// XMLRSS MacCMS 标准 XML 协议根节点结构体。
type XMLRSS struct {
	XMLName xml.Name     `xml:"rss"`
	Version string       `xml:"version,attr"`
	Class   XMLClass     `xml:"class"`
	List    XMLVideoList `xml:"list"`
}

// XMLClass 分类集合节点。
type XMLClass struct {
	Types []XMLType `xml:"ty"`
}

// XMLType 单个分类项定义。
type XMLType struct {
	ID   int64  `xml:"id,attr"`
	Pid  int64  `xml:"pid,attr"`
	Name string `xml:",chardata"`
}

// XMLVideoList 分页与影片列表节点。
type XMLVideoList struct {
	Page        int        `xml:"page,attr"`
	PageCount   int        `xml:"pagecount,attr"`
	PageSize    int        `xml:"pagesize,attr"`
	RecordCount int        `xml:"recordcount,attr"`
	Videos      []XMLVideo `xml:"video"`
}

// XMLVideo 单部影片基础与明细字段。
type XMLVideo struct {
	ID       int64   `xml:"id"`
	Tid      int64   `xml:"tid"`
	Name     string  `xml:"name"`
	Type     string  `xml:"type"`
	Pic      string  `xml:"pic"`
	Lang     string  `xml:"lang"`
	Area     string  `xml:"area"`
	Year     string  `xml:"year"`
	State    string  `xml:"state"`
	Note     string  `xml:"note"`
	Actor    string  `xml:"actor"`
	Director string  `xml:"director"`
	Des      string  `xml:"des"`
	Last     string  `xml:"last"`
	DLs      []XMLDL `xml:"dl"`
}

// XMLDL 播放来源集合。
type XMLDL struct {
	DDs []XMLDD `xml:"dd"`
}

// XMLDD 单个播放来源与剧集列表。
type XMLDD struct {
	Flag string `xml:"flag,attr"`
	From string `xml:"from,attr"`
	URL  string `xml:",chardata"`
}

// UnmarshalXMLWithCharset 使用支持字符集自动探测与转换的解码器解析 XML（支持 GBK/GB2312/UTF-8）。
func UnmarshalXMLWithCharset(data []byte, v any) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = charset.NewReaderLabel
	return decoder.Decode(v)
}

// XmlCollect 处理返回值为 XML 格式的 MacCMS 采集数据。
type XmlCollect struct{}

// GetCategoryTree 获取 XML 源分类树形数据。
func (xc *XmlCollect) GetCategoryTree(r utils.RequestInfo) (*model.CategoryTree, int, error) {
	ensureParams(&r)
	if r.Params.Get("ac") == "" {
		r.Params.Set("ac", "list")
	}
	if r.Params.Get("pg") == "" {
		r.Params.Set("pg", "1")
	}
	utils.ApiGet(&r)
	if len(r.Resp) == 0 {
		err := emptyResponseErr(r)
		log.Printf("[Spider-XML] 分类数据获取异常: %v", err)
		return nil, 0, fmt.Errorf("XML 分类数据获取异常: %w", err)
	}
	var rss XMLRSS
	if err := UnmarshalXMLWithCharset(r.Resp, &rss); err != nil {
		return nil, 0, fmt.Errorf("XML unmarshal error: %w", err)
	}
	if len(rss.Class.Types) == 0 {
		return &model.CategoryTree{}, rss.List.PageCount, nil
	}
	classes := make([]model.FilmClass, 0, len(rss.Class.Types))
	for _, t := range rss.Class.Types {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}
		classes = append(classes, model.FilmClass{ID: t.ID, Pid: t.Pid, Name: name})
	}
	return converter.GenCategoryTreeWithParentHints(classes, nil), rss.List.PageCount, nil
}

// ProbePage1 一次请求探测 XML 总页数并直接解析第 1 页影片明细。
func (xc *XmlCollect) ProbePage1(r utils.RequestInfo) (int, []model.MovieDetail, error) {
	ensureParams(&r)
	if len(r.Params.Get("ac")) == 0 {
		r.Params.Set("ac", "videolist")
	}
	r.Params.Set("pg", "1")
	utils.ApiGet(&r)
	if len(r.Resp) == 0 {
		return 0, nil, emptyResponseErr(r)
	}
	var rss XMLRSS
	if err := UnmarshalXMLWithCharset(r.Resp, &rss); err != nil {
		return 0, nil, fmt.Errorf("XML 反序列化失败: %w", err)
	}
	if rss.List.PageCount < 0 || rss.List.PageCount > MaxCollectPageCount {
		return 0, nil, fmt.Errorf("采集站返回异常总页数: %d", rss.List.PageCount)
	}
	return rss.List.PageCount, convertXMLVideosToMovieDetails(rss.List.Videos), nil
}

// GetPageCount 探测 XML 源分页总页数。
func (xc *XmlCollect) GetPageCount(r utils.RequestInfo) (int, error) {
	count, _, err := xc.ProbePage1(r)
	return count, err
}

// GetFilmDetail 获取 XML 源单页影片明细。
func (xc *XmlCollect) GetFilmDetail(r utils.RequestInfo) ([]model.MovieDetail, error) {
	ensureParams(&r)
	if len(r.Params.Get("ac")) == 0 {
		r.Params.Set("ac", "videolist")
	}
	utils.ApiGet(&r)
	if len(r.Resp) == 0 {
		return nil, emptyResponseErr(r)
	}
	var rss XMLRSS
	if err := UnmarshalXMLWithCharset(r.Resp, &rss); err != nil {
		return nil, fmt.Errorf("XML 反序列化失败: %w", err)
	}
	return convertXMLVideosToMovieDetails(rss.List.Videos), nil
}

// convertXMLVideosToMovieDetails 单遍解析 XMLVideo 播放列表并转换为 MovieDetail，避免重复拼接和拆串。
func convertXMLVideosToMovieDetails(videos []XMLVideo) []model.MovieDetail {
	details := make([]model.MovieDetail, 0, len(videos))
	for _, v := range videos {
		name := strings.TrimSpace(v.Name)
		if name == "" {
			continue
		}
		var playFroms []string
		var playList [][]model.MovieUrlInfo
		for _, dl := range v.DLs {
			for _, dd := range dl.DDs {
				rawURL := strings.TrimSpace(dd.URL)
				if rawURL == "" {
					continue
				}
				episodes := converter.ConvertPlayUrl(rawURL)
				if len(episodes) == 0 {
					continue
				}
				from := strings.TrimSpace(dd.Flag)
				if from == "" {
					from = strings.TrimSpace(dd.From)
				}
				if from == "" {
					from = "default"
				}
				playFroms = append(playFroms, from)
				playList = append(playList, episodes)
			}
		}
		if len(playList) == 0 {
			continue
		}
		md := converter.ConvertFilmDetail(model.FilmDetail{
			VodID:       v.ID,
			TypeID:      v.Tid,
			VodName:     name,
			TypeName:    strings.TrimSpace(v.Type),
			VodPic:      strings.TrimSpace(v.Pic),
			VodLang:     strings.TrimSpace(v.Lang),
			VodArea:     strings.TrimSpace(v.Area),
			VodYear:     strings.TrimSpace(v.Year),
			VodState:    strings.TrimSpace(v.State),
			VodRemarks:  strings.TrimSpace(v.Note),
			VodActor:    strings.TrimSpace(v.Actor),
			VodDirector: strings.TrimSpace(v.Director),
			VodContent:  strings.TrimSpace(v.Des),
			VodTime:     strings.TrimSpace(v.Last),
		})
		md.PlayFrom = playFroms
		md.PlayList = playList
		if !converter.HasPlayableLinks(md) {
			continue
		}
		details = append(details, md)
	}
	return details
}
