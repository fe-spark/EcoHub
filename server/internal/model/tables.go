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
	TableMovieMatchKey      = "movie_match_key"
	TableCollectSourceStats = "collect_source_stats"
	TableCategory           = "film_category"
	TableSourceCategory     = "source_categories"
	TableVirtualPicture     = "virtual_picture_queue"
	TableSearchTag          = "search_tag_item"
	TableFilmSource         = "film_sources"
	TableFailureRecord      = "failure_records"
	TableCrontabRecord      = "crontab_record"
	TableCronSourceRel      = "cron_source_rel"
	TableSiteConfig         = "site_config_record"
	TableBanners            = "banners_record"
	TableFileInfo           = "file_info"
)
