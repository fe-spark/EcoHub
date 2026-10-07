package model

import (
	"server/internal/config"
	"testing"
)

func TestIsAdmin(t *testing.T) {
	cases := []struct {
		userID uint
		role   int
		want   bool
	}{
		{config.UserIdInitialVal, UserRoleNormal, true},
		{config.UserIdInitialVal, UserRoleVisitor, true},
		{10001, UserRoleAdmin, true},
		{10001, UserRoleNormal, false},
		{10001, UserRoleVisitor, false},
		{1, UserRoleAdmin, true},
		{1, UserRoleNormal, false},
	}
	for _, c := range cases {
		if got := IsAdmin(c.userID, c.role); got != c.want {
			t.Fatalf("IsAdmin(%d, %d)=%v want %v", c.userID, c.role, got, c.want)
		}
	}
}

func TestCheckUserUpdateStatus(t *testing.T) {
	adminRole := UserRoleAdmin
	normalRole := UserRoleNormal
	disabled := 1
	enabled := 0

	if msg := CheckUserUpdate(10001, adminRole, 10002, "editor", normalRole, enabled, nil, &disabled); msg != "" {
		t.Fatalf("admin should change another user's status, got %q", msg)
	}
	if msg := CheckUserUpdate(config.UserIdInitialVal, adminRole, config.UserIdInitialVal, "admin", adminRole, enabled, nil, &disabled); msg != "不能禁用当前登录账号" {
		t.Fatalf("self disable = %q", msg)
	}
	if msg := CheckUserUpdate(10001, adminRole, config.UserIdInitialVal, "admin", adminRole, enabled, nil, &disabled); msg != "" {
		t.Fatalf("another admin should disable builtin admin, got %q", msg)
	}
	if msg := CheckUserUpdate(10002, normalRole, 10002, "editor", normalRole, enabled, &normalRole, &enabled); msg != "" {
		t.Fatalf("normal user keeping own role and status = %q", msg)
	}
	if msg := CheckUserUpdate(10002, normalRole, 10003, "other", normalRole, enabled, nil, nil); msg != "权限不足，仅可修改本人账号信息" {
		t.Fatalf("normal user editing others = %q", msg)
	}
	visitor := UserRoleVisitor
	if msg := CheckUserUpdate(10001, adminRole, config.UserIdInitialVal, "admin", adminRole, enabled, &visitor, nil); msg != "内置账号不可修改角色" {
		t.Fatalf("builtin role change = %q", msg)
	}
}

func TestCheckUserDelete(t *testing.T) {
	if msg := CheckUserDelete(10001, UserRoleAdmin, 10002, "editor"); msg != "" {
		t.Fatalf("admin delete normal = %q", msg)
	}
	if msg := CheckUserDelete(10001, UserRoleAdmin, 10003, "guest2"); msg != "" {
		t.Fatalf("admin delete custom visitor = %q", msg)
	}
	if msg := CheckUserDelete(10001, UserRoleNormal, 10002, "editor"); msg != "权限不足，仅超级管理员可删除用户" {
		t.Fatalf("normal delete = %q", msg)
	}
	if msg := CheckUserDelete(10001, UserRoleAdmin, 10001, "ops"); msg != "不能删除当前登录账号" {
		t.Fatalf("self delete = %q", msg)
	}
	if msg := CheckUserDelete(10001, UserRoleAdmin, config.UserIdInitialVal, "admin"); msg != "默认超级管理员不可删除" {
		t.Fatalf("builtin admin delete = %q", msg)
	}
	if msg := CheckUserDelete(10001, UserRoleAdmin, 10004, "guest"); msg != "" {
		t.Fatalf("guest is a normal account and can be deleted, got %q", msg)
	}
}

func TestClampNewUser(t *testing.T) {
	role, status := ClampNewUser(false, UserRoleAdmin, 1)
	if role != UserRoleNormal || status != 0 {
		t.Fatalf("non-admin create = %d/%d", role, status)
	}
	role, status = ClampNewUser(true, UserRoleVisitor, 1)
	if role != UserRoleVisitor || status != 1 {
		t.Fatalf("admin create = %d/%d", role, status)
	}
}

func TestUserCanWrite(t *testing.T) {
	if !UserCanWrite(UserRoleAdmin) {
		t.Fatalf("UserCanWrite(UserRoleAdmin) should be true")
	}
	if !UserCanWrite(UserRoleNormal) {
		t.Fatalf("UserCanWrite(UserRoleNormal) should be true")
	}
	if UserCanWrite(UserRoleVisitor) {
		t.Fatalf("UserCanWrite(UserRoleVisitor) should be false")
	}
}
