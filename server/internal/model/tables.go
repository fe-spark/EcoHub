package model

// 统一管理所有数据表名常量
// 仅用于 db.Mdb.Exec / db.Mdb.Raw 等原生 SQL 操作，杜绝魔术字符串
const (
	TableUser               = "user"
	TableFilmIndex          = "film_index"
	TableFilmListSnapshot   = "film_list_snapshot"
	TableFilterOption       = "film_filter_option_snapshot"
	TableFilterIndex        = "film_filter_index_snapshot"
	TableMovieDetail        = "movie_detail_info"
	TableMoviePlaylist      = "movie_playlist"
	TableMoviePoster        = "movie_poster"
	TableMovieMatchKey      = "movie_match_key"
	TableMovieSourceMapping = "movie_source_mapping"
	TableCollectSourceStats = "collect_source_stats"
	TableCategory           = "film_category"
	TableCategoryMapping    = "category_mappings"
	TableSourceCategory     = "source_categories"
	TableVirtualPicture     = "virtual_picture_queue"
	TableSearchTag          = "search_tag_item"
	TableFilmSource         = "film_sources"
	TableFailureRecord      = "failure_records"
	TableCrontabRecord      = "crontab_record"
	TableCronSourceRel      = "cron_source_rel"
	TableSiteConfig         = "site_config_record"
	TableBanners            = "banners_record"
	TableFileInfo           = "files"
	TableNotifyConfig       = "notify_config"
	TableNotifyChangeBatch  = "notify_change_batch"
	TableNotifyChangeMid    = "notify_change_mid"
	TableAccessDailyStats   = "access_daily_stats"
	TableAccessDailyTop     = "access_daily_top"
	TableApiAccessLog       = "api_access_logs"
)

// AllModels 系统所有持久化数据模型（单一事实来源，供 AutoMigrate 全局幂等初始化与升级）
var AllModels = []any{
	&User{},
	&FilmIndex{},
	&FilmListSnapshot{},
	&FilmFilterOptionSnapshot{},
	&FilmFilterIndexSnapshot{},
	&FileInfo{},
	&FailureRecord{},
	&MovieDetailInfo{},
	&Category{},
	&MoviePlaylist{},
	&MoviePoster{},
	&MovieMatchKey{},
	&VirtualPictureQueue{},
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
	&NotifyChangeBatch{},
	&NotifyChangeMid{},
	&AccessDailyStats{},
	&AccessDailyTop{},
	&ApiAccessLog{},
}
