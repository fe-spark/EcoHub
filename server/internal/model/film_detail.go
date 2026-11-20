package model

/*
 视频详情接口序列化 struct
*/

//-------------------------------------------------Json 格式-------------------------------------------------

// FilmDetailLPage  视频详情分页数据
type FilmDetailLPage struct {
	Code      int          `json:"code"`      // 响应状态码
	Msg       string       `json:"msg"`       // 数据类型
	Page      any          `json:"page"`      // 页码
	PageCount int          `json:"pagecount"` // 总页数
	Limit     any          `json:"limit"`     // 每页数据量
	Total     int          `json:"total"`     // 总数据量
	List      []FilmDetail `json:"list"`      // 影片详情数据List集合
}

// FilmDetail 视频详情列表
type FilmDetail struct {
	VodID            int64  `json:"vod_id"`                       // 影片ID
	TypeID           int64  `json:"type_id"`                      // 分类ID
	TypeID1          int64  `json:"type_id_1,omitempty"`          // 一级分类ID
	GroupID          int    `json:"group_id,omitempty"`           // 用户组ID
	VodName          string `json:"vod_name"`                     // 影片名称
	VodSub           string `json:"vod_sub,omitempty"`            // 影片别名
	VodEn            string `json:"vod_en,omitempty"`             //	影片名中文拼音
	VodStatus        int64  `json:"vod_status,omitempty"`         // 影片状态
	VodLetter        string `json:"vod_letter,omitempty"`         //	影片名首字母(大写)
	VodColor         string `json:"vod_color,omitempty"`          //	UI展示颜色
	VodTag           string `json:"vod_tag,omitempty"`            // 索引标签
	VodClass         string `json:"vod_class,omitempty"`          // 剧情分类标签
	VodPic           string `json:"vod_pic"`                      // 影片封面图
	VodPicThumb      string `json:"vod_pic_thumb,omitempty"`      // 缩略图
	VodPicSlide      string `json:"vod_pic_slide,omitempty"`      //  幻灯图片
	VodPicScreenshot string `json:"vod_pic_screenshot,omitempty"` // ?截图
	VodActor         string `json:"vod_actor,omitempty"`          // 演员名
	VodDirector      string `json:"vod_director,omitempty"`       // 导演
	VodWriter        string `json:"vod_writer,omitempty"`         // 作者
	VodBehind        string `json:"vod_behind,omitempty"`         // 幕后
	VodBlurb         string `json:"vod_blurb,omitempty"`          // 内容简介
	VodRemarks       string `json:"vod_remarks,omitempty"`        // 更新状态 ( 完结 || 更新值 xx集)
	VodPubDate       string `json:"vod_pubdate,omitempty"`        // 上映日期
	VodTotal         int64  `json:"vod_total,omitempty"`          // 总集数
	VodSerial        string `json:"vod_serial,omitempty"`         // 连载数
	VodTv            string `json:"vod_tv,omitempty"`             // 上映电视台
	VodWeekday       string `json:"vod_weekday,omitempty"`        // 节目周期
	VodArea          string `json:"vod_area,omitempty"`           // 地区
	VodLang          string `json:"vod_lang,omitempty"`           // 语言
	VodYear          string `json:"vod_year,omitempty"`           // 年代
	VodVersion       string `json:"vod_version,omitempty"`        // 画质版本 DVD || HD || 720P
	VodState         string `json:"vod_state,omitempty"`          // 影片类别 正片 || 花絮 || 预告
	VodAuthor        string `json:"vod_author,omitempty"`         // 编辑人员
	VodJumpUrl       string `json:"vod_jumpurl,omitempty"`        // 跳转url
	VodTpl           string `json:"vod_tpl,omitempty"`            // 独立模板
	VodTplPlay       string `json:"vod_tpl_play,omitempty"`       // 独立播放页模板
	VodTplDown       string `json:"vod_tpl_down,omitempty"`       // 独立下载页模板
	VodIsEnd         int64  `json:"vod_isend,omitempty"`          // 是否完结
	VodLock          int64  `json:"vod_lock,omitempty"`           // 锁定
	VodLevel         int64  `json:"vod_level,omitempty"`          // 推荐级别
	VodCopyright     int64  `json:"vod_copyright,omitempty"`      // 版权
	VodPoints        int64  `json:"vod_points,omitempty"`         // 积分
	VodPointsPlay    int64  `json:"vod_points_play,omitempty"`    // 点播付费
	VodPointsDown    int64  `json:"vod_points_down,omitempty"`    // 下载付费
	VodHits          int64  `json:"vod_hits,omitempty"`           // 总点击量
	VodHitsDay       int64  `json:"vod_hits_day,omitempty"`       // 日点击量
	VodHitsWeek      int64  `json:"vod_hits_week,omitempty"`      // 周点击量
	VodHitsMonth     int64  `json:"vod_hits_month,omitempty"`     // 月点击量
	VodDuration      string `json:"vod_duration,omitempty"`       // 时长
	VodUp            int64  `json:"vod_up,omitempty"`             // 顶数
	VodDown          int64  `json:"vod_down,omitempty"`           // 踩数
	VodScore         string `json:"vod_score,omitempty"`          // 平均分
	VodScoreAll      int64  `json:"vod_score_all,omitempty"`      // 总评分
	VodScoreNum      int64  `json:"vod_score_num,omitempty"`      // 评分次数
	VodTime          string `json:"vod_time,omitempty"`           // 更新时间
	VodTimeAdd       int64  `json:"vod_time_add,omitempty"`       // 添加时间
	VodTimeHits      int64  `json:"vod_time_hits,omitempty"`      // 点击时间
	VodTimeMake      int64  `json:"vod_time_make,omitempty"`      // 生成时间
	VodTrySee        int64  `json:"vod_trysee,omitempty"`         // 试看时长
	VodDouBanID      int64  `json:"vod_douban_id,omitempty"`      // 豆瓣ID
	VodDouBanScore   string `json:"vod_douban_score,omitempty"`   // 豆瓣评分
	VodReRrl         string `json:"vod_reurl,omitempty"`          // 来源地址
	VodRelVod        string `json:"vod_rel_vod,omitempty"`        // 关联视频ids
	VodRelArt        string `json:"vod_rel_art,omitempty"`        // 关联文章 ids
	VodPwd           string `json:"vod_pwd,omitempty"`            // 访问内容密码
	VodPwdURL        string `json:"vod_pwd_url,omitempty"`        // 访问密码连接
	VodPwdPlay       string `json:"vod_pwd_play,omitempty"`       // 访问播放页密码
	VodPwdPlayURL    string `json:"vod_pwd_play_url,omitempty"`   // 获取访问密码连接
	VodPwdDown       string `json:"vod_pwd_down,omitempty"`       // 访问下载页密码
	VodPwdDownURL    string `json:"vod_pwd_down_url,omitempty"`   // 获取下载密码连接
	VodContent       string `json:"vod_content,omitempty"`        // 详细介绍
	VodPlayFrom      string `json:"vod_play_from"`                // 播放组
	VodPlayServer    string `json:"vod_play_server,omitempty"`    // 播放组服务器
	VodPlayNote      string `json:"vod_play_note,omitempty"`      // 播放组备注 (分隔符)
	VodPlayURL       string `json:"vod_play_url"`                 // 播放地址
	VodDownFrom      string `json:"vod_down_from,omitempty"`      // 下载组
	VodDownServer    string `json:"vod_down_server,omitempty"`    // 瞎子服务器组
	VodDownNote      string `json:"vod_down_note,omitempty"`      // 下载备注 (分隔符)
	VodDownURL       string `json:"vod_down_url,omitempty"`       // 下载地址
	VodPlot          int64  `json:"vod_plot,omitempty"`           // 是否包含分级剧情
	VodPlotName      string `json:"vod_plot_name,omitempty"`      // 分类剧情名称
	VodPlotDetail    string `json:"vod_plot_detail,omitempty"`    // 分集剧情详情
	TypeName         string `json:"type_name"`                    // 分类名称
}
