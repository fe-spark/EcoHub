package model

import (
	"time"

	"gorm.io/gorm"
)

// TMDBConfig TMDB 刮削配置
type TMDBConfig struct {
	Enabled     bool   `json:"enabled"`
	ApiKey      string `json:"apiKey"`
	Language    string `json:"language"`
	ImageDomain string `json:"imageDomain"`
}

// TMDBConfigRecord TMDB 配置持久化模型 (MySQL)
type TMDBConfigRecord struct {
	gorm.Model
	Payload string `gorm:"type:text"`
}

func (TMDBConfigRecord) TableName() string {
	return TableTMDBConfig
}

// TMDBCandidate TMDB 搜索候选条目
type TMDBCandidate struct {
	ID            int64   `json:"id"`
	MediaType     string  `json:"mediaType"` // "movie" | "tv"
	Title         string  `json:"title"`
	OriginalTitle string  `json:"originalTitle"`
	ReleaseDate   string  `json:"releaseDate"`
	Year          string  `json:"year"`
	Poster        string  `json:"poster"`
	Backdrop      string  `json:"backdrop"`
	VoteAverage   float64 `json:"voteAverage"`
	Overview      string  `json:"overview"`
}

// TMDBDetail TMDB 完整影视详情信息
type TMDBDetail struct {
	ID            int64    `json:"id"`
	MediaType     string   `json:"mediaType"`
	Title         string   `json:"title"`
	OriginalTitle string   `json:"originalTitle"`
	ReleaseDate   string   `json:"releaseDate"`
	Year          string   `json:"year"`
	Poster        string   `json:"poster"`
	Backdrop      string   `json:"backdrop"`
	VoteAverage   float64  `json:"voteAverage"`
	VoteScore     string   `json:"voteScore"`
	Overview      string   `json:"overview"`
	Tagline       string   `json:"tagline"`
	Genres        []string `json:"genres"`
	Directors     []string `json:"directors"`
	Actors        []string `json:"actors"`
}

// TMDBApplyReq 刮削应用请求
type TMDBApplyReq struct {
	Mid       int64    `json:"mid"`
	TmdbID    int64    `json:"tmdbId"`
	MediaType string   `json:"mediaType"` // "movie" | "tv"
	Fields    []string `json:"fields"`    // 需覆盖的字段列表: "poster", "backdrop", "overview", "actor", "director", "year", "score", "tag"
}

// TMDBRevertReq 刮削还原请求
type TMDBRevertReq struct {
	Mid int64 `json:"mid"`
}

// FilmTMDBScrape TMDB 刮削元数据持久化模型 (MySQL)
// 物理分层：采集站原始数据留在 film_index，TMDB 刮削数据保存在 film_tmdb_scrapes
type FilmTMDBScrape struct {
	CreatedAt     time.Time `json:"createdAt"`
	UpdatedAt     time.Time `json:"updatedAt"`
	Mid           int64     `gorm:"primaryKey;autoIncrement:false;index:idx_tmdb_scrape_mid" json:"mid"` // 关联的影片 ID
	TmdbID        int64     `gorm:"index:idx_tmdb_scrape_tmdb_id;not null" json:"tmdbId"`                // TMDB 词条 ID
	MediaType     string    `gorm:"size:16;not null" json:"mediaType"`                                   // "movie" | "tv"
	Title         string    `gorm:"size:255" json:"title"`                                               // TMDB 标题
	OriginalTitle string    `gorm:"size:255" json:"originalTitle"`                                       // 原始片名/外文名
	ReleaseDate   string    `gorm:"size:64" json:"releaseDate"`                                          // 上映日期 (YYYY-MM-DD)
	Year          int64     `gorm:"index" json:"year"`                                                   // 年份
	Poster        string    `gorm:"type:text" json:"poster"`                                             // 竖版海报图
	Backdrop      string    `gorm:"type:text" json:"backdrop"`                                           // 横版背景幻灯图
	VoteAverage   float64   `json:"voteAverage"`                                                         // TMDB 评分
	VoteScore     string    `gorm:"size:16" json:"voteScore"`                                            // 评分字符串表示
	Overview      string    `gorm:"type:longtext" json:"overview"`                                       // 剧情简介
	Tagline       string    `gorm:"type:text" json:"tagline"`                                            // 宣传标语
	Genres        string    `gorm:"type:text" json:"genres"`                                             // 分类标签 (逗号分隔)
	Directors     string    `gorm:"type:text" json:"directors"`                                          // 导演列表 (斜杠分隔)
	Actors        string    `gorm:"type:text" json:"actors"`                                             // 演员列表 (斜杠分隔)
	AppliedFields string    `gorm:"type:text" json:"appliedFields"`                                      // 用户选择应用的字段 (逗号分隔)
	RawJSON       string    `gorm:"type:longtext" json:"rawJson"`                                        // TMDB 原始详情完整 JSON
	Status        int       `gorm:"default:1;index" json:"status"`                                       // 状态: 1=已启用, 0=已禁用/已还原
}

func (FilmTMDBScrape) TableName() string {
	return TableFilmTMDBScrape
}
