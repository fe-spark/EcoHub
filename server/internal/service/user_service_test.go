package service

import (
	"fmt"
	"strings"
	"testing"

	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/repository"
	"github.com/alicebob/miniredis/v2"
	"github.com/redis/go-redis/v9"
	"github.com/glebarez/sqlite"
	"gorm.io/gorm"
	"gorm.io/gorm/logger"
)

func TestUserService_VerifyUserPassword_EdgeCases(t *testing.T) {
	// 1. 非法 ID
	if UserSvc.VerifyUserPassword(0, "123456") {
		t.Errorf("id 为 0 时应返回 false")
	}

	// 2. 空密码
	if UserSvc.VerifyUserPassword(1, "") {
		t.Errorf("密码为空时应返回 false")
	}

	// 3. 数据库未就绪 (db.Mdb 为 nil)
	origMdb := db.Mdb
	db.Mdb = nil
	defer func() {
		db.Mdb = origMdb
	}()

	if UserSvc.VerifyUserPassword(1, "123456") {
		t.Errorf("db.Mdb 为 nil 时应返回 false")
	}
}

func setupUserServiceTestDB(t *testing.T) (*gorm.DB, *miniredis.Miniredis) {
	t.Helper()
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatalf("miniredis: %v", err)
	}
	rdb := redis.NewClient(&redis.Options{Addr: mr.Addr()})
	db.Rdb = rdb

	dsn := fmt.Sprintf("file:%s?mode=memory&cache=shared", t.Name())
	gdb, err := gorm.Open(sqlite.Open(dsn), &gorm.Config{
		Logger: logger.Default.LogMode(logger.Silent),
	})
	if err != nil {
		t.Fatalf("open sqlite: %v", err)
	}
	if err := gdb.AutoMigrate(&model.User{}); err != nil {
		t.Fatalf("migrate schema: %v", err)
	}
	db.Mdb = gdb
	return gdb, mr
}

func TestUserService_TokenLifecycleOnUpdateAndLogout(t *testing.T) {
	origMdb := db.Mdb
	origRdb := db.Rdb
	defer func() {
		db.Mdb = origMdb
		db.Rdb = origRdb
	}()

	gdb, mr := setupUserServiceTestDB(t)
	defer mr.Close()

	u := model.User{
		UserName: "test_admin",
		Password: "oldpassword",
		Salt:     "randomsalt",
		Status:   0,
		Role:     model.UserRoleAdmin,
	}
	if err := gdb.Create(&u).Error; err != nil {
		t.Fatalf("create user: %v", err)
	}

	// 1. 登录写入单点 Token
	if err := repository.SaveUserToken("initial_token", u.ID); err != nil {
		t.Fatalf("save token: %v", err)
	}
	if tok, err := repository.GetUserToken(u.ID); err != nil || tok != "initial_token" {
		t.Fatalf("expected initial_token, got %q err=%v", tok, err)
	}

	// 2. 普通修改（仅更新昵称），Token 应该保持有效
	newNick := "NewNick"
	err := UserSvc.UpdateUser(u.ID, u.Role, model.UserUpdatePayload{
		ID:       u.ID,
		NickName: &newNick,
	})
	if err != nil {
		t.Fatalf("update nick: %v", err)
	}
	if tok, err := repository.GetUserToken(u.ID); err != nil || tok != "initial_token" {
		t.Fatalf("token should remain after nickname update, got %q err=%v", tok, err)
	}

	// 清令牌失败时不改密码，旧令牌保持有效。
	testRdb := db.Rdb
	db.Rdb = nil
	blockedPwd := "should-not-apply"
	if err := UserSvc.UpdateUser(u.ID, u.Role, model.UserUpdatePayload{
		ID:       u.ID,
		Password: &blockedPwd,
	}); err == nil {
		t.Fatal("expected password update to fail when token store is unavailable")
	}
	db.Rdb = testRdb
	var stored model.User
	if err := gdb.First(&stored, u.ID).Error; err != nil {
		t.Fatal(err)
	}
	if stored.Password != "oldpassword" {
		t.Fatalf("password changed despite token clear failure: %q", stored.Password)
	}
	if tok, err := repository.GetUserToken(u.ID); err != nil || tok != "initial_token" {
		t.Fatalf("token should remain when password update is rejected, got %q err=%v", tok, err)
	}

	// 3. 修改密码，必须清除 Token（强制全设备重新登录）
	newPwd := "newpassword123"
	err = UserSvc.UpdateUser(u.ID, u.Role, model.UserUpdatePayload{
		ID:       u.ID,
		Password: &newPwd,
	})
	if err != nil {
		t.Fatalf("update password: %v", err)
	}
	if tok, err := repository.GetUserToken(u.ID); err != nil || tok != "" {
		t.Fatalf("token should be cleared after password change, got %q err=%v", tok, err)
	}

	// 4. 再次登录后注销，必须清除 Token
	if err := repository.SaveUserToken("second_token", u.ID); err != nil {
		t.Fatalf("save second token: %v", err)
	}
	UserSvc.UserLogout(u.ID)
	if tok, err := repository.GetUserToken(u.ID); err != nil || tok != "" {
		t.Fatalf("token should be cleared after logout, got %q err=%v", tok, err)
	}
}

func TestUserService_BuiltinAdminPasswordProtection(t *testing.T) {
	mr, err := miniredis.Run()
	if err != nil {
		t.Fatal(err)
	}
	defer mr.Close()
	origRdb := db.Rdb
	origMdb := db.Mdb
	defer func() {
		db.Rdb = origRdb
		db.Mdb = origMdb
	}()
	db.Rdb = redis.NewClient(&redis.Options{Addr: mr.Addr()})
	gdb, _ := gorm.Open(sqlite.Open(":memory:"), &gorm.Config{Logger: logger.Default.LogMode(logger.Silent)})
	_ = gdb.AutoMigrate(&model.User{})
	db.Mdb = gdb

	root := model.User{
		Model:    gorm.Model{ID: config.UserIdInitialVal},
		UserName: "admin",
		Password: "rootpassword",
		Salt:     "rootsalt",
		Role:     model.UserRoleAdmin,
		Status:   0,
	}
	gdb.Create(&root)

	newPwd := "hacked123"
	err = UserSvc.UpdateUser(10001, model.UserRoleAdmin, model.UserUpdatePayload{
		ID:       config.UserIdInitialVal,
		Password: &newPwd,
	})
	if err == nil || !strings.Contains(err.Error(), "默认超级管理员密码仅允许本人修改") {
		t.Fatalf("expected error on another admin modifying root password, got: %v", err)
	}

	err = UserSvc.UpdateUser(config.UserIdInitialVal, model.UserRoleAdmin, model.UserUpdatePayload{
		ID:       config.UserIdInitialVal,
		Password: &newPwd,
	})
	if err != nil {
		t.Fatalf("expected root admin self password update to succeed, got: %v", err)
	}

	var stored model.User
	if err := gdb.First(&stored, config.UserIdInitialVal).Error; err != nil {
		t.Fatalf("query updated user: %v", err)
	}
	if stored.Password == "rootpassword" || stored.Password == "" {
		t.Fatalf("password should be updated and hashed, got %q", stored.Password)
	}
	if stored.Salt == "rootsalt" || stored.Salt == "" {
		t.Fatalf("salt should be refreshed on password update, got %q", stored.Salt)
	}
	if !UserSvc.VerifyUserPassword(config.UserIdInitialVal, newPwd) {
		t.Fatalf("new password should be verified successfully")
	}
	if tok, err := repository.GetUserToken(config.UserIdInitialVal); err != nil || tok != "" {
		t.Fatalf("token should be cleared after password change, got %q err=%v", tok, err)
	}
}
