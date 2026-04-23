package model

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
