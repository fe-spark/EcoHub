package model

// 统一管理所有数据表名常量
// 仅用于 db.Mdb.Exec / db.Mdb.Raw 等原生 SQL 操作，杜绝魔术字符串
const (
	TableUser               = "user"
	TableFilmIndex          = "film_index"
	TableFilmListSnapshot   = "film_list_snapshot"
	TableMovieDetail        = "movie_detail_info"
	TableSlaveMoviePlaylist = "slave_movie_playlists"
	TableMoviePoster        = "movie_poster"
	TableMovieMatchKey      = "movie_match_key"
	TableMovieSourceMapping = "movie_source_mapping"
	TableCollectSourceStats = "collect_source_stats"
	TableCategory           = "film_category"
	TableCategoryMapping    = "category_mappings"
	TableSourceCategory     = "source_categories"
	TableMappingRule        = "mapping_rules"
	TableSearchTag          = "search_tag_item"
	TableFilmSource         = "film_sources"
	TableCrontabRecord      = "crontab_record"
	TableCronSourceRel      = "cron_source_rel"
	TableSiteConfig         = "site_config_record"
	TableBanners            = "banners_record"
	TableFileInfo           = "files"
	TableNotifyConfig       = "notify_config"
	TableAccessDailyStats   = "access_daily_stats"
	TableAccessDailyTop     = "access_daily_top"
	TableFailureRecord      = "failure_records"
)

// AllModels 系统所有持久化数据模型（单一事实来源，供 AutoMigrate 全局幂等初始化与升级）
var AllModels = []any{
	&User{},
	&FilmIndex{},
	&FilmListSnapshot{},
	&FileInfo{},
	&MovieDetailInfo{},
	&Category{},
	&SlaveMoviePlaylist{},
	&MoviePoster{},
	&MovieMatchKey{},
	&FilmSource{},
	&CollectSourceStats{},
	&SearchTagItem{},
	&CrontabRecord{},
	&SiteConfigRecord{},
	&MovieSourceMapping{},
	&Banner{},
	&CronSourceRel{},
	&MappingRule{},
	&CategoryMapping{},
	&SourceCategory{},
	&NotifyConfigRecord{},
	&AccessDailyStats{},
	&AccessDailyTop{},
	&FailureRecord{},
}

