package migration

import "gorm.io/gorm"

// unusedFilmIndexes 已不再用于 SQL 过滤或造成严重写放大的单列及超宽复合索引。
var unusedFilmIndexes = []string{
	"idx_film_index_db_id",
	"idx_film_index_original_category",
	"idx_film_index_area",
	"idx_film_index_language",
	"idx_film_index_year",
	"idx_film_index_collect_stamp",
	"idx_film_index_category_version",
	"idx_film_index_rule_version",
	"idx_filter_score",
	"idx_filter_update",
	"idx_filter_hits",
}

func migrateFilmIndexPruneUnused(gdb *gorm.DB) error {
	return dropFilmIndexes(gdb, unusedFilmIndexes)
}
