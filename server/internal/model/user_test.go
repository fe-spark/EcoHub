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
