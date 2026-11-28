package user

import (
	"errors"

	"server/internal/infra/syslog"
	"server/internal/model"
	"server/internal/model/dto"
	"server/internal/utils"
)

type UserService struct{}

var UserSvc = new(UserService)

// UserLogin 用户登录
func (s *UserService) UserLogin(account, password string) (token string, err error) {
	// 根据 username 或 email 查询用户信息
	u, err := GetUserByNameOrEmail(account)
	if err != nil {
		return "", err
	}
	if u == nil {
		return "", errors.New("用户名或密码错误")
	}
	// 校验用户信息, 执行账号密码校验逻辑
	matched, needsUpgrade := utils.VerifyPassword(password, u.Salt, u.Password)
	if !matched {
		return "", errors.New("用户名或密码错误")
	}
	if u.Status != 0 {
		return "", errors.New("账号已被禁用")
	}
	if needsUpgrade {
		if newSalt, saltErr := utils.GenerateSalt(); saltErr == nil {
			if newHash := utils.PasswordEncrypt(password, newSalt); newHash != "" {
				u.Salt = newSalt
				u.Password = newHash
				if upErr := UpdateUserInfo(*u); upErr != nil {
					syslog.Warnf("[User] 密码哈希无感升级失败 user=%d: %v", u.ID, upErr)
				}
			}
		}
	}
	// 密码校验成功后下发token
	token, err = utils.GenToken(u.ID, u.UserName, u.Role)
	if err != nil {
		return "", err
	}
	err = SaveUserToken(token, u.ID, u.Role)
	return
}

// UserLogout 用户退出登录（访客仅注销当前设备令牌，非访客清除单点令牌）
func (s *UserService) UserLogout(id uint, role int, token string) error {
	return RevokeUserToken(id, role, token)
}

// ClearUserSessions 强制失效指定用户的全部登录会话（封禁/改密/改角色等场景）
func (s *UserService) ClearUserSessions(id uint) error {
	return ClearUserToken(id)
}

// VerifyToken 校验用户令牌在 Redis 中的状态
func (s *UserService) VerifyToken(userID uint, role int, token string) (TokenCheckStatus, error) {
	return CheckUserToken(userID, role, token)
}

// AccountGate 读取账号状态和角色。鉴权以库里的值为准，不信 JWT 里的旧角色。
func (s *UserService) AccountGate(userID uint) (status int, role int, err error) {
	return UserAccountGate(userID)
}

// GetUserInfo 获取用户基本信息
func (s *UserService) GetUserInfo(id uint) (model.UserInfoVo, error) {
	u, err := GetUserById(id)
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
	u, err := GetUserById(id)
	if err != nil || u.ID == 0 || u.Password == "" {
		return false
	}
	// 校验密码是否正确
	matched, _ := utils.VerifyPassword(password, u.Salt, u.Password)
	return matched
}

// GetUserPage 用户分页
func (s *UserService) GetUserPage(page *dto.Page, userName string, role, status int) ([]model.UserInfoVo, error) {
	list, err := GetUserPage(page, userName, role, status)
	if err != nil {
		return nil, err
	}
	voList := make([]model.UserInfoVo, 0, len(list))
	for _, u := range list {
		voList = append(voList, buildUserInfoVo(u))
	}
	return voList, nil
}

// AddUser 添加用户
func (s *UserService) AddUser(u model.User) error {
	// 检查用户名是否重复
	exist, err := GetUserByNameOrEmail(u.UserName)
	if err != nil {
		return err
	}
	if exist != nil {
		return errors.New("用户名已存在")
	}
	if u.Email != "" {
		exist, err = GetUserByNameOrEmail(u.Email)
		if err != nil {
			return err
		}
		if exist != nil && exist.ID != 0 {
			return errors.New("邮箱已被其它账号使用")
		}
	}
	salt, err := utils.GenerateSalt()
	if err != nil {
		return err
	}
	u.Salt = salt
	u.Password = utils.PasswordEncrypt(u.Password, u.Salt)
	return AddUser(&u)
}

// UpdateProfile 个人资料修改（含修改密码的原密码校验）
func (s *UserService) UpdateProfile(userID uint, req model.UserUpdatePayload) error {
	oldUser, err := GetUserById(userID)
	if err != nil || oldUser.ID == 0 {
		return errors.New("用户不存在")
	}
	if req.Password != nil && *req.Password != "" {
		if oldUser.Password != "" {
			if req.OldPassword == nil || *req.OldPassword == "" {
				return errors.New("修改密码必须提供原密码")
			}
			if matched, _ := utils.VerifyPassword(*req.OldPassword, oldUser.Salt, oldUser.Password); !matched {
				return errors.New("原密码校验失败，请核对后重试")
			}
		}
	}
	req.ID = userID
	req.Role = nil
	req.Status = nil
	return s.UpdateUser(userID, oldUser.Role, req)
}

// UpdateUser 更新用户。operatorID、operatorRole 为当前登录人。
func (s *UserService) UpdateUser(operatorID uint, operatorRole int, req model.UserUpdatePayload) error {
	oldUser, err := GetUserById(req.ID)
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
			exist, err := GetUserByNameOrEmail(*req.Email)
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
	// 默认超级管理员锁定角色，且密码仅允许本人修改。
	if model.IsBuiltinAccount(oldUser.ID, oldUser.UserName) {
		u.Role = model.UserRoleAdmin
		roleChanged = false
		if req.Password != nil && *req.Password != "" && operatorID != oldUser.ID {
			return errors.New("默认超级管理员密码仅允许本人修改")
		}
	}
	// 如果修改了密码，需要重新加密
	passwordChanged := false
	if req.Password != nil && *req.Password != "" {
		newSalt, err := utils.GenerateSalt()
		if err != nil {
			return err
		}
		u.Salt = newSalt
		u.Password = utils.PasswordEncrypt(*req.Password, u.Salt)
		passwordChanged = true
	}
	// 先清令牌再改库。清失败则密码、角色、状态都不变，旧令牌仍只对应旧账号。
	if passwordChanged || u.Status == 1 || roleChanged {
		if err := ClearUserToken(u.ID); err != nil {
			return err
		}
	}
	return UpdateUserInfo(u)
}

// DeleteUser 删除用户。operatorID、operatorRole 为当前登录人。
func (s *UserService) DeleteUser(operatorID uint, operatorRole int, id uint) error {
	user, err := GetUserById(id)
	if err != nil {
		return err
	}
	if user.ID == 0 {
		return errors.New("用户不存在")
	}
	if msg := model.CheckUserDelete(operatorID, operatorRole, id, user.UserName); msg != "" {
		return errors.New(msg)
	}
	if err = ClearUserToken(id); err != nil {
		return err
	}
	return DeleteUser(id)
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
