package spider

import (
	"bytes"
	"encoding/xml"
	"errors"
	"fmt"
	"log"
	"net/url"
	"strings"

	"golang.org/x/net/html/charset"
	"server/internal/model"
	"server/internal/spider/converter"
	"server/internal/utils"
)

func ensureParams(r *utils.RequestInfo) {
	if r.Params == nil {
		r.Params = url.Values{}
	}
}

// ------------------------------------------------- XML Collect -------------------------------------------------

// XMLRSS MacCMS 标准 XML 协议根节点结构体
type XMLRSS struct {
	XMLName xml.Name     `xml:"rss"`
	Version string       `xml:"version,attr"`
	Class   XMLClass     `xml:"class"`
	List    XMLVideoList `xml:"list"`
}

// XMLClass 分类集合节点
type XMLClass struct {
	Types []XMLType `xml:"ty"`
}

// XMLType 单个分类项定义
type XMLType struct {
	ID   int64  `xml:"id,attr"`
	Pid  int64  `xml:"pid,attr"`
	Name string `xml:",chardata"`
}

// XMLVideoList 分页与影片列表节点
type XMLVideoList struct {
	Page        int        `xml:"page,attr"`
	PageCount   int        `xml:"pagecount,attr"`
	PageSize    int        `xml:"pagesize,attr"`
	RecordCount int        `xml:"recordcount,attr"`
	Videos      []XMLVideo `xml:"video"`
}

// XMLVideo 单部影片基础与明细字段
type XMLVideo struct {
	ID       int64  `xml:"id"`
	Tid      int64  `xml:"tid"`
	Name     string `xml:"name"`
	Type     string `xml:"type"`
	Pic      string `xml:"pic"`
	Lang     string `xml:"lang"`
	Area     string `xml:"area"`
	Year     string `xml:"year"`
	State    string `xml:"state"`
	Note     string `xml:"note"`
	Actor    string `xml:"actor"`
	Director string `xml:"director"`
	Des      string `xml:"des"`
	Last     string   `xml:"last"`
	DLs      []XMLDL  `xml:"dl"`
}

// XMLDL 播放来源集合
type XMLDL struct {
	DDs []XMLDD `xml:"dd"`
}

// XMLDD 单个播放来源与剧集列表
type XMLDD struct {
	Flag string `xml:"flag,attr"`
	From string `xml:"from,attr"`
	URL  string `xml:",chardata"`
}

// unmarshalXMLWithCharset 使用支持字符集自动探测与转换的解码器解析 XML（支持 GBK/GB2312/UTF-8 等）
func unmarshalXMLWithCharset(data []byte, v any) error {
	decoder := xml.NewDecoder(bytes.NewReader(data))
	decoder.CharsetReader = charset.NewReaderLabel
	return decoder.Decode(v)
}

// XmlCollect 处理返回值为 XML 格式的苹果 CMS (MacCMS) 采集数据
type XmlCollect struct{}

// GetCategoryTree 获取 XML 源分类树形数据
func (xc *XmlCollect) GetCategoryTree(r utils.RequestInfo) (*model.CategoryTree, error) {
	ensureParams(&r)
	if r.Params.Get("ac") == "" {
		r.Params.Set("ac", "list")
	}
	if r.Params.Get("pg") == "" {
		r.Params.Set("pg", "1")
	}
	utils.ApiGet(&r)
	if len(r.Resp) == 0 {
		errMsg := r.Err
		if errMsg == "" {
			errMsg = "response is empty"
		}
		log.Printf("[Spider-XML] 分类数据获取异常: %s", errMsg)
		return nil, fmt.Errorf("XML 分类数据获取异常: %s", errMsg)
	}

	var rss XMLRSS
	if err := unmarshalXMLWithCharset(r.Resp, &rss); err != nil {
		return nil, fmt.Errorf("XML unmarshal error: %w", err)
	}

	if len(rss.Class.Types) == 0 {
		return &model.CategoryTree{}, nil
	}

	classes := make([]model.FilmClass, 0, len(rss.Class.Types))
	for _, t := range rss.Class.Types {
		name := strings.TrimSpace(t.Name)
		if name == "" {
			continue
		}
		classes = append(classes, model.FilmClass{
			ID:   t.ID,
			Pid:  t.Pid,
			Name: name,
		})
	}

	tree := converter.GenCategoryTreeWithParentHints(classes, nil)
	return tree, nil
}

// GetPageCount 探测 XML 源分页总页数
func (xc *XmlCollect) GetPageCount(r utils.RequestInfo) (int, error) {
	ensureParams(&r)
	if len(r.Params.Get("ac")) == 0 {
		r.Params.Set("ac", "videolist")
	}
	r.Params.Set("pg", "1")
	utils.ApiGet(&r)

	if len(r.Resp) == 0 {
		errMsg := r.Err
		if errMsg == "" {
			errMsg = "response is empty"
		}
		return 0, errors.New(errMsg)
	}

	var rss XMLRSS
	if err := unmarshalXMLWithCharset(r.Resp, &rss); err != nil {
		return 0, fmt.Errorf("XML 反序列化失败: %w", err)
	}
	return rss.List.PageCount, nil
}

// GetFilmDetail 获取 XML 源单页影片明细
func (xc *XmlCollect) GetFilmDetail(r utils.RequestInfo) (list []model.MovieDetail, err error) {
	defer func() {
		if e := recover(); e != nil {
			log.Printf("[Spider-XML] GetFilmDetail 异常恢复: %v", e)
			err = fmt.Errorf("XML GetFilmDetail panic recovered: %v", e)
		}
	}()

	ensureParams(&r)
	if len(r.Params.Get("ac")) == 0 {
		r.Params.Set("ac", "videolist")
	}
	utils.ApiGet(&r)

	if len(r.Resp) == 0 {
		errMsg := r.Err
		if errMsg == "" {
			errMsg = "response is empty"
		}
		return nil, errors.New(errMsg)
	}

	var rss XMLRSS
	if err = unmarshalXMLWithCharset(r.Resp, &rss); err != nil {
		return nil, fmt.Errorf("XML 反序列化失败: %w", err)
	}

	filmDetails := convertXMLVideosToFilmDetails(rss.List.Videos)
	list = converter.ConvertFilmDetails(filmDetails)
	return list, nil
}

// convertXMLVideosToFilmDetails 将 XMLVideo 列表转化为中间模型 FilmDetail
func convertXMLVideosToFilmDetails(videos []XMLVideo) []model.FilmDetail {
	details := make([]model.FilmDetail, 0, len(videos))
	for _, v := range videos {
		name := strings.TrimSpace(v.Name)
		if name == "" {
			continue
		}

		var playFroms []string
		var playURLs []string
		for _, dl := range v.DLs {
			for _, dd := range dl.DDs {
				rawURL := strings.TrimSpace(dd.URL)
				if rawURL == "" {
					continue
				}
				// 过滤非视频直链（如网盘链接、纯网页链接等），防止与下游 PlayList 长度不一致导致播放源下标错位
				if len(converter.ConvertPlayUrl(rawURL)) == 0 {
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
				playURLs = append(playURLs, rawURL)
			}
		}

		playFromStr := strings.Join(playFroms, "$$$")
		playURLStr := strings.Join(playURLs, "$$$")

		details = append(details, model.FilmDetail{
			VodID:         v.ID,
			TypeID:        v.Tid,
			VodName:       name,
			TypeName:      strings.TrimSpace(v.Type),
			VodPic:        strings.TrimSpace(v.Pic),
			VodLang:       strings.TrimSpace(v.Lang),
			VodArea:       strings.TrimSpace(v.Area),
			VodYear:       strings.TrimSpace(v.Year),
			VodState:      strings.TrimSpace(v.State),
			VodRemarks:    strings.TrimSpace(v.Note),
			VodActor:      strings.TrimSpace(v.Actor),
			VodDirector:   strings.TrimSpace(v.Director),
			VodContent:    strings.TrimSpace(v.Des),
			VodTime:       strings.TrimSpace(v.Last),
			VodPlayFrom:   playFromStr,
			VodPlayURL:    playURLStr,
			VodPlayNote:   "$$$",
		})
	}
	return details
}

