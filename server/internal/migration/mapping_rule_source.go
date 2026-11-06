package migration

import (
	"fmt"
	"strings"

	"server/internal/model"

	"gorm.io/gorm"
)

func migrateMappingRuleSource(gdb *gorm.DB) error {
	if gdb == nil {
		return nil
	}
	if err := gdb.AutoMigrate(&model.MappingRule{}); err != nil {
		return err
	}

	table := model.MappingRule{}.TableName()
	isSQLite := gdb.Dialector != nil && gdb.Dialector.Name() == "sqlite"
	for _, name := range []string{"uidx_group_raw", "uidx_group_raw_match_type"} {
		var dropSQL string
		if isSQLite {
			dropSQL = fmt.Sprintf("DROP INDEX IF EXISTS %s", name)
		} else {
			dropSQL = fmt.Sprintf("ALTER TABLE %s DROP INDEX %s", table, name)
		}
		if err := gdb.Exec(dropSQL).Error; err != nil && !ignorableMappingIndexError(err) {
			return err
		}
	}

	var primary model.FilmSource
	if err := gdb.Where("state = ?", true).Order("sort ASC, created_at ASC, id ASC").First(&primary).Error; err == nil && strings.TrimSpace(primary.Id) != "" {
		if err := gdb.Model(&model.MappingRule{}).
			Where("`group` IN ? AND (source_id IS NULL OR source_id = '')", []string{"CategoryRoot", "CategorySub"}).
			Update("source_id", primary.Id).Error; err != nil {
			return err
		}
	}

	var createSQL string
	if isSQLite {
		createSQL = fmt.Sprintf("CREATE UNIQUE INDEX IF NOT EXISTS uidx_source_group_raw_match_type ON %s (source_id, `group`, raw, match_type)", table)
	} else {
		createSQL = fmt.Sprintf("CREATE UNIQUE INDEX uidx_source_group_raw_match_type ON %s (source_id, `group`, raw, match_type)", table)
	}
	if err := gdb.Exec(createSQL).Error; err != nil && !ignorableMappingIndexError(err) {
		return err
	}
	return nil
}

func ignorableMappingIndexError(err error) bool {
	if err == nil {
		return false
	}
	message := strings.ToLower(err.Error())
	return strings.Contains(message, "check that column/key exists") ||
		strings.Contains(message, "duplicate key name") ||
		strings.Contains(message, "already exists") ||
		strings.Contains(message, "no such index")
}
