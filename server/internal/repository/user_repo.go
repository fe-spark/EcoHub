package repository

import (
	"log"
	"server/internal/config"
	"server/internal/infra/db"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"

	"gorm.io/gorm"
)

// ExistUserTable 判断表中是否存在User表
func ExistUserTable() bool {
	return db.Mdb.Migrator().HasTable(&model.User{})
}

// InitBuiltinAccounts 初始化内置账号
func InitBuiltinAccounts() {
	ensureBuiltinUser(config.DefaultAdminUser, config.DefaultAdminPass, "administrator@gmail.com", "Spark", model.UserRoleAdmin)
	ensureBuiltinUser(config.DefaultVisitorUser, config.DefaultVisitorPass, "guest@example.com", "访客", model.UserRoleVisitor)
}

func ensureBuiltinUser(userName, password, email, nickName string, role int) {
	user := GetUserByNameOrEmail(userName)
	if user != nil {
		updates := map[string]any{}
		if user.Role != role {
			updates["role"] = role
		}
		if user.Status != 0 {
			updates["status"] = 0
		}
		if len(updates) > 0 {
			db.Mdb.Model(&model.User{}).Where("id = ?", user.ID).Updates(updates)
		}
		return
	}

	u := &model.User{
		UserName: userName,
		Password: password,
		Salt:     utils.GenerateSalt(),
		Email:    email,
		Gender:   2,
		NickName: nickName,
		Avatar:   "empty",
		Status:   0,
		Role:     role,
	}

	u.Password = utils.PasswordEncrypt(u.Password, u.Salt)
	db.Mdb.Create(u)
}

// GetUserByNameOrEmail 查询 username || email 对应的账户信息
func GetUserByNameOrEmail(userName string) *model.User {
	var u model.User
	err := db.Mdb.Where("user_name = ? OR email = ?", userName, userName).Limit(1).Find(&u).Error
	if err != nil {
		log.Println("GetUserByNameOrEmail Error:", err)
		return nil
	}
	if u.ID == 0 {
		return nil
	}
	return &u
}

// GetUserById 通过id获取对应的用户信息
func GetUserById(id uint) model.User {
	var user = model.User{Model: gorm.Model{ID: id}}
	db.Mdb.First(&user)
	return user
}

// UpdateUserInfo 更新用户信息
func UpdateUserInfo(u model.User) {
	// 值更新允许修改的部分字段, 零值会在更新时被自动忽略
	db.Mdb.Model(&u).Updates(model.User{Password: u.Password, Email: u.Email, NickName: u.NickName, Status: u.Status, Gender: u.Gender, Avatar: u.Avatar, Role: u.Role})
}

// GetUserPage 分页获取用户信息
func GetUserPage(page *dto.Page, userName string) []model.User {
	var list []model.User
	query := db.Mdb.Model(&model.User{})
	if userName != "" {
		query = query.Where("user_name LIKE ?", "%"+userName+"%")
	}
	dto.GetPage(query, page)
	query.Order("id DESC").Offset((page.Current - 1) * page.PageSize).Limit(page.PageSize).Find(&list)
	return list
}

// AddUser 添加新用户
func AddUser(u *model.User) error {
	return db.Mdb.Create(u).Error
}

// DeleteUser 删除用户
func DeleteUser(id uint) error {
	return db.Mdb.Delete(&model.User{}, id).Error
}
