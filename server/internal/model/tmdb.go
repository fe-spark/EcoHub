package model

import "gorm.io/gorm"

// TMDBConfig TMDB 刮削配置
type TMDBConfig struct {
	Enabled     bool   `json:"enabled"`
	ApiKey      string `json:"apiKey"`
	Proxy       string `json:"proxy"`
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
