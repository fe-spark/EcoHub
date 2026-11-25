package admin

import (
	"os"
	"path/filepath"
	"testing"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/testutil"

	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestFileService_SingleFileUpload_CleansDiskOnDBFailure(t *testing.T) {
	// 打开一个未迁移 file_infos 表的内存库，触发 SaveGallery 写入失败
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })

	tmpFile := filepath.Join(t.TempDir(), "upload_fail.png")
	if err := os.WriteFile(tmpFile, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}

	if _, err := FileSvc.SingleFileUpload(tmpFile, "test", 1); err == nil {
		t.Fatal("expected error when SaveGallery fails")
	}
	if _, statErr := os.Stat(tmpFile); !os.IsNotExist(statErr) {
		t.Fatalf("expected disk file cleaned on DB failure, statErr=%v", statErr)
	}
}

func TestFileService_RemoveFiles_DiskBeforeDB(t *testing.T) {
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FileInfo{}); err != nil {
		t.Fatal(err)
	}
	prevDB := db.Mdb
	prevDir := config.FilmPictureUploadDir
	dir := t.TempDir()
	db.Mdb = gdb
	config.FilmPictureUploadDir = dir
	t.Cleanup(func() {
		db.Mdb = prevDB
		config.FilmPictureUploadDir = prevDir
	})

	okPath := filepath.Join(dir, "ok.png")
	if err := os.WriteFile(okPath, []byte("png"), 0o600); err != nil {
		t.Fatal(err)
	}
	okRow := model.FileInfo{Link: config.FilmPictureAccess + "ok.png", FileType: "png", Name: "ok", Uid: 1}
	if err := gdb.Create(&okRow).Error; err != nil {
		t.Fatal(err)
	}
	if err := FileSvc.RemoveFiles([]uint{okRow.ID}, 1, true); err != nil {
		t.Fatal(err)
	}
	if _, statErr := os.Stat(okPath); !os.IsNotExist(statErr) {
		t.Fatalf("disk file should be gone, statErr=%v", statErr)
	}
	var gone model.FileInfo
	if err := gdb.Unscoped().First(&gone, okRow.ID).Error; err == nil {
		t.Fatal("db row should be deleted after disk remove succeeds")
	}

	stuck := filepath.Join(dir, "stuck.png")
	if err := os.Mkdir(stuck, 0o755); err != nil {
		t.Fatal(err)
	}
	if err := os.WriteFile(filepath.Join(stuck, "child"), []byte("x"), 0o600); err != nil {
		t.Fatal(err)
	}
	badRow := model.FileInfo{Link: config.FilmPictureAccess + "stuck.png", FileType: "png", Name: "stuck", Uid: 1}
	if err := gdb.Create(&badRow).Error; err != nil {
		t.Fatal(err)
	}
	if err := FileSvc.RemoveFiles([]uint{badRow.ID}, 1, true); err == nil {
		t.Fatal("expected error when disk remove fails")
	}
	var kept model.FileInfo
	if err := gdb.Unscoped().First(&kept, badRow.ID).Error; err != nil {
		t.Fatalf("db row should remain so delete can be retried: %v", err)
	}
}
