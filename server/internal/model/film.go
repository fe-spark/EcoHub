package model

import (
	"time"

	"server/internal/model/dto"

	"gorm.io/gorm"
)

// MovieDescriptor 影片详情介绍信息
type MovieDescriptor struct {
	SubTitle    string `json:"subTitle"`    // 子标题
	CName       string `json:"cName"`       // 分类名称
	EnName      string `json:"enName"`      // 英文名
	Initial     string `json:"initial"`     // 首字母
	ClassTag    string `json:"classTag"`    // 分类标签
	Actor       string `json:"actor"`       // 主演
	Director    string `json:"director"`    // 导演
	Writer      string `json:"writer"`      // 作者
	Blurb       string `json:"blurb"`       // 简介, 残缺,不建议使用
	Remarks     string `json:"remarks"`     // 更新情况
	ReleaseDate string `json:"releaseDate"` // 上映时间
	Area        string `json:"area"`        // 地区
	Language    string `json:"language"`    // 语言
	Year        string `json:"year"`        // 年份
	State       string `json:"state"`       // 影片状态 正片|预告...
	UpdateTime  string `json:"updateTime"`  // 更新时间
	AddTime     int64  `json:"addTime"`     // 资源添加时间戳
	DbId        int64  `json:"dbId"`        // 豆瓣id
	DbScore     string `json:"dbScore"`     // 豆瓣评分
	Hits        int64  `json:"hits"`        // 影片热度
	Content     string `json:"content"`     // 内容简介
}

// MovieBasicInfo 影片基本信息
type MovieBasicInfo struct {
	Id           int64  `json:"id"`                  // 影片Id
	Cid          int64  `json:"cid"`                 // 分类ID
	Pid          int64  `json:"pid"`                 // 一级分类ID
	Name         string `json:"name"`                // 片名
	SubTitle     string `json:"subTitle"`            // 子标题
	CName        string `json:"cName"`               // 分类名称
	State        string `json:"state"`               // 影片状态 正片|预告...
	Picture      string `json:"picture"`             // 竖版封面图
	PictureSlide string `json:"pictureSlide"`        // 横版幻灯图
	Actor        string `json:"actor"`               // 主演
	Director     string `json:"director"`            // 导演
	Blurb        string `json:"blurb"`               // 简介, 不完整
	Remarks      string `json:"remarks"`             // 更新情况
	Area         string `json:"area"`                // 地区
	Year         string `json:"year"`                // 年份
	ClassTag     string `json:"classTag,omitempty"`  // 源站分类/类型标签
	SourceId     string `json:"sourceId,omitempty"`  // 当前结果所属采集源
	SourceMid    int64  `json:"sourceMid,omitempty"` // 源站 vod_id，未对齐本地 mid 时进播用
}

// SearchSourceTab 搜索页按采集源分组的 Tab。
type SearchSourceTab struct {
	Id    string `json:"id"`
	Name  string `json:"name"`
	Count int    `json:"count,omitempty"`
}

// MovieUrlInfo 影视资源url信息
type MovieUrlInfo struct {
	Episode    string `json:"episode"`              // 集数
	Link       string `json:"link"`                 // 播放地址
	SourceId   string `json:"sourceId,omitempty"`   // 来源站ID
	SourceName string `json:"sourceName,omitempty"` // 来源站名称
}

// MovieDetail 影片详情信息
type MovieDetail struct {
	Id                 int64               `json:"id"`                 // 影片Id
	RawCid             int64               `json:"rawCid"`             // 原始来源分类ID
	RawPid             int64               `json:"rawPid"`             // 原始来源一级分类ID
	Cid                int64               `json:"cid"`                // 分类ID
	Pid                int64               `json:"pid"`                // 一级分类ID
	Name               string              `json:"name"`               // 片名
	Picture            string              `json:"picture"`            // 竖版封面图（源站/海报图源）
	PictureSlide       string              `json:"pictureSlide"`       // 横版幻灯图（源站/海报图源）
	CustomPicture      string              `json:"customPicture"`      // 自定义竖版封面图（独立存储）
	CustomPictureSlide string              `json:"customPictureSlide"` // 自定义横版幻灯图（独立存储）
	IsCustomPicture    bool                `json:"isCustomPicture"`    // 是否启用人工自定义海报
	PlayFrom           []string            `json:"playFrom"`           // 播放来源
	DownFrom           string              `json:"DownFrom"`           // 下载来源 例: http
	PlayList           [][]MovieUrlInfo    `json:"playList"`           // 播放地址url
	DownloadList       [][]MovieUrlInfo    `json:"downloadList"`       // 下载url地址
	MovieDescriptor    `json:"descriptor"` // 影片描述信息
}

func (d MovieDetail) DisplayPicture() string {
	if d.IsCustomPicture && len(d.CustomPicture) > 0 {
		return d.CustomPicture
	}
	return d.Picture
}

func (d MovieDetail) DisplayPictureSlide() string {
	if d.IsCustomPicture && len(d.CustomPictureSlide) > 0 {
		return d.CustomPictureSlide
	}
	return d.PictureSlide
}

// MovieSourceMapping 影片源站 ID 与全局影片 ID 的最小映射。
// 主键就是 (source_id, source_mid)，不再使用自增 id 加第二把唯一索引。
type MovieSourceMapping struct {
	CreatedAt time.Time
	UpdatedAt time.Time
	SourceId  string `gorm:"primaryKey;size:32;index:idx_global_source,priority:2"`
	SourceMid int64  `gorm:"primaryKey;autoIncrement:false"`
	GlobalMid int64  `gorm:"index:idx_global_source,priority:1"`
}

func (MovieSourceMapping) TableName() string {
	return TableMovieSourceMapping
}

// MoviePoster 附属站海报图源持久化模型。
// 当站点设为 IsPosterSource 时，采集时将海报按 match_key 写入该表。
// 主站入库和附属站采集双向反查该表，彻底解耦时序依赖。
type MoviePoster struct {
	gorm.Model
	SourceId     string `gorm:"uniqueIndex:uidx_poster_source_key;size:64"`
	MovieKey     string `gorm:"uniqueIndex:uidx_poster_source_key;index:idx_movie_poster_movie_key;size:128"`
	Picture      string `gorm:"type:text"`
	PictureSlide string `gorm:"type:text"`
}

func (MoviePoster) TableName() string {
	return TableMoviePoster
}

// MovieMatchKey 主站影片匹配键索引（豆瓣 / 片名#大类 / 纯片名回退）。
// 主键就是 (mid, match_key)。match_key 只保留普通索引，供跨站反查。
type MovieMatchKey struct {
	CreatedAt time.Time
	UpdatedAt time.Time
	Mid       int64  `gorm:"primaryKey;autoIncrement:false"`
	MatchKey  string `gorm:"primaryKey;size:64;index:idx_match_key"`
}

func (MovieMatchKey) TableName() string {
	return TableMovieMatchKey
}

// FilmIndexIdentity 索引标识层：只负责来源与主键归属。
type FilmIndexIdentity struct {
	Mid           int64  `json:"mid" gorm:"primaryKey;autoIncrement;index:idx_root_key_upd_stamp,priority:3;index:idx_root_key_hits_val,priority:3;index:idx_cat_key_upd_stamp,priority:3;index:idx_cat_key_hits_val,priority:3"` // 系统内全局唯一影片 ID
	FirstSourceId string `json:"firstSourceId" gorm:"size:32;index"`                                                                                                                                                              // 首次录入该片的站点 ID
	DbId          int64  `json:"dbId" gorm:"index"`                                                                                                                                                                               // 豆瓣 ID (用于精准去重)
}

// FilmIndexCategory 分类层：RootCategoryKey/CategoryKey 是来源分类身份；Pid/Cid/CName 仅作写入快照和兼容展示。
type FilmIndexCategory struct {
	Cid              int64  `json:"cid" gorm:"index;index:idx_pid_update;index:idx_cid_update;index:idx_pid_hits;index:idx_cid_hits;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits"`                                                                            // 分类ID
	Pid              int64  `json:"pid" gorm:"index;index:idx_film_index_pid_update_mid,priority:1;index:idx_pid_update;index:idx_cid_update;index:idx_pid_hits;index:idx_cid_hits;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits;constraint:OnDelete:CASCADE"` // 上级分类ID
	RootCategoryKey  string `json:"rootCategoryKey" gorm:"size:128;index;index:idx_root_key_update;index:idx_root_key_hits;index:idx_root_key_upd_stamp,priority:1;index:idx_root_key_hits_val,priority:1;index:idx_filter_root_score;index:idx_filter_root_update;index:idx_filter_root_hits"`
	CategoryKey      string `json:"categoryKey" gorm:"size:128;index;index:idx_category_key_update;index:idx_category_key_hits;index:idx_category_key_latest;index:idx_cat_key_upd_stamp,priority:1;index:idx_cat_key_hits_val,priority:1"`
	OriginalCategory string `json:"originalCategory" gorm:"size:128;index"` // 采集时固化的来源主类名
	CName            string `json:"cName"`                                  // 当前展示用分类名
}

// FilmIndexContent 展示内容层：列表与详情入口直接消费的字段。
type FilmIndexContent struct {
	SeriesKey          string  `json:"seriesKey" gorm:"size:128;index"`                                                                                                                                                                                                                                     // 系列标识，用于相关推荐召回与排序
	Name               string  `json:"name"`                                                                                                                                                                                                                                                                // 片名
	SubTitle           string  `json:"subTitle" gorm:"type:text"`                                                                                                                                                                                                                                           // 影片子标题
	ClassTag           string  `json:"classTag" gorm:"type:text"`                                                                                                                                                                                                                                           // 类型标签
	Area               string  `json:"area" gorm:"index;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits"`                                                                                                                                                                              // 地区
	Language           string  `json:"language" gorm:"index;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits"`                                                                                                                                                                          // 语言
	Year               int64   `json:"year" gorm:"index;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits"`                                                                                                                                                                              // 年份
	Initial            string  `json:"initial"`                                                                                                                                                                                                                                                             // 首字母
	Score              float64 `json:"score" gorm:"index;index:idx_filter_score"`                                                                                                                                                                                                                           // 评分
	UpdateStamp        int64   `json:"updateStamp" gorm:"index;index:idx_film_index_update_mid,priority:1;index:idx_film_index_pid_update_mid,priority:2;index:idx_root_key_upd_stamp,priority:2;index:idx_cat_key_upd_stamp,priority:2;index:idx_pid_update;index:idx_cid_update;index:idx_filter_update"` // 更新时间
	UpdateReason       string  `json:"updateReason" gorm:"type:varchar(64)"`                                                                                                                                                                                                                                // 更新原因
	Hits               int64   `json:"hits" gorm:"index;index:idx_pid_hits;index:idx_cid_hits;index:idx_root_key_hits_val,priority:2;index:idx_cat_key_hits_val,priority:2;index:idx_filter_hits"`                                                                                                          // 热度排行
	State              string  `json:"state"`                                                                                                                                                                                                                                                               // 状态 正片|预告
	Remarks            string  `json:"remarks"`                                                                                                                                                                                                                                                             // 完结 | 更新至x集
	Picture            string  `json:"picture" gorm:"type:text"`                                                                                                                                                                                                                                            // 竖版封面图（源站/海报源原图）
	PictureSlide       string  `json:"pictureSlide" gorm:"type:text"`                                                                                                                                                                                                                                       // 横版幻灯图（源站/海报源原图）
	CustomPicture      string  `json:"customPicture" gorm:"type:text"`                                                                                                                                                                                                                                      // 自定义竖版封面图（独立存储）
	CustomPictureSlide string  `json:"customPictureSlide" gorm:"type:text"`                                                                                                                                                                                                                                 // 自定义横版幻灯图（独立存储）
	IsCustomPicture    bool    `json:"isCustomPicture" gorm:"default:false"`                                                                                                                                                                                                                                // 是否人工自定义海报（锁定保护）
	Actor              string  `json:"actor" gorm:"type:text"`                                                                                                                                                                                                                                              // 主演
	Director           string  `json:"director" gorm:"type:text"`                                                                                                                                                                                                                                           // 导演
	Writer             string  `json:"writer" gorm:"type:text"`                                                                                                                                                                                                                                             // 编剧
	Blurb              string  `json:"blurb" gorm:"type:text"`                                                                                                                                                                                                                                              // 简介, 不完整
	Content            string  `json:"content" gorm:"type:longtext"`                                                                                                                                                                                                                                        // 完整详情内容
	ReleaseDate        string  `json:"releaseDate" gorm:"size:64"`                                                                                                                                                                                                                                          // 上映日期
}

func (c FilmIndexContent) DisplayPicture() string {
	if c.IsCustomPicture && len(c.CustomPicture) > 0 {
		return c.CustomPicture
	}
	return c.Picture
}

func (c FilmIndexContent) DisplayPictureSlide() string {
	if c.IsCustomPicture && len(c.CustomPictureSlide) > 0 {
		return c.CustomPictureSlide
	}
	return c.PictureSlide
}

// FilmIndexVersion 入库版本层：用于排障与版本追踪。
type FilmIndexVersion struct {
	CollectStamp    int64  `json:"collectStamp" gorm:"column:collect_stamp;index"` // 采集/入库时间时间戳
	CategoryVersion string `json:"categoryVersion" gorm:"size:64;index"`           // 采集时使用的分类版本
	RuleVersion     string `json:"ruleVersion" gorm:"size:64;index"`               // 采集时使用的规则版本
}

// FilmIndexDerived 衍生结果层：可重新计算的聚合字段。
type FilmIndexDerived struct {
	PlayFromSummary string `json:"playFromSummary"` // 播放源摘要，供列表接口直出
}

// FilmIndex 存储影片检索与展示入口所需的数据。
type FilmIndex struct {
	CreatedAt         time.Time      `json:"createdAt"`
	UpdatedAt         time.Time      `json:"updatedAt"`
	DeletedAt         gorm.DeletedAt `json:"deletedAt" gorm:"index"`
	FilmIndexIdentity `gorm:"embedded"`
	FilmIndexCategory `gorm:"embedded"`
	FilmIndexContent  `gorm:"embedded"`
	FilmIndexVersion  `gorm:"embedded"`
	FilmIndexDerived  `gorm:"embedded"`
}

func (FilmIndex) TableName() string {
	return TableFilmIndex
}

// FilmSourcePlaylist 统一多源播放列表持久化模型。
// 主键就是线路身份。group_index 从 0 开始，必须允许 0 值写入。
type FilmSourcePlaylist struct {
	CreatedAt    time.Time
	UpdatedAt    time.Time
	Mid          int64  `gorm:"primaryKey;autoIncrement:false;index:idx_playlist_source_mid,priority:2"`
	SourceId     string `gorm:"primaryKey;size:32;index:idx_playlist_source_mid,priority:1"`
	LineKind     string `gorm:"primaryKey;size:16"` // play 或 download
	GroupIndex   int    `gorm:"primaryKey;autoIncrement:false"`
	GroupName    string `gorm:"type:varchar(255)"`
	EpisodeCount int    `gorm:"default:0"`
	LastEpisode  string `gorm:"size:64"`
	ContentHash  string `gorm:"size:32"`       // line_kind、group_index、group_name、Content 原文的 MD5
	Content      string `gorm:"type:longtext"` // 原始 []MovieUrlInfo JSON，含 episode 与 link
}

func (FilmSourcePlaylist) TableName() string {
	return TableFilmSourcePlaylist
}

// FilmListSnapshot 是列表接口的内存结构，由 film_index 现场组装，不落库。
type FilmListSnapshot struct {
	CreatedAt       time.Time
	UpdatedAt       time.Time
	SnapshotVersion string `json:"snapshotVersion"`
	Mid             int64  `json:"mid"`
	SourceId        string `json:"sourceId"`
	DbId            int64  `json:"dbId"`

	Cid              int64  `json:"cid"`
	Pid              int64  `json:"pid"`
	RootCategoryKey  string `json:"rootCategoryKey"`
	CategoryKey      string `json:"categoryKey"`
	OriginalCategory string `json:"originalCategory"`
	CName            string `json:"cName"`

	SeriesKey          string  `json:"seriesKey"`
	Name               string  `json:"name"`
	SubTitle           string  `json:"subTitle"`
	ClassTag           string  `json:"classTag"`
	Area               string  `json:"area"`
	Language           string  `json:"language"`
	Year               int64   `json:"year"`
	Initial            string  `json:"initial"`
	Score              float64 `json:"score"`
	UpdateStamp        int64   `json:"updateStamp"`
	UpdateReason       string  `json:"updateReason"`
	Hits               int64   `json:"hits"`
	State              string  `json:"state"`
	Remarks            string  `json:"remarks"`
	Picture            string  `json:"picture"`
	PictureSlide       string  `json:"pictureSlide"`
	CustomPicture      string  `json:"customPicture"`
	CustomPictureSlide string  `json:"customPictureSlide"`
	IsCustomPicture    bool    `json:"isCustomPicture"`
	Actor              string  `json:"actor"`
	Director           string  `json:"director"`
	Writer             string  `json:"writer"`
	Blurb              string  `json:"blurb"`
	Content            string  `json:"content"`
	ReleaseDate        string  `json:"releaseDate"`
	CollectStamp       int64   `json:"collectStamp"`
	CategoryVersion    string  `json:"categoryVersion"`
	RuleVersion        string  `json:"ruleVersion"`
	PlayFromSummary    string  `json:"playFromSummary"`
}

func (s FilmListSnapshot) DisplayPicture() string {
	if s.IsCustomPicture && len(s.CustomPicture) > 0 {
		return s.CustomPicture
	}
	return s.Picture
}

func (s FilmListSnapshot) DisplayPictureSlide() string {
	if s.IsCustomPicture && len(s.CustomPictureSlide) > 0 {
		return s.CustomPictureSlide
	}
	return s.PictureSlide
}

// SearchTagItem 影片检索标签持久化模型 (MySQL)
type SearchTagItem struct {
	gorm.Model
	Pid     int64  `gorm:"uniqueIndex:uidx_search_tag;index:idx_tag_score;not null;constraint:OnDelete:CASCADE"`
	TagType string `gorm:"uniqueIndex:uidx_search_tag;index:idx_tag_score;size:32;not null"` // Category/Plot/Area/Language/Year/Initial/Sort
	Name    string `gorm:"size:128;not null"`                                                // 展示名称
	Value   string `gorm:"uniqueIndex:uidx_search_tag;size:128;not null"`                    // 筛选值
	Score   int64  `gorm:"index:idx_tag_score;default:0"`                                    // 热度权重，用于排序
}

func (SearchTagItem) TableName() string {
	return TableSearchTag
}

// SearchTagsVO 搜索标签请求参数
type SearchTagsVO struct {
	Pid              int64  `json:"pid"`
	Cid              int64  `json:"cid"`
	OriginalCategory string `json:"originalCategory"`
	Plot             string `json:"plot"`
	Area             string `json:"area"`
	Language         string `json:"language"`
	Year             string `json:"year"`
	Sort             string `json:"sort"`
	SourceId         string `json:"sourceId"`
}

// SearchVo 影片信息搜索参数
type SearchVo struct {
	SourceId  string    `json:"sourceId"`  // 采集源ID
	Name      string    `json:"name"`      // 影片名
	Pid       int64     `json:"pid"`       // 一级分类ID
	Cid       int64     `json:"cid"`       // 二级分类ID
	Plot      string    `json:"plot"`      // 剧情
	Area      string    `json:"area"`      // 地区
	Language  string    `json:"language"`  // 语言
	Year      int64     `json:"year"`      // 年份
	BeginTime int64     `json:"beginTime"` // 更新时间戳起始值
	EndTime   int64     `json:"endTime"`   // 更新时间戳结束值
	Paging    *dto.Page `json:"paging"`    // 分页参数
}

// FilmDetailVo 添加影片对象
type FilmDetailVo struct {
	Id                 int64    `json:"id"`                 // 影片id
	Cid                int64    `json:"cid"`                // 分类ID
	Pid                int64    `json:"pid"`                // 一级分类ID
	Name               string   `json:"name"`               // 片名
	Picture            string   `json:"picture"`            // 竖版封面图（源站/海报源原图）
	PictureSlide       string   `json:"pictureSlide"`       // 横版幻灯图（源站/海报源原图）
	CustomPicture      string   `json:"customPicture"`      // 自定义竖版封面图（独立存储）
	CustomPictureSlide string   `json:"customPictureSlide"` // 自定义横版幻灯图（独立存储）
	IsCustomPicture    bool     `json:"isCustomPicture"`    // 是否人工自定义海报（锁定保护）
	PlayFrom           []string `json:"playFrom"`           // 播放来源
	DownFrom           string   `json:"DownFrom"`           // 下载来源 例: http
	PlayLink           string   `json:"playLink"`           // 播放地址url
	DownloadLink       string   `json:"downloadLink"`       // 下载url地址
	SubTitle           string   `json:"subTitle"`           // 子标题
	CName              string   `json:"cName"`              // 分类名称
	EnName             string   `json:"enName"`             // 英文名
	Initial            string   `json:"initial"`            // 首字母
	ClassTag           string   `json:"classTag"`           // 分类标签
	Actor              string   `json:"actor"`              // 主演
	Director           string   `json:"director"`           // 导演
	Writer             string   `json:"writer"`             // 作者
	Remarks            string   `json:"remarks"`            // 更新情况
	ReleaseDate        string   `json:"releaseDate"`        // 上映时间
	Area               string   `json:"area"`               // 地区
	Language           string   `json:"language"`           // 语言
	Year               string   `json:"year"`               // 年份
	State              string   `json:"state"`              // 影片状态 正片|预告...
	UpdateTime         string   `json:"updateTime"`         // 更新时间
	AddTime            string   `json:"addTime"`            // 资源添加时间戳
	DbId               int64    `json:"dbId"`               // 豆瓣id
	DbScore            string   `json:"dbScore"`            // 豆瓣评分
	Hits               int64    `json:"hits"`               // 影片热度
	Content            string   `json:"content"`            // 内容简介
}

// PlayLinkVo 多站点播放链接数据列表
type PlayLinkVo struct {
	Id          string         `json:"id"`
	SourceId    string         `json:"sourceId"`
	Name        string         `json:"name"`
	IsPreferred bool           `json:"isPreferred,omitempty"`
	Proxy       bool           `json:"proxy,omitempty"`
	LinkList    []MovieUrlInfo `json:"linkList"`
}

// MovieDetailVo 影片详情数据, 播放源合并版
type MovieDetailVo struct {
	MovieDetail
	List            []PlayLinkVo `json:"list"`
	LocalUpdateTime int64        `json:"localUpdateTime"` // 本地详情更新时间戳
	UpdateReason    string       `json:"updateReason"`    // 更新原因
}
