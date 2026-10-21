package repository

import (
	"errors"
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

// InitBuiltinAccounts 初始化内置账号。默认只有超级管理员，其它角色由管理员自行创建。
func InitBuiltinAccounts() {
	ensureBuiltinUser(config.DefaultAdminUser, config.DefaultAdminPass, "", "Spark", model.UserRoleAdmin)
}

func ensureBuiltinUser(userName, password, email, nickName string, role int) {
	user, err := GetUserByNameOrEmail(userName)
	if err != nil {
		log.Println("ensureBuiltinUser Error:", err)
		return
	}
	if user != nil {
		updates := map[string]any{}
		if user.Role != role {
			updates["role"] = role
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
func GetUserByNameOrEmail(userName string) (*model.User, error) {
	if db.Mdb == nil {
		return nil, errors.New("数据库未连接")
	}
	var u model.User
	err := db.Mdb.Where("user_name = ? OR email = ?", userName, userName).Limit(1).Find(&u).Error
	if err != nil {
		return nil, err
	}
	if u.ID == 0 {
		return nil, nil
	}
	return &u, nil
}

// GetUserById 通过id获取对应的用户信息
func GetUserById(id uint) (model.User, error) {
	var user = model.User{Model: gorm.Model{ID: id}}
	if db.Mdb == nil {
		return user, errors.New("数据库未连接")
	}
	err := db.Mdb.First(&user).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return model.User{}, nil
	}
	if err != nil {
		return model.User{}, err
	}
	return user, nil
}

// UpdateUserInfo 更新用户信息
func UpdateUserInfo(u model.User) {
	updates := map[string]any{
		"email":     u.Email,
		"nick_name": u.NickName,
		"status":    u.Status,
		"gender":    u.Gender,
		"avatar":    u.Avatar,
		"role":      u.Role,
	}
	if u.Password != "" {
		updates["password"] = u.Password
	}
	db.Mdb.Model(&model.User{}).Where("id = ?", u.ID).Updates(updates)
}

// GetUserPage 分页获取用户信息
func GetUserPage(page *dto.Page, userName string, role, status int) []model.User {
	var list []model.User
	query := db.Mdb.Model(&model.User{})
	if userName != "" {
		query = query.Where("user_name LIKE ?", "%"+userName+"%")
	}
	if role >= 0 {
		query = query.Where("role = ?", role)
	}
	if status >= 0 {
		query = query.Where("status = ?", status)
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
