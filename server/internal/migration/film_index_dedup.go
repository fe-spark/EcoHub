package migration

import (
	"fmt"

	"server/internal/model"

	"gorm.io/gorm"
)

// redundantFilmIndexes 历史版本 GORM tag 残留或完全重复的冗余索引。
var redundantFilmIndexes = []string{
	"idx_root_key_update",
	"idx_root_key_hits",
	"idx_filter_root_score",
	"idx_filter_root_update",
	"idx_filter_root_hits",
	"idx_film_index_root_category_key",
	"idx_category_key_update",
	"idx_category_key_hits",
	"idx_category_key_latest",
	"idx_film_index_category_key",
	"idx_film_index_update_mid",
	"idx_pid_update",
	"idx_pid_hits",
	"idx_film_index_first_source_id",
}

func migrateFilmIndexDedup(gdb *gorm.DB) error {
	if gdb == nil || !gdb.Migrator().HasTable(&model.FilmIndex{}) {
		return nil
	}
	isSQLite := gdb.Dialector != nil && gdb.Dialector.Name() == "sqlite"
	for _, name := range redundantFilmIndexes {
		if !gdb.Migrator().HasIndex(&model.FilmIndex{}, name) {
			continue
		}
		if isSQLite {
			if err := gdb.Exec(fmt.Sprintf("DROP INDEX IF EXISTS %s", name)).Error; err != nil {
				return err
			}
			continue
		}
		if err := gdb.Exec(fmt.Sprintf("ALTER TABLE %s DROP INDEX %s, ALGORITHM=INPLACE, LOCK=NONE", model.TableFilmIndex, name)).Error; err != nil && !ignorableMappingIndexError(err) {
			return err
		}
	}
	return nil
}
