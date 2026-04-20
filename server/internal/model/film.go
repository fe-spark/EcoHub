package model

import (
	"context"
	"fmt"

	"server/internal/config"
	"server/internal/infra/db"
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
	Id       int64  `json:"id"`       // 影片Id
	Cid      int64  `json:"cid"`      // 分类ID
	Pid      int64  `json:"pid"`      // 一级分类ID
	Name     string `json:"name"`     // 片名
	SubTitle string `json:"subTitle"` // 子标题
	CName    string `json:"cName"`    // 分类名称
	State    string `json:"state"`    // 影片状态 正片|预告...
	Picture  string `json:"picture"`  // 竖版封面图
	PictureSlide string `json:"pictureSlide"` // 横版幻灯图
	Actor    string `json:"actor"`    // 主演
	Director string `json:"director"` // 导演
	Blurb    string `json:"blurb"`    // 简介, 不完整
	Remarks  string `json:"remarks"`  // 更新情况
	Area     string `json:"area"`     // 地区
	Year     string `json:"year"`     // 年份
}

// MovieUrlInfo 影视资源url信息
type MovieUrlInfo struct {
	Episode string `json:"episode"` // 集数
	Link    string `json:"link"`    // 播放地址
}

// MovieDetail 影片详情信息
type MovieDetail struct {
	Id              int64               `json:"id"`           // 影片Id
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

// MoviePlaySource 多站播放源信息
type MoviePlaySource struct {
	SiteName string         `json:"siteName"` // 站点名称
	PlayList []MovieUrlInfo `json:"playList"` // 播放列表
}

// MovieDetailInfo 影片详情持久化模型 (MySQL)
type MovieDetailInfo struct {
	gorm.Model
	Mid      int64  `gorm:"uniqueIndex"`
	SourceId string `gorm:"index"`         // 预留：标识主站来源
	Content  string `gorm:"type:longtext"` // 存储序列化后的完整 MovieDetail JSON
}

// MovieSourceMapping 影片来源映射表 (核心：解决不同站 ID 不一致问题)
type MovieSourceMapping struct {
	gorm.Model
	SourceId  string `gorm:"uniqueIndex:uidx_source_mid"` // 来源站点ID
	SourceMid int64  `gorm:"uniqueIndex:uidx_source_mid"` // 来源站点原始ID
	GlobalMid int64  `gorm:"index"`                       // 映射到的全局统一ID
}

// MoviePlaylist 多源播放列表持久化模型 (MySQL)
type MoviePlaylist struct {
	gorm.Model
	SourceId   string `gorm:"uniqueIndex:uidx_source_key_group"`
	MovieKey   string `gorm:"uniqueIndex:uidx_source_key_group"` // 播放列表匹配键：优先豆瓣ID，其次规范化片名
	GroupIndex int    `gorm:"uniqueIndex:uidx_source_key_group"` // 播放组顺序
	GroupName  string `gorm:"type:varchar(255)"`                 // 原始播放组名称
	Content    string `gorm:"type:longtext"`                     // 单个播放组的播放列表 JSON
}

// MovieMatchKey 主站影片匹配键索引。
// 主站详情会写入多个匹配键：优先豆瓣ID，同时保留规范化片名，附属站播放数据只通过该索引关联。
type MovieMatchKey struct {
	gorm.Model
	Mid      int64  `gorm:"uniqueIndex:uidx_mid_match;index:idx_match_key"`
	MatchKey string `gorm:"size:64;uniqueIndex:uidx_mid_match;index:idx_match_key"`
}

func (MovieMatchKey) TableName() string {
	return TableMovieMatchKey
}

// SearchInfo 存储用于检索的信息
type SearchInfo struct {
	gorm.Model
	Mid               int64   `json:"mid" gorm:"uniqueIndex:idx_mid"`                                                                                                                                                                    // 影片ID (全局唯一)
	ContentKey        string  `json:"contentKey" gorm:"uniqueIndex:idx_content"`                                                                                                                                                         // 主站内容指纹：优先豆瓣ID，其次规范化片名
	SourceId          string  `json:"sourceId" gorm:"index"`                                                                                                                                                                             // 来源站点ID
	Cid               int64   `json:"cid" gorm:"index;index:idx_pid_update;index:idx_cid_update;index:idx_pid_hits;index:idx_cid_hits;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits"`                             // 分类ID
	Pid               int64   `json:"pid" gorm:"index;index:idx_pid_update;index:idx_cid_update;index:idx_pid_hits;index:idx_cid_hits;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits;constraint:OnDelete:CASCADE"` // 上级分类ID
	RootCategoryKey   string  `json:"rootCategoryKey" gorm:"size:128;index;index:idx_root_key_update;index:idx_root_key_hits;index:idx_root_key_latest;index:idx_filter_root_score;index:idx_filter_root_update;index:idx_filter_root_hits"`
	CategoryKey       string  `json:"categoryKey" gorm:"size:128;index;index:idx_category_key_update;index:idx_category_key_hits;index:idx_category_key_latest"`
	SeriesKey         string  `json:"seriesKey" gorm:"size:128;index"`                                                            // 系列标识，用于相关推荐召回与排序
	Name              string  `json:"name"`                                                                                       // 片名
	SubTitle          string  `json:"subTitle"`                                                                                   // 影片子标题
	CName             string  `json:"cName"`                                                                                      // 分类名称
	ClassTag          string  `json:"classTag"`                                                                                   // 类型标签
	Area              string  `json:"area" gorm:"index;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits"`     // 地区
	Language          string  `json:"language" gorm:"index;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits"` // 语言
	Year              int64   `json:"year" gorm:"index;index:idx_filter_score;index:idx_filter_update;index:idx_filter_hits"`     // 年份
	Initial           string  `json:"initial"`                                                                                    // 首字母
	Score             float64 `json:"score" gorm:"index;index:idx_filter_score"`                                                  // 评分
	UpdateStamp       int64   `json:"updateStamp" gorm:"index;index:idx_pid_update;index:idx_cid_update;index:idx_filter_update"` // 更新时间
	LatestSourceStamp int64   `json:"latestSourceStamp" gorm:"index;index:idx_pid_latest;index:idx_cid_latest"`                   // 聚合更新时间(主站/附属站取最新)
	Hits              int64   `json:"hits" gorm:"index;index:idx_pid_hits;index:idx_cid_hits;index:idx_filter_hits"`              // 热度排行
	State             string  `json:"state"`                                                                                      // 状态 正片|预告
	Remarks           string  `json:"remarks"`                                                                                    // 完结 | 更新至x集
	PlayFromSummary   string  `json:"playFromSummary"`                                                                            // 播放源摘要，供列表接口直出
	DbId              int64   `json:"dbId" gorm:"index"`                                                                          // 豆瓣ID (用于精准去重)
	ReleaseStamp      int64   `json:"releaseStamp" gorm:"index"`                                                                  // 上映时间 时间戳
	Picture           string  `json:"picture"`                                                                                    // 竖版封面图
	PictureSlide      string  `json:"pictureSlide" gorm:"size:512"`                                                             // 横版幻灯图
	Actor             string  `json:"actor"`                                                                                      // 主演
	Director          string  `json:"director"`                                                                                   // 导演
	Blurb             string  `json:"blurb"`                                                                                      // 简介, 不完整
}

// AfterSave GORM 钩子：在数据保存/更新后自动清理缓存，确保首页数据实时性
func (s *SearchInfo) AfterSave(tx *gorm.DB) (err error) {
	ctx := context.Background()

	// 1. 清理首页全量缓存
	iter := db.Rdb.Scan(ctx, 0, config.IndexPageCacheKey+"*", 100).Iterator()
	for iter.Next(ctx) {
		db.Rdb.Del(ctx, iter.Val())
	}

	// 2. 清理 TVBox 列表第一页缓存 (由于涉及多种 Sort/Pid/Limit 组合，使用模糊匹配清理)
	// 注意：此处使用 Keys 操作在数据量极大时可能有性能影响，但考虑到采集频率可控且主要是首页缓存，是合理的
	pattern := config.TVBoxList + ":*"
	iter = db.Rdb.Scan(ctx, 0, pattern, 100).Iterator()
	for iter.Next(ctx) {
		db.Rdb.Del(ctx, iter.Val())
	}

	// 3. 清理搜索标签缓存 (SearchTags:*), 确保新入库/更新的影片能实时在筛选菜单中体现
	// 清理当前分类的复合标签缓存 (格式：Search:Tags:{pid}:*)
	if s.Pid > 0 {
		tagPattern := fmt.Sprintf("%s:%d:*", config.SearchTags, s.Pid)
		iter := db.Rdb.Scan(ctx, 0, tagPattern, 100).Iterator()
		for iter.Next(ctx) {
			db.Rdb.Del(ctx, iter.Val())
		}
		// 兼容基础版 key: Search:Tags:{pid}
		db.Rdb.Del(ctx, fmt.Sprintf("%s:%d", config.SearchTags, s.Pid))
	}

	return
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
	Pid      int64  `json:"pid"`
	Cid      int64  `json:"cid"`
	Plot     string `json:"plot"`
	Area     string `json:"area"`
	Language string `json:"language"`
	Year     string `json:"year"`
	Sort     string `json:"sort"`
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
	Name     string         `json:"name"`
	LinkList []MovieUrlInfo `json:"linkList"`
}

// MovieDetailVo 影片详情数据, 播放源合并版
type MovieDetailVo struct {
	MovieDetail
	List []PlayLinkVo `json:"list"`
}
