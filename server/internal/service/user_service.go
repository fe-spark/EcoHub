package service

import (
	"errors"
	"server/internal/config"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/repository"
	"server/internal/utils"
)

type UserService struct{}

var UserSvc = new(UserService)

// UserLogin 用户登录
func (s *UserService) UserLogin(account, password string) (token string, err error) {
	// 根据 username 或 email 查询用户信息
	u, err := repository.GetUserByNameOrEmail(account)
	if err != nil {
		return "", err
	}
	if u == nil {
		return "", errors.New("用户信息不存在!!!")
	}
	// 校验用户信息, 执行账号密码校验逻辑
	if utils.PasswordEncrypt(password, u.Salt) != u.Password {
		return "", errors.New("用户名或密码错误")
	}
	if u.Status != 0 {
		return "", errors.New("账号已被禁用")
	}
	// 密码校验成功后下发token
	token, err = utils.GenToken(u.ID, u.UserName, u.Role)
	if err != nil {
		return "", err
	}
	err = repository.SaveUserToken(token, u.ID)
	return
}

// UserLogout 用户退出登录 注销
func (s *UserService) UserLogout(id uint) {
	// 通过用户ID清除Redis中的token信息
	_ = repository.ClearUserToken(id)
}

// GetUserInfo 获取用户基本信息
func (s *UserService) GetUserInfo(id uint) (model.UserInfoVo, error) {
	u, err := repository.GetUserById(id)
	if err != nil {
		return model.UserInfoVo{}, err
	}
	if u.ID == 0 {
		return model.UserInfoVo{}, errors.New("用户不存在")
	}
	return buildUserInfoVo(u), nil
}

// VerifyUserPassword 校验密码
func (s *UserService) VerifyUserPassword(id uint, password string) bool {
	if id == 0 || password == "" {
		return false
	}
	// 获取当前登录的用户全部信息
	u, err := repository.GetUserById(id)
	if err != nil || u.ID == 0 || u.Password == "" {
		return false
	}
	// 校验密码是否正确
	return utils.PasswordEncrypt(password, u.Salt) == u.Password
}

// GetUserPage 用户分页
func (s *UserService) GetUserPage(page *dto.Page, userName string, role, status int) []model.UserInfoVo {
	list := repository.GetUserPage(page, userName, role, status)
	var voList []model.UserInfoVo
	for _, u := range list {
		voList = append(voList, buildUserInfoVo(u))
	}
	return voList
}

// AddUser 添加用户
func (s *UserService) AddUser(u model.User) error {
	// 检查用户名是否重复
	exist, err := repository.GetUserByNameOrEmail(u.UserName)
	if err != nil {
		return err
	}
	if exist != nil {
		return errors.New("用户名已存在")
	}
	if u.Email != "" {
		exist, err = repository.GetUserByNameOrEmail(u.Email)
		if err != nil {
			return err
		}
		if exist != nil && exist.ID != 0 {
			return errors.New("邮箱已被其它账号使用")
		}
	}
	// 密码加密
	u.Salt = utils.GenerateSalt()
	u.Password = utils.PasswordEncrypt(u.Password, u.Salt)
	return repository.AddUser(&u)
}

// UpdateUser 更新用户。operatorID、operatorRole 为当前登录人。
func (s *UserService) UpdateUser(operatorID uint, operatorRole int, req model.UserUpdatePayload) error {
	oldUser, err := repository.GetUserById(req.ID)
	if err != nil {
		return err
	}
	if oldUser.ID == 0 {
		return errors.New("用户不存在")
	}
	if msg := model.CheckUserUpdate(operatorID, operatorRole, oldUser.ID, oldUser.UserName, oldUser.Role, oldUser.Status, req.Role, req.Status); msg != "" {
		return errors.New(msg)
	}
	u := oldUser

	if req.Email != nil {
		if *req.Email != "" && *req.Email != oldUser.Email {
			exist, err := repository.GetUserByNameOrEmail(*req.Email)
			if err != nil {
				return err
			}
			if exist != nil && exist.ID != oldUser.ID {
				return errors.New("邮箱已被其它账号使用")
			}
		}
		u.Email = *req.Email
	}
	if req.NickName != nil {
		u.NickName = *req.NickName
	}
	if req.Avatar != nil {
		u.Avatar = *req.Avatar
	}
	if req.Gender != nil {
		u.Gender = *req.Gender
	}
	if req.Status != nil {
		u.Status = *req.Status
	}
	roleChanged := false
	if req.Role != nil {
		if *req.Role != oldUser.Role {
			roleChanged = true
		}
		u.Role = *req.Role
	}
	// 默认超级管理员只锁定角色，状态可以改。
	if oldUser.ID == config.UserIdInitialVal {
		u.Role = model.UserRoleAdmin
		roleChanged = false
	}
	// 如果修改了密码，需要重新加密
	if req.Password != nil && *req.Password != "" {
		u.Salt = oldUser.Salt
		u.Password = utils.PasswordEncrypt(*req.Password, u.Salt)
	}
	repository.UpdateUserInfo(u)
	// 如果用户被禁用（Status == 1）或角色变更，强制清除其登录状态使 Token 失效
	if u.Status == 1 || roleChanged {
		_ = repository.ClearUserToken(u.ID)
	}
	return nil
}

// DeleteUser 删除用户。operatorID、operatorRole 为当前登录人。
func (s *UserService) DeleteUser(operatorID uint, operatorRole int, id uint) error {
	user, err := repository.GetUserById(id)
	if err != nil {
		return err
	}
	if user.ID == 0 {
		return errors.New("用户不存在")
	}
	if msg := model.CheckUserDelete(operatorID, operatorRole, id, user.UserName); msg != "" {
		return errors.New(msg)
	}
	err = repository.DeleteUser(id)
	if err == nil {
		// 删除成功后，强制清除该用户的登录状态
		_ = repository.ClearUserToken(id)
	}
	return err
}

func buildUserInfoVo(u model.User) model.UserInfoVo {
	isAdmin := model.IsAdmin(u.ID, u.Role)
	isVisitor := model.IsVisitorRole(u.Role)
	return model.UserInfoVo{
		Id:        u.ID,
		UserName:  u.UserName,
		Email:     u.Email,
		Gender:    u.Gender,
		NickName:  u.NickName,
		Avatar:    u.Avatar,
		Status:    u.Status,
		IsAdmin:   isAdmin,
		IsVisitor: isVisitor,
		CanWrite:  model.UserCanWrite(u.Role),
		Builtin:   model.IsBuiltinAccount(u.ID, u.UserName),
		Role:      u.Role,
		RoleName:  model.GetUserRoleName(u.Role),
	}
}
