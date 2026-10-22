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
	Writers       []string `json:"writers"`
	Area          string   `json:"area"`
	Language      string   `json:"language"`
}

// TMDBApplyReq 刮削应用请求
type TMDBApplyReq struct {
	Mid       int64    `json:"mid"`
	TmdbID    int64    `json:"tmdbId"`
	MediaType string   `json:"mediaType"` // "movie" | "tv"
	Fields    []string `json:"fields"`    // 需覆盖的字段列表: "poster", "backdrop", "overview", "actor", "director", "year", "score", "tag"
}

// TMDBScrapeReq 刮削请求（兼容单个与批量）
type TMDBScrapeReq struct {
	Ids []int64 `json:"ids"`
}

// FilmTMDBScrape 只记录影片对应的 TMDB 词条，正文在 film_index。
type FilmTMDBScrape struct {
	CreatedAt time.Time `json:"createdAt"`
	UpdatedAt time.Time `json:"updatedAt"`
	Mid       int64     `gorm:"primaryKey;autoIncrement:false" json:"mid"`
	TmdbID    int64     `gorm:"index;not null" json:"tmdbId"`
	MediaType string    `gorm:"size:16;not null" json:"mediaType"`
}

func (FilmTMDBScrape) TableName() string {
	return TableFilmTMDBScrape
}

// RawTMDBItem TMDB API 原始条目
type RawTMDBItem struct {
	ID            int64   `json:"id"`
	MediaType     string  `json:"media_type"`
	Title         string  `json:"title"`
	Name          string  `json:"name"`
	OriginalTitle string  `json:"original_title"`
	OriginalName  string  `json:"original_name"`
	ReleaseDate   string  `json:"release_date"`
	FirstAirDate  string  `json:"first_air_date"`
	PosterPath    string  `json:"poster_path"`
	BackdropPath  string  `json:"backdrop_path"`
	VoteAverage   float64 `json:"vote_average"`
	Overview      string  `json:"overview"`
}

type TMDBCreditPerson struct {
	Name string `json:"name"`
	Job  string `json:"job"`
}

type TMDBDetailRaw struct {
	RawTMDBItem
	Tagline             string `json:"tagline"`
	Genres              []struct{ Name string `json:"name"` } `json:"genres"`
	ProductionCountries []struct{ Name string `json:"name"` } `json:"production_countries"`
	OriginCountry       []string                              `json:"origin_country"`
	SpokenLanguages     []struct{ Name string `json:"name"` } `json:"spoken_languages"`
	Credits             struct {
		Cast []struct{ Name string `json:"name"` } `json:"cast"`
		Crew []TMDBCreditPerson                    `json:"crew"`
	} `json:"credits"`
}
