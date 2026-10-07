package model

import (
	"server/internal/config"

	"gorm.io/gorm"
)

const (
	UserRoleNormal = iota
	UserRoleAdmin
	UserRoleVisitor
)

func GetUserRoleName(role int) string {
	switch role {
	case UserRoleAdmin:
		return "超级用户"
	case UserRoleVisitor:
		return "访客"
	default:
		return "普通用户"
	}
}

func IsAdminRole(role int) bool {
	return role == UserRoleAdmin
}

func IsVisitorRole(role int) bool {
	return role == UserRoleVisitor
}

func UserCanWrite(role int) bool {
	return !IsVisitorRole(role)
}

func IsAdmin(userID uint, role int) bool {
	return userID == config.UserIdInitialVal || IsAdminRole(role)
}

// IsBuiltinAccount 仅默认超级管理员锁定角色且不可删除。
func IsBuiltinAccount(id uint, _ string) bool {
	return id == config.UserIdInitialVal
}

// ClampNewUser 非超级管理员只能创建启用中的普通用户。
func ClampNewUser(operatorIsAdmin bool, role, status int) (int, int) {
	if !operatorIsAdmin {
		return UserRoleNormal, 0
	}
	if role != UserRoleAdmin && role != UserRoleVisitor && role != UserRoleNormal {
		role = UserRoleNormal
	}
	if status != 0 && status != 1 {
		status = 0
	}
	return role, status
}

// CheckUserUpdate 校验账号更新。nextRole、nextStatus 为空表示本次不修改。返回空字符串表示允许。
func CheckUserUpdate(operatorID uint, operatorRole int, targetID uint, targetName string, targetRole, targetStatus int, nextRole, nextStatus *int) string {
	if targetID == 0 {
		return "用户不存在"
	}
	operatorIsAdmin := IsAdmin(operatorID, operatorRole)
	if !operatorIsAdmin && operatorID != targetID {
		return "权限不足，仅可修改本人账号信息"
	}
	roleChanged := nextRole != nil && *nextRole != targetRole
	statusChanged := nextStatus != nil && *nextStatus != targetStatus
	if !operatorIsAdmin {
		if roleChanged {
			return "权限不足，仅超级管理员可修改用户角色"
		}
		if statusChanged {
			return "权限不足，仅超级管理员可修改账号状态"
		}
	}
	if roleChanged {
		if *nextRole != UserRoleAdmin && *nextRole != UserRoleVisitor && *nextRole != UserRoleNormal {
			return "无效的用户角色"
		}
		if IsBuiltinAccount(targetID, targetName) {
			return "内置账号不可修改角色"
		}
	}
	if statusChanged {
		if *nextStatus != 0 && *nextStatus != 1 {
			return "无效的账号状态"
		}
		if *nextStatus == 1 && operatorID == targetID {
			return "不能禁用当前登录账号"
		}
	}
	return ""
}

// CheckUserDelete 校验账号删除。返回空字符串表示允许。
func CheckUserDelete(operatorID uint, operatorRole int, targetID uint, _ string) string {
	if !IsAdmin(operatorID, operatorRole) {
		return "权限不足，仅超级管理员可删除用户"
	}
	if targetID == 0 {
		return "用户不存在"
	}
	if operatorID == targetID {
		return "不能删除当前登录账号"
	}
	if targetID == config.UserIdInitialVal {
		return "默认超级管理员不可删除"
	}
	return ""
}

type User struct {
	gorm.Model
	UserName string `json:"userName"` // 用户名
	Password string `json:"password"` // 密码
	Salt     string `json:"salt"`     // 盐值
	Email    string `json:"email"`    // 邮箱
	Gender   int    `json:"gender"`   // 性别
	NickName string `json:"nickName"` // 昵称
	Avatar   string `json:"avatar"`   // 头像
	Status   int    `json:"status"`   // 状态
	Role     int    `json:"role"`     // 角色
	Reserve1 string `json:"reserve1"` // 预留字段 3
	Reserve2 string `json:"reserve2"` // 预留字段 2
	Reserve3 string `json:"reserve3"` // 预留字段 1
}

func (User) TableName() string {
	return TableUser
}

type UserUpdatePayload struct {
	ID       uint    `json:"id"`
	Password *string `json:"password"`
	Email    *string `json:"email"`
	Gender   *int    `json:"gender"`
	NickName *string `json:"nickName"`
	Avatar   *string `json:"avatar"`
	Status   *int    `json:"status"`
	Role     *int    `json:"role"`
}

// UserInfoVo 用户信息返回对象
type UserInfoVo struct {
	Id        uint   `json:"id"`
	UserName  string `json:"userName"`  // 用户名
	Email     string `json:"email"`     // 邮箱
	Gender    int    `json:"gender"`    // 性别
	NickName  string `json:"nickName"`  // 昵称
	Avatar    string `json:"avatar"`    // 头像
	Status    int    `json:"status"`    // 状态
	IsAdmin   bool   `json:"isAdmin"`   // 是否为超级管理员
	IsVisitor bool   `json:"isVisitor"` // 是否为访客只读用户
	CanWrite  bool   `json:"canWrite"`  // 是否允许写操作
	Builtin   bool   `json:"builtin"`   // 内置账号，不可改角色、状态，也不可删除
	Role      int    `json:"role"`      // 角色值
	RoleName  string `json:"roleName"`  // 角色名称
}
