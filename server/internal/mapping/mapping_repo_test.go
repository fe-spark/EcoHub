package mapping

import (
	"testing"

	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestEnsureMappingRuleIndexesAndReset(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	prevDB := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prevDB })

	if err := EnsureMappingRuleIndexes(); err != nil {
		t.Fatalf("EnsureMappingRuleIndexes failed on SQLite: %v", err)
	}
	gdb.Create(&model.MappingRule{Group: "Area", Raw: "港台", Target: "中国香港"})
	var ruleCount int64
	gdb.Model(&model.MappingRule{}).Count(&ruleCount)
	if ruleCount != 1 {
		t.Fatalf("expected 1 rule before reset, got %d", ruleCount)
	}
	if err := ResetMappingRules(); err != nil {
		t.Fatalf("ResetMappingRules failed on SQLite: %v", err)
	}
	gdb.Model(&model.MappingRule{}).Count(&ruleCount)
	if ruleCount != 0 {
		t.Fatalf("expected 0 rules after ResetMappingRules, got %d", ruleCount)
	}
}
