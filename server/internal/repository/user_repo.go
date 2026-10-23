package repository

import (
	"errors"
	"log"
	"os"
	"strings"

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

// InitBuiltinAccounts 初始化内置管理员。已存在则不改密码。
// 首次创建优先读取 ECOHUB_ADMIN_PASSWORD；未设置时回退至默认密码 admin，密码不写入日志。
func InitBuiltinAccounts() error {
	existing, err := GetUserByNameOrEmail(config.DefaultAdminUser)
	if err != nil {
		return err
	}
	if existing != nil {
		if existing.Role == model.UserRoleAdmin {
			return nil
		}
		return db.Mdb.Model(&model.User{}).Where("id = ?", existing.ID).Update("role", model.UserRoleAdmin).Error
	}
	password := strings.TrimSpace(os.Getenv("ECOHUB_ADMIN_PASSWORD"))
	if password == "" {
		password = config.DefaultAdminPassword
	}
	_, err = ensureBuiltinUser(config.DefaultAdminUser, password, "", "Spark", model.UserRoleAdmin)
	if err != nil {
		return err
	}
	log.Printf("[Init] 已创建管理员 %s", config.DefaultAdminUser)
	return nil
}

func ensureBuiltinUser(userName, password, email, nickName string, role int) (bool, error) {
	user, err := GetUserByNameOrEmail(userName)
	if err != nil {
		return false, err
	}
	if user != nil {
		if user.Role == role {
			return false, nil
		}
		err = db.Mdb.Model(&model.User{}).Where("id = ?", user.ID).Update("role", role).Error
		return false, err
	}

	salt, err := utils.GenerateSalt()
	if err != nil {
		return false, err
	}
	u := &model.User{
		UserName: userName,
		Password: utils.PasswordEncrypt(password, salt),
		Salt:     salt,
		Email:    email,
		Gender:   2,
		NickName: nickName,
		Avatar:   "empty",
		Status:   0,
		Role:     role,
	}
	if err := db.Mdb.Create(u).Error; err != nil {
		return false, err
	}
	return true, nil
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
func UpdateUserInfo(u model.User) error {
	if db.Mdb == nil {
		return errors.New("数据库未连接")
	}
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
	return db.Mdb.Model(&model.User{}).Where("id = ?", u.ID).Updates(updates).Error
}

// GetUserPage 分页获取用户信息
func GetUserPage(page *dto.Page, userName string, role, status int) ([]model.User, error) {
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
	if err := dto.GetPage(query, page); err != nil {
		return nil, err
	}
	if page.PageCount == 0 {
		return []model.User{}, nil
	}
	err := query.Order("id DESC").Offset((page.Current - 1) * page.PageSize).Limit(page.PageSize).Find(&list).Error
	return list, err
}

// AddUser 添加新用户
func AddUser(u *model.User) error {
	return db.Mdb.Create(u).Error
}

// DeleteUser 删除用户
func DeleteUser(id uint) error {
	return db.Mdb.Delete(&model.User{}, id).Error
}

// UserAccountGate 读取账号状态和角色。状态 0 为正常。
func UserAccountGate(id uint) (status int, role int, err error) {
	if db.Mdb == nil {
		return 0, 0, errors.New("数据库未连接")
	}
	var row struct {
		Status int
		Role   int
	}
	err = db.Mdb.Model(&model.User{}).Select("status", "role").Where("id = ?", id).Take(&row).Error
	if errors.Is(err, gorm.ErrRecordNotFound) {
		return 0, 0, errors.New("用户不存在")
	}
	if err != nil {
		return 0, 0, err
	}
	return row.Status, row.Role, nil
}

// UserAccountStatus 读取账号状态。0 为正常。
func UserAccountStatus(id uint) (int, error) {
	status, _, err := UserAccountGate(id)
	return status, err
}
