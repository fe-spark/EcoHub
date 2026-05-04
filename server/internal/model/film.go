package model

import (
	"server/internal/model/dto"

	"gorm.io/gorm"
)

// Movie 影片基本信息
type Movie struct {
	Id       int64  `json:"id"`       // 影片ID
	Name     string `json:"name"`     // 影片名
	Cid      int64  `json:"cid"`      // 所属分类ID
	CName    string `json:"CName"`    // 所属分类名称
	EnName   string `json:"enName"`   // 英文片名
	Time     string `json:"time"`     // 更新时间
	Remarks  string `json:"remarks"`  // 备注 | 清晰度
	PlayFrom string `json:"playFrom"` // 播放来源
}

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
	Id           int64  `json:"id"`           // 影片Id
	Cid          int64  `json:"cid"`          // 分类ID
	Pid          int64  `json:"pid"`          // 一级分类ID
	Name         string `json:"name"`         // 片名
	SubTitle     string `json:"subTitle"`     // 子标题
	CName        string `json:"cName"`        // 分类名称
	State        string `json:"state"`        // 影片状态 正片|预告...
	Picture      string `json:"picture"`      // 竖版封面图
	PictureSlide string `json:"pictureSlide"` // 横版幻灯图
	Actor        string `json:"actor"`        // 主演
	Director     string `json:"director"`     // 导演
	Blurb        string `json:"blurb"`        // 简介, 不完整
	Remarks      string `json:"remarks"`      // 更新情况
	Area         string `json:"area"`         // 地区
	Year         string `json:"year"`         // 年份
}

// MovieUrlInfo 影视资源url信息
type MovieUrlInfo struct {
	Episode string `json:"episode"` // 集数
	Link    string `json:"link"`    // 播放地址
}

// MovieDetail 影片详情信息
type MovieDetail struct {
	Id              int64               `json:"id"`           // 影片Id
	RawCid          int64               `json:"rawCid"`       // 原始来源分类ID
	RawPid          int64               `json:"rawPid"`       // 原始来源一级分类ID
	Cid             int64               `json:"cid"`          // 分类ID
	Pid             int64               `json:"pid"`          // 一级分类ID
	Name            string              `json:"name"`         // 片名
	Picture         string              `json:"picture"`      // 竖版封面图
	PictureSlide    string              `json:"pictureSlide"` // 横版幻灯图
	PlayFrom        []string            `json:"playFrom"`     // 播放来源
	DownFrom        string              `json:"DownFrom"`     // 下载来源 例: http
	PlayList        [][]MovieUrlInfo    `json:"playList"`     // 播放地址url
	DownloadList    [][]MovieUrlInfo    `json:"downloadList"` // 下载url地址
	MovieDescriptor `json:"descriptor"` // 影片描述信息
}

// MovieDetailInfo 影片详情持久化模型 (MySQL)
type MovieDetailInfo struct {
	gorm.Model
	Mid             int64  `gorm:"uniqueIndex"`
	SourceId        string `gorm:"index"` // 预留：标识主站来源
	CategoryVersion string `gorm:"size:64;index"`
	RuleVersion     string `gorm:"size:64;index"`
	Content         string `gorm:"type:longtext"` // 存储序列化后的完整 MovieDetail JSON
}

// MovieSourceMapping 影片源站 ID 与全局影片 ID 的最小映射。
// 主站详情写入和附属站播放列表采集匹配后都会维护该表。
// 当前仅用于后台“单片更新全部站点”时，把全局 mid 翻译回各站 source_mid。
type MovieSourceMapping struct {
	gorm.Model
	SourceId  string `gorm:"uniqueIndex:uidx_source_mid"`
	SourceMid int64  `gorm:"uniqueIndex:uidx_source_mid"`
	GlobalMid int64  `gorm:"index"`
}

// MoviePlaylist 附属站播放列表持久化模型。
// 主站不写该表，附属站采集后按匹配键写入，供详情页和播放页直接聚合读取。
type MoviePlaylist struct {
	gorm.Model
	SourceId   string `gorm:"uniqueIndex:uidx_source_key_group"`
	MovieKey   string `gorm:"uniqueIndex:uidx_source_key_group"`
	GroupIndex int    `gorm:"uniqueIndex:uidx_source_key_group"`
	GroupName  string `gorm:"type:varchar(255)"`
	Content    string `gorm:"type:longtext"`
}

// MovieMatchKey 主站影片匹配键索引。
// 主站详情会写入多个匹配键：优先豆瓣ID，同时保留规范化片名，供详情页实时补附属站播放源。
type MovieMatchKey struct {
	gorm.Model
	Mid      int64  `gorm:"uniqueIndex:uidx_mid_match;index:idx_match_key"`
	MatchKey string `gorm:"size:64;uniqueIndex:uidx_mid_match;index:idx_match_key"`
}

func (MovieMatchKey) TableName() string {
	return TableMovieMatchKey
}

// FilmIndexIdentity 索引标识层：只负责来源与主键归属。
type FilmIndexIdentity struct {
	Mid        int64  `json:"mid" gorm:"uniqueIndex:idx_mid"`            // 影片ID (全局唯一)
	ContentKey string `json:"contentKey" gorm:"uniqueIndex:idx_content"` // 主站内容指纹：优先豆瓣ID，其次规范化片名
	SourceId   string `json:"sourceId" gorm:"index"`                     // 来源站点ID
	DbId       int64  `json:"dbId" gorm:"index"`                         // 豆瓣ID (用于精准去重)
}

// FilmIndexCategory 分类层：RootCategoryKey/CategoryKey 是来源分类身份；Pid/Cid/CName 仅作写入快照和兼容展示。
type FilmIndexCategory struct {
	Cid              int64  `json:"cid" gorm:"index;index:idx_pid_update;index:idx_cid_update;index:idx_pid_hits;index:idx_cid_hits;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits"`                             // 分类ID
	Pid              int64  `json:"pid" gorm:"index;index:idx_pid_update;index:idx_cid_update;index:idx_pid_hits;index:idx_cid_hits;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits;constraint:OnDelete:CASCADE"` // 上级分类ID
	RootCategoryKey  string `json:"rootCategoryKey" gorm:"size:128;index;index:idx_root_key_update;index:idx_root_key_hits;index:idx_filter_root_score;index:idx_filter_root_update;index:idx_filter_root_hits"`
	CategoryKey      string `json:"categoryKey" gorm:"size:128;index;index:idx_category_key_update;index:idx_category_key_hits;index:idx_category_key_latest"`
	OriginalCategory string `json:"originalCategory" gorm:"size:128;index"` // 采集时固化的来源主类名
	CName            string `json:"cName"`                                  // 当前展示用分类名
}

// FilmIndexContent 展示内容层：列表与详情入口直接消费的字段。
type FilmIndexContent struct {
	SeriesKey    string  `json:"seriesKey" gorm:"size:128;index"`                                                            // 系列标识，用于相关推荐召回与排序
	Name         string  `json:"name"`                                                                                       // 片名
	SubTitle     string  `json:"subTitle"`                                                                                   // 影片子标题
	ClassTag     string  `json:"classTag"`                                                                                   // 类型标签
	Area         string  `json:"area" gorm:"index;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits"`     // 地区
	Language     string  `json:"language" gorm:"index;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits"` // 语言
	Year         int64   `json:"year" gorm:"index;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits"`     // 年份
	Initial      string  `json:"initial"`                                                                                    // 首字母
	Score        float64 `json:"score" gorm:"index;index:idx_filter_score"`                                                  // 评分
	UpdateStamp  int64   `json:"updateStamp" gorm:"index;index:idx_pid_update;index:idx_cid_update;index:idx_filter_update"` // 更新时间
	Hits         int64   `json:"hits" gorm:"index;index:idx_pid_hits;index:idx_cid_hits;index:idx_filter_hits"`              // 热度排行
	State        string  `json:"state"`                                                                                      // 状态 正片|预告
	Remarks      string  `json:"remarks"`                                                                                    // 完结 | 更新至x集
	Picture      string  `json:"picture"`                                                                                    // 竖版封面图
	PictureSlide string  `json:"pictureSlide" gorm:"size:512"`                                                               // 横版幻灯图
	Actor        string  `json:"actor"`                                                                                      // 主演
	Director     string  `json:"director"`                                                                                   // 导演
	Blurb        string  `json:"blurb"`                                                                                      // 简介, 不完整
}

// FilmIndexVersion 入库版本层：用于排障与版本追踪。
type FilmIndexVersion struct {
	CollectStamp    int64  `json:"collectStamp" gorm:"column:collect_stamp;index"` // 采集/入库时间时间戳
	CategoryVersion string `json:"categoryVersion" gorm:"size:64;index"`           // 采集时使用的分类版本
	RuleVersion     string `json:"ruleVersion" gorm:"size:64;index"`               // 采集时使用的规则版本
}

// FilmIndexDerived 衍生结果层：可重新计算的聚合字段。
type FilmIndexDerived struct {
	PlayFromSummary string `json:"playFromSummary"` // 主站播放源摘要，供列表接口直出
}

// FilmIndex 存储影片检索与展示入口所需的数据。
// 结构上拆成：标识层 / 分类归属层 / 展示内容层 / 版本层 / 衍生层。
type FilmIndex struct {
	gorm.Model
	FilmIndexIdentity `gorm:"embedded"`
	FilmIndexCategory `gorm:"embedded"`
	FilmIndexContent  `gorm:"embedded"`
	FilmIndexVersion  `gorm:"embedded"`
	FilmIndexDerived  `gorm:"embedded"`
}

func (FilmIndex) TableName() string {
	return TableFilmIndex
}

// FilmListSnapshot 是前台列表与 TVBox 列表的只读快照。
// 采集写入仍落 film_index，采集收尾成功后重建新版本快照并原子切换 active version。
type FilmListSnapshot struct {
	gorm.Model
	SnapshotVersion string `json:"snapshotVersion" gorm:"size:64;uniqueIndex:uidx_snapshot_mid;index:idx_snapshot_pid_update;index:idx_snapshot_cid_update;index:idx_snapshot_pid_hits;index:idx_snapshot_cid_hits;index:idx_snapshot_filter_score;index:idx_snapshot_filter_update;index:idx_snapshot_filter_hits"`
	Mid             int64  `json:"mid" gorm:"uniqueIndex:uidx_snapshot_mid;index"`
	ContentKey      string `json:"contentKey" gorm:"size:128;index"`
	SourceId        string `json:"sourceId" gorm:"index"`
	DbId            int64  `json:"dbId" gorm:"index"`

	Cid              int64  `json:"cid" gorm:"index;index:idx_snapshot_pid_update;index:idx_snapshot_cid_update;index:idx_snapshot_pid_hits;index:idx_snapshot_cid_hits;index:idx_snapshot_filter_score;index:idx_snapshot_filter_update;index:idx_snapshot_filter_hits"`
	Pid              int64  `json:"pid" gorm:"index;index:idx_snapshot_pid_update;index:idx_snapshot_cid_update;index:idx_snapshot_pid_hits;index:idx_snapshot_cid_hits;index:idx_snapshot_filter_score;index:idx_snapshot_filter_update;index:idx_snapshot_filter_hits"`
	RootCategoryKey  string `json:"rootCategoryKey" gorm:"size:128;index;index:idx_snapshot_root_key_update;index:idx_snapshot_root_key_hits;index:idx_snapshot_filter_root_score;index:idx_snapshot_filter_root_update;index:idx_snapshot_filter_root_hits"`
	CategoryKey      string `json:"categoryKey" gorm:"size:128;index;index:idx_snapshot_category_key_update;index:idx_snapshot_category_key_hits"`
	OriginalCategory string `json:"originalCategory" gorm:"size:128;index"`
	CName            string `json:"cName"`

	SeriesKey       string  `json:"seriesKey" gorm:"size:128;index"`
	Name            string  `json:"name"`
	SubTitle        string  `json:"subTitle"`
	ClassTag        string  `json:"classTag"`
	Area            string  `json:"area" gorm:"index;index:idx_snapshot_filter_score;index:idx_snapshot_filter_update;index:idx_snapshot_filter_hits"`
	Language        string  `json:"language" gorm:"index;index:idx_snapshot_filter_score;index:idx_snapshot_filter_update;index:idx_snapshot_filter_hits"`
	Year            int64   `json:"year" gorm:"index;index:idx_snapshot_filter_score;index:idx_snapshot_filter_update;index:idx_snapshot_filter_hits"`
	Initial         string  `json:"initial"`
	Score           float64 `json:"score" gorm:"index;index:idx_snapshot_filter_score"`
	UpdateStamp     int64   `json:"updateStamp" gorm:"index;index:idx_snapshot_pid_update;index:idx_snapshot_cid_update;index:idx_snapshot_filter_update"`
	Hits            int64   `json:"hits" gorm:"index;index:idx_snapshot_pid_hits;index:idx_snapshot_cid_hits;index:idx_snapshot_filter_hits"`
	State           string  `json:"state"`
	Remarks         string  `json:"remarks"`
	Picture         string  `json:"picture"`
	PictureSlide    string  `json:"pictureSlide" gorm:"size:512"`
	Actor           string  `json:"actor"`
	Director        string  `json:"director"`
	Blurb           string  `json:"blurb"`
	CollectStamp    int64   `json:"collectStamp" gorm:"column:collect_stamp;index"`
	CategoryVersion string  `json:"categoryVersion" gorm:"size:64;index"`
	RuleVersion     string  `json:"ruleVersion" gorm:"size:64;index"`
	PlayFromSummary string  `json:"playFromSummary"`
}

func (FilmListSnapshot) TableName() string {
	return TableFilmListSnapshot
}

// FilmFilterOptionSnapshot 是当前前台快照版本下的筛选项读模型。
// 它在快照发布阶段生成，前台、后台和 TVBox 配置只读取该表，不在请求链路实时聚合标签。
type FilmFilterOptionSnapshot struct {
	gorm.Model
	SnapshotVersion string `json:"snapshotVersion" gorm:"size:64;uniqueIndex:uidx_filter_option;index:idx_filter_option_lookup"`
	Pid             int64  `json:"pid" gorm:"uniqueIndex:uidx_filter_option;index:idx_filter_option_lookup;not null"`
	TagType         string `json:"tagType" gorm:"size:32;uniqueIndex:uidx_filter_option;index:idx_filter_option_lookup;not null"`
	Name            string `json:"name" gorm:"size:128;not null"`
	Value           string `json:"value" gorm:"size:128;uniqueIndex:uidx_filter_option;not null"`
	Score           int64  `json:"score" gorm:"index;default:0"`
	Sort            int    `json:"sort" gorm:"index;default:0"`
}

func (FilmFilterOptionSnapshot) TableName() string {
	return TableFilterOption
}

// FilmFilterIndexSnapshot 是当前前台快照版本下的筛选倒排索引。
// 它把 Category/Plot/Area/Language/Year 映射到影片 mid，避免请求时扫描 class_tag 或全量快照。
type FilmFilterIndexSnapshot struct {
	gorm.Model
	SnapshotVersion string  `json:"snapshotVersion" gorm:"size:64;uniqueIndex:uidx_filter_index;index:idx_filter_index_lookup"`
	Pid             int64   `json:"pid" gorm:"uniqueIndex:uidx_filter_index;index:idx_filter_index_lookup;not null"`
	Cid             int64   `json:"cid" gorm:"index"`
	TagType         string  `json:"tagType" gorm:"size:32;uniqueIndex:uidx_filter_index;index:idx_filter_index_lookup;not null"`
	TagValue        string  `json:"tagValue" gorm:"size:128;uniqueIndex:uidx_filter_index;index:idx_filter_index_lookup;not null"`
	Mid             int64   `json:"mid" gorm:"uniqueIndex:uidx_filter_index;index;not null"`
	Year            int64   `json:"year" gorm:"index"`
	UpdateStamp     int64   `json:"updateStamp" gorm:"index"`
	Hits            int64   `json:"hits" gorm:"index"`
	Score           float64 `json:"score" gorm:"index"`
}

func (FilmFilterIndexSnapshot) TableName() string {
	return TableFilterIndex
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

// Tag 影片分类标签结构体
type Tag struct {
	Name  string `json:"name"`
	Value any    `json:"value"`
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
}

// SearchVo 影片信息搜索参数
type SearchVo struct {
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
	Id           int64    `json:"id"`           // 影片id
	Cid          int64    `json:"cid"`          // 分类ID
	Pid          int64    `json:"pid"`          // 一级分类ID
	Name         string   `json:"name"`         // 片名
	Picture      string   `json:"picture"`      // 竖版封面图
	PictureSlide string   `json:"pictureSlide"` // 横版幻灯图
	PlayFrom     []string `json:"playFrom"`     // 播放来源
	DownFrom     string   `json:"DownFrom"`     // 下载来源 例: http
	PlayLink     string   `json:"playLink"`     // 播放地址url
	DownloadLink string   `json:"downloadLink"` // 下载url地址
	SubTitle     string   `json:"subTitle"`     // 子标题
	CName        string   `json:"cName"`        // 分类名称
	EnName       string   `json:"enName"`       // 英文名
	Initial      string   `json:"initial"`      // 首字母
	ClassTag     string   `json:"classTag"`     // 分类标签
	Actor        string   `json:"actor"`        // 主演
	Director     string   `json:"director"`     // 导演
	Writer       string   `json:"writer"`       // 作者
	Remarks      string   `json:"remarks"`      // 更新情况
	ReleaseDate  string   `json:"releaseDate"`  // 上映时间
	Area         string   `json:"area"`         // 地区
	Language     string   `json:"language"`     // 语言
	Year         string   `json:"year"`         // 年份
	State        string   `json:"state"`        // 影片状态 正片|预告...
	UpdateTime   string   `json:"updateTime"`   // 更新时间
	AddTime      string   `json:"addTime"`      // 资源添加时间戳
	DbId         int64    `json:"dbId"`         // 豆瓣id
	DbScore      string   `json:"dbScore"`      // 豆瓣评分
	Hits         int64    `json:"hits"`         // 影片热度
	Content      string   `json:"content"`      // 内容简介
}

// PlayLinkVo 多站点播放链接数据列表
type PlayLinkVo struct {
	Id       string         `json:"id"`
	SourceId string         `json:"sourceId"`
	Name     string         `json:"name"`
	LinkList []MovieUrlInfo `json:"linkList"`
}

// MovieDetailVo 影片详情数据, 播放源合并版
type MovieDetailVo struct {
	MovieDetail
	List []PlayLinkVo `json:"list"`
}
