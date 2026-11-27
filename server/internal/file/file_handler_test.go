package file

import (
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
	"net/http"
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/testutil"
	"server/internal/utils"
	"testing"
)

func TestFileHandler_DelFile_Validation(t *testing.T) {
	c, w := testutil.NewJSONContext(http.MethodPost, "/api/manage/file/delete", "invalid-json")
	FileHd.DelFile(c)
	resp := testutil.DecodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on invalid JSON, got %d", resp.Code)
	}
	c, w = testutil.NewJSONContext(http.MethodPost, "/api/manage/file/delete", `{"ids":[]}`)
	FileHd.DelFile(c)
	resp = testutil.DecodeResponse(t, w)
	if resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on empty ids, got %d", resp.Code)
	}
}

func TestFileHandler_OwnershipEnforcement(t *testing.T) {
	setupManageConfigDB(t)
	file := model.FileInfo{
		Model:    gorm.Model{ID: 99},
		Uid:      10001,
		Name:     "UserFile",
		FileType: "jpg",
		Link:     config.FilmPictureAccess + "user_file.jpg",
	}
	db.Mdb.Create(&file)

	c, w := testutil.NewJSONContext(http.MethodPost, "/api/manage/file/rename", `{"id":99,"name":"HackedName"}`)
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 10002, Role: model.UserRoleNormal})
	FileHd.RenameFile(c)
	if resp := testutil.DecodeResponse(t, w); resp.Code != dto.FAILED {
		t.Fatalf("expected FAILED on non-owner rename, got %d", resp.Code)
	}

	c2, w2 := testutil.NewJSONContext(http.MethodPost, "/api/manage/file/delete", `{"ids":[99]}`)
	c2.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 10002, Role: model.UserRoleNormal})
	FileHd.DelFile(c2)
	if resp2 := testutil.DecodeResponse(t, w2); resp2.Code != dto.FAILED {
		t.Fatalf("expected FAILED on non-owner delete, got %d", resp2.Code)
	}

	c3, w3 := testutil.NewJSONContext(http.MethodPost, "/api/manage/file/rename", `{"id":99,"name":"UpdatedName"}`)
	c3.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 10001, Role: model.UserRoleNormal})
	FileHd.RenameFile(c3)
	if resp3 := testutil.DecodeResponse(t, w3); resp3.Code != dto.SUCCESS {
		t.Fatalf("expected SUCCESS on owner rename, got %d msg=%s", resp3.Code, resp3.Msg)
	}

	c4, w4 := testutil.NewJSONContext(http.MethodPost, "/api/manage/file/delete", `{"ids":[99]}`)
	c4.Set(config.AuthUserClaims, &utils.UserClaims{UserID: config.UserIdInitialVal, Role: model.UserRoleAdmin})
	FileHd.DelFile(c4)
	if resp4 := testutil.DecodeResponse(t, w4); resp4.Code != dto.SUCCESS {
		t.Fatalf("expected SUCCESS on admin delete, got %d msg=%s", resp4.Code, resp4.Msg)
	}

	fileOwn := model.FileInfo{Model: gorm.Model{ID: 101}, Uid: 10001, Name: "OwnFile"}
	fileOther := model.FileInfo{Model: gorm.Model{ID: 102}, Uid: 10002, Name: "OtherFile"}
	db.Mdb.Create(&fileOwn)
	db.Mdb.Create(&fileOther)

	cBatch, wBatch := testutil.NewJSONContext(http.MethodPost, "/api/manage/file/delete", `{"ids":[101, 102]}`)
	cBatch.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 10001, Role: model.UserRoleNormal})
	FileHd.DelFile(cBatch)
	if respBatch := testutil.DecodeResponse(t, wBatch); respBatch.Code != dto.FAILED {
		t.Fatalf("expected FAILED on batch with unauthorized item, got %d", respBatch.Code)
	}
	var checkOwn model.FileInfo
	if err := db.Mdb.First(&checkOwn, 101).Error; err != nil {
		t.Fatalf("expected file 101 to remain intact after batch failure, got err: %v", err)
	}
}

func setupManageConfigDB(t *testing.T) {
	t.Helper()
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.FileInfo{}); err != nil {
		t.Fatal(err)
	}
	prev := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prev })
}
