package shared

import (
	"fmt"
	"testing"

	"server/internal/infra/db"
	"server/internal/model"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestLoadSourceMidsByGlobalMids(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.MovieSourceMapping{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	rows := []model.MovieSourceMapping{
		{SourceId: "jy", SourceMid: 159488, GlobalMid: 1},
		{SourceId: "jy", SourceMid: 161309, GlobalMid: 13204},
		{SourceId: "other", SourceMid: 9, GlobalMid: 1},
	}
	if err := gdb.Create(&rows).Error; err != nil {
		t.Fatal(err)
	}

	got, err := LoadSourceMidsByGlobalMids("jy", []int64{1, 13204, 1, 0})
	if err != nil {
		t.Fatal(err)
	}
	if got[1] != 159488 || got[13204] != 161309 {
		t.Fatalf("source mids = %#v", got)
	}
	if _, ok := got[0]; ok {
		t.Fatal("zero mid should be ignored")
	}
}
