package user

import (
	"bytes"
	"encoding/json"
	"net/http"
	"net/http/httptest"
	"strings"
	"testing"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/testutil"
	"server/internal/utils"

	"github.com/alicebob/miniredis/v2"
	"github.com/gin-gonic/gin"
	"github.com/glebarez/sqlite"
	"github.com/redis/go-redis/v9"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUserHandler_UserProfile(t *testing.T) {
	gin.SetMode(gin.TestMode)
	gdb, err := gorm.Open(sqlite.Open(testutil.MemDSN(t)), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	if err != nil {
		t.Fatal(err)
	}
	if err := gdb.AutoMigrate(&model.User{}); err != nil {
		t.Fatal(err)
	}
	prevDB := db.Mdb
	db.Mdb = gdb
	t.Cleanup(func() { db.Mdb = prevDB })

	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	origRdb := db.Rdb
	db.Rdb = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	defer func() {
		_ = db.Rdb.Close()
		db.Rdb = origRdb
	}()
	u := model.User{
		Model:    gorm.Model{ID: 10005},
		UserName: "testuser",
		NickName: "OldNick",
		Role:     model.UserRoleNormal,
		Status:   0,
	}
	db.Mdb.Create(&u)

	decode := func(w *httptest.ResponseRecorder) dto.Response {
		var r dto.Response
		if err := json.Unmarshal(w.Body.Bytes(), &r); err != nil {
			t.Fatalf("decode response: %v body=%s", err, w.Body.String())
		}
		return r
	}

	newNick := "NewNick"
	newPwd := "newpassword123"
	payload, _ := json.Marshal(model.UserUpdatePayload{NickName: &newNick, Password: &newPwd})
	w := httptest.NewRecorder()
	c, _ := gin.CreateTestContext(w)
	c.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 10005, UserName: "testuser", Role: model.UserRoleNormal})
	c.Request = httptest.NewRequest(http.MethodPost, "/api/manage/user/profile", bytes.NewReader(payload))
	c.Request.Header.Set("Content-Type", "application/json")
	UserHd.UserProfile(c)
	resp := decode(w)
	if resp.Code != dto.SUCCESS {
		t.Fatalf("expected SUCCESS, got code=%d msg=%s", resp.Code, resp.Msg)
	}
	var updated model.User
	db.Mdb.First(&updated, 10005)
	if updated.NickName != "NewNick" {
		t.Fatalf("expected NewNick, got %s", updated.NickName)
	}

	newerPwd := "newerpwd456"
	payloadNoOld, _ := json.Marshal(model.UserUpdatePayload{Password: &newerPwd})
	wNoOld := httptest.NewRecorder()
	cNoOld, _ := gin.CreateTestContext(wNoOld)
	cNoOld.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 10005, Role: model.UserRoleNormal})
	cNoOld.Request = httptest.NewRequest(http.MethodPost, "/api/manage/user/profile", bytes.NewReader(payloadNoOld))
	cNoOld.Request.Header.Set("Content-Type", "application/json")
	UserHd.UserProfile(cNoOld)
	respNoOld := decode(wNoOld)
	if respNoOld.Code != dto.FAILED || !strings.Contains(respNoOld.Msg, "必须提供原密码") {
		t.Fatalf("expected failure on missing old password, got code=%d msg=%s", respNoOld.Code, respNoOld.Msg)
	}

	wrongOld := "wrongpassword"
	payloadWrongOld, _ := json.Marshal(model.UserUpdatePayload{Password: &newerPwd, OldPassword: &wrongOld})
	wWrong := httptest.NewRecorder()
	cWrong, _ := gin.CreateTestContext(wWrong)
	cWrong.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 10005, Role: model.UserRoleNormal})
	cWrong.Request = httptest.NewRequest(http.MethodPost, "/api/manage/user/profile", bytes.NewReader(payloadWrongOld))
	cWrong.Request.Header.Set("Content-Type", "application/json")
	UserHd.UserProfile(cWrong)
	respWrong := decode(wWrong)
	if respWrong.Code != dto.FAILED || !strings.Contains(respWrong.Msg, "原密码校验失败") {
		t.Fatalf("expected failure on wrong old password, got code=%d msg=%s", respWrong.Code, respWrong.Msg)
	}

	correctOld := "newpassword123"
	payloadCorrect, _ := json.Marshal(model.UserUpdatePayload{Password: &newerPwd, OldPassword: &correctOld})
	wCorrect := httptest.NewRecorder()
	cCorrect, _ := gin.CreateTestContext(wCorrect)
	cCorrect.Set(config.AuthUserClaims, &utils.UserClaims{UserID: 10005, Role: model.UserRoleNormal})
	cCorrect.Request = httptest.NewRequest(http.MethodPost, "/api/manage/user/profile", bytes.NewReader(payloadCorrect))
	cCorrect.Request.Header.Set("Content-Type", "application/json")
	UserHd.UserProfile(cCorrect)
	respCorrect := decode(wCorrect)
	if respCorrect.Code != dto.SUCCESS {
		t.Fatalf("expected success on correct old password, got code=%d msg=%s", respCorrect.Code, respCorrect.Msg)
	}
}
