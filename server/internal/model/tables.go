package model

// 统一管理所有数据表名常量
// 仅用于 db.Mdb.Exec / db.Mdb.Raw 等原生 SQL 操作，杜绝魔术字符串
const (
	TableUser               = "user"
	TableFilmIndex          = "film_index"
	TableFilmSourcePlaylist = "film_source_playlists"
	TableMoviePoster        = "movie_poster"
	TableMovieMatchKey      = "movie_match_key"
	TableMovieSourceMapping = "movie_source_mapping"
	TableCollectSourceStats = "collect_source_stats"
	TableCategory           = "film_category"
	TableCategoryMapping    = "category_mappings"
	TableSourceCategory     = "source_categories"
	TableMappingRule        = "mapping_rules"
	TableSearchTag          = "search_tag_item"
	TableCrontabRecord      = "crontab_record"
	TableCronSourceRel      = "cron_source_rel"
	TableSiteConfig         = "site_config_record"
	TableBanners            = "banners_record"
	TableFileInfo           = "files"
	TableNotifyConfig       = "notify_config"
	TableAccessDailyStats   = "access_daily_stats"
	TableAccessDailyTop     = "access_daily_top"
	TableFailureRecord      = "failure_records"
	TableSchemaMigration    = "schema_migrations"
	TableTMDBConfig         = "tmdb_config"
	TableBannerConfig       = "banner_config"
	TableProxyConfig        = "proxy_config"
	TableFilmSource         = "film_sources"
)

// FilmHasPlaySourceSQL 判断 film_index 当前行是否有指定采集源的播放线路。
// 探测走 film_source_playlists 主键 (mid, source_id, line_kind)。
// 占位符依次是 source_id、line_kind。不要改成 DISTINCT mid 子查询，那会先扫完整站线路。
func FilmHasPlaySourceSQL() string {
	return "EXISTS (SELECT 1 FROM " + TableFilmSourcePlaylist + " AS p WHERE p.mid = " + TableFilmIndex + ".mid AND p.source_id = ? AND p.line_kind = ?)"
}

// AllModels 系统所有持久化数据模型（单一事实来源，供 AutoMigrate 按当前模型建表）
var AllModels = []any{
	&User{},
	&FilmIndex{},
	&FileInfo{},
	&FilmSourcePlaylist{},
	&Category{},
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
	&SchemaMigration{},
	&TMDBConfigRecord{},
	&BannerConfigRecord{},
	&ProxyConfigRecord{},
}
