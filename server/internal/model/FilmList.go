package model

import (
	"encoding/json"
	"encoding/xml"
)

/*
 视频列表接口序列化 struct
*/

//-------------------------------------------------Json 格式-------------------------------------------------

// CommonPage 影视列表接口分页数据结构体
type CommonPage struct {
	Code      int    `json:"code"`      // 响应状态码
	Msg       string `json:"msg"`       // 数据类型
	Page      any    `json:"page"`      // 页码
	PageCount int    `json:"pagecount"` // 总页数
	Limit     any    `json:"limit"`     // 每页数据量
	Total     int    `json:"total"`     // 总数据量
}

// FilmListPage 影视列表接口分页数据结构体
type FilmListPage struct {
	Code      int         `json:"code"`      // 响应状态码
	Msg       string      `json:"msg"`       // 数据类型
	Page      any         `json:"page"`      // 页码
	PageCount int         `json:"pagecount"` // 总页数
	Limit     any         `json:"limit"`     // 每页数据量
	Total     int         `json:"total"`     // 总数据量
	List      []FilmList  `json:"list"`      // 影片列表数据List集合
	Class     []FilmClass `json:"class"`     // 影片分类信息
}

// FilmList 影视列表单部影片信息结构体
type FilmList struct {
	VodID       int64  `json:"vod_id"`        // 影片ID
	VodName     string `json:"vod_name"`      // 影片名称
	TypeID      int64  `json:"type_id"`       // 分类ID
	TypeName    string `json:"type_name"`     // 分类名称
	VodEn       string `json:"vod_en"`        // 影片名中文拼音
	VodTime     string `json:"vod_time"`      // 更新时间
	VodRemarks  string `json:"vod_remarks"`   // 更新状态
	VodPlayFrom string `json:"vod_play_from"` // 播放来源
	VodPic      string `json:"vod_pic"`       // 影片图片
}

type FilmClass struct {
	ID   int64  `json:"id"`   // 分类ID
	Pid  int64  `json:"pid"`  // 父级ID
	Name string `json:"name"` // 类型名称
}

// UnmarshalJSON 核心兼容：支持采集站原始的 type_id/type_name 字段，同时保持结构体字段干净
func (fc *FilmClass) UnmarshalJSON(data []byte) error {
	type Alias FilmClass
	aux := struct {
		TypeID   int64  `json:"type_id"`
		TypeName string `json:"type_name"`
		*Alias
	}{
		Alias: (*Alias)(fc),
	}
	if err := json.Unmarshal(data, &aux); err != nil {
		return err
	}
	// 如果原始 id 为空，尝试用 type_id 填充
	if fc.ID == 0 {
		fc.ID = aux.TypeID
	}
	// 如果名称为空，尝试用 type_name 填充
	if fc.Name == "" {
		fc.Name = aux.TypeName
	}
	return nil
}

// MarshalJSON 核心兼容：在输出 JSON 时，同时输出 id/name (内部使用) 和 type_id/type_name (TVBox兼容)
func (fc FilmClass) MarshalJSON() ([]byte, error) {
	type Alias FilmClass
	return json.Marshal(&struct {
		TypeID   int64  `json:"type_id"`
		TypeName string `json:"type_name"`
		Alias
	}{
		TypeID:   fc.ID,
		TypeName: fc.Name,
		Alias:    (Alias)(fc),
	})
}

//-------------------------------------------------Xml 格式-------------------------------------------------

type RssL struct {
	XMLName xml.Name      `xml:"rss"`
	Version string        `xml:"version,attr"`
	List    FilmListPageX `xml:"list"`
	ClassXL ClassXL       `xml:"class"`
}
type FilmListPageX struct {
	XMLName     xml.Name    `xml:"list"`
	Page        any         `xml:"page,attr"`
	PageCount   int         `xml:"pagecount,attr"`
	PageSize    any         `xml:"pagesize,attr"`
	RecordCount int         `xml:"recordcount,attr"`
	Videos      []VideoList `xml:"video"`
}

type VideoList struct {
	Last string `xml:"last"`
	ID   int64  `xml:"id"`
	Tid  int64  `xml:"tid"`
	Name CDATA  `xml:"name"`
	Type string `xml:"type"`
	Dt   string `xml:"dt"`
	Note CDATA  `xml:"note"`
}

type ClassXL struct {
	XMLName xml.Name `xml:"class"`
	ClassX  []ClassX `xml:"ty"`
}

type ClassX struct {
	XMLName xml.Name `xml:"ty"`
	ID      int64    `xml:"id,attr"`
	Value   string   `xml:",chardata"`
}
