package service

import (
	"testing"

	"server/internal/infra/db"
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
