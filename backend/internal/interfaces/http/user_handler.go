package http

import (
	"database/sql"
	"errors"
	"fmt"
	"mime/multipart"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"

	appauth "voc-go-backend/internal/application/auth"
	rbac "voc-go-backend/internal/domain/rbac"
	"voc-go-backend/internal/domain/user"
	"voc-go-backend/internal/infrastructure/security"
)

// UserHandler exposes /auth/user related endpoints (info, route).
type UserHandler struct {
	users        user.Repository
	roles        rbac.RoleRepository
	menus        rbac.MenuRepository
	tokenSvc     *security.TokenService
	db           *sql.DB
	rsaDecryptor *security.RSADecryptor
	hasher       security.PasswordHasher
	verifier     security.PasswordVerifier
}

func NewUserHandler(
	users user.Repository,
	roles rbac.RoleRepository,
	menus rbac.MenuRepository,
	tokenSvc *security.TokenService,
	db *sql.DB,
	rsaDecryptor *security.RSADecryptor,
	hasher security.PasswordHasher,
	verifier security.PasswordVerifier,
) *UserHandler {
	return &UserHandler{
		users:        users,
		roles:        roles,
		menus:        menus,
		tokenSvc:     tokenSvc,
		db:           db,
		rsaDecryptor: rsaDecryptor,
		hasher:       hasher,
		verifier:     verifier,
	}
}

// RegisterUserRoutes registers /auth/user endpoints.
func (h *UserHandler) RegisterUserRoutes(r *gin.Engine) {
	r.GET("/auth/user/info", h.GetUserInfo)
	r.GET("/auth/user/route", h.ListUserRoute)
	r.PATCH("/user/profile/avatar", h.UploadAvatar)
	r.PATCH("/user/profile/basic/info", h.UpdateBasicInfo)
	r.PATCH("/user/profile/password", h.UpdatePassword)
}

// GetUserInfo handles GET /auth/user/info.
func (h *UserHandler) GetUserInfo(c *gin.Context) {
	userID := CurrentUserID(c)
	if userID == 0 {
		Fail(c, "401", "未授权，请重新登录")
		return
	}

	domainUser, err := h.users.GetByID(c.Request.Context(), userID)
	if err != nil {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	if domainUser == nil || !domainUser.IsEnabled() {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	// 角色与权限
	roles, err := h.roles.ListByUserID(c.Request.Context(), userID)
	if err != nil {
		Fail(c, "500", "获取角色信息失败")
		return
	}
	roleCodes := appauth.ExtractRoleCodes(roles)

	perms, err := h.menus.ListPermissionsByUserID(c.Request.Context(), userID)
	if err != nil {
		Fail(c, "500", "获取权限信息失败")
		return
	}

	deptName, err := h.loadDeptName(c, domainUser.DeptID)
	if err != nil {
		Fail(c, "500", "获取部门信息失败")
		return
	}
	pwdExpiry, err := h.loadPasswordExpiry(c, domainUser.PwdResetTime)
	if err != nil {
		Fail(c, "500", "获取密码策略失败")
		return
	}

	info := appauth.BuildUserInfo(
		domainUser, roleCodes, perms, deptName,
		pwdExpiry.Expired, pwdExpiry.Warning, pwdExpiry.DaysRemaining,
	)
	OK(c, info)
}

// ListUserRoute returns an empty route list for now.
func (h *UserHandler) ListUserRoute(c *gin.Context) {
	userID := CurrentUserID(c)
	if userID == 0 {
		Fail(c, "401", "未授权，请重新登录")
		return
	}

	// Load roles and menus for this user.
	roles, err := h.roles.ListByUserID(c.Request.Context(), userID)
	if err != nil {
		Fail(c, "500", "获取角色信息失败")
		return
	}
	if len(roles) == 0 {
		OK(c, []appauth.RouteItem{})
		return
	}

	// Collect menus for all roles (de-duplicated by id).
	menuMap := make(map[int64]rbac.Menu)
	for _, rctx := range roles {
		menus, err := h.menus.ListByRoleID(c.Request.Context(), rctx.ID)
		if err != nil {
			Fail(c, "500", "获取菜单信息失败")
			return
		}
		for _, m := range menus {
			menuMap[m.ID] = m
		}
	}
	flatMenus := make([]rbac.Menu, 0, len(menuMap))
	for _, m := range menuMap {
		flatMenus = append(flatMenus, m)
	}

	roleCodes := appauth.ExtractRoleCodes(roles)
	tree := appauth.BuildRouteTree(flatMenus, roleCodes)
	OK(c, tree)
}

type updateUserBasicInfoReq struct {
	Nickname string `json:"nickname"`
	Gender   int16  `json:"gender"`
}

type updateUserPasswordReq struct {
	OldPassword string `json:"oldPassword"`
	NewPassword string `json:"newPassword"`
}

type uploadAvatarResp struct {
	Avatar string `json:"avatar"`
}

func (h *UserHandler) UploadAvatar(c *gin.Context) {
	userID := CurrentUserID(c)
	if userID == 0 {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	file, err := c.FormFile("avatarFile")
	if err != nil {
		Fail(c, "400", "请选择头像文件")
		return
	}
	avatar, err := h.saveAvatarFile(c, userID, file)
	if err != nil {
		Fail(c, "500", "上传头像失败")
		return
	}
	if h.db == nil {
		Fail(c, "500", "数据库未初始化")
		return
	}
	if _, err := h.db.ExecContext(
		c.Request.Context(),
		`UPDATE sys_user SET avatar = $1, update_user = $2, update_time = $3 WHERE id = $4`,
		avatar,
		userID,
		time.Now(),
		userID,
	); err != nil {
		Fail(c, "500", "更新头像失败")
		return
	}
	OK(c, uploadAvatarResp{Avatar: avatar})
}

func (h *UserHandler) UpdateBasicInfo(c *gin.Context) {
	userID := CurrentUserID(c)
	if userID == 0 {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	var req updateUserBasicInfoReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, "400", "参数缺失或格式不正确")
		return
	}
	req.Nickname = strings.TrimSpace(req.Nickname)
	if req.Nickname == "" {
		Fail(c, "400", "昵称不能为空")
		return
	}
	if req.Gender < 0 || req.Gender > 2 {
		Fail(c, "400", "性别不正确")
		return
	}
	if h.db == nil {
		Fail(c, "500", "数据库未初始化")
		return
	}
	result, err := h.db.ExecContext(
		c.Request.Context(),
		`UPDATE sys_user SET nickname = $1, gender = $2, update_user = $3, update_time = $4 WHERE id = $5`,
		req.Nickname,
		req.Gender,
		userID,
		time.Now(),
		userID,
	)
	if err != nil {
		Fail(c, "500", "更新基本信息失败")
		return
	}
	if affected, err := result.RowsAffected(); err == nil && affected == 0 {
		Fail(c, "404", "用户不存在")
		return
	}
	OK(c, true)
}

func (h *UserHandler) UpdatePassword(c *gin.Context) {
	userID := CurrentUserID(c)
	if userID == 0 {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	var req updateUserPasswordReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, "400", "参数缺失或格式不正确")
		return
	}
	oldPassword, err := h.decryptPassword(strings.TrimSpace(req.OldPassword))
	if err != nil {
		Fail(c, "400", "当前密码格式不正确")
		return
	}
	newPassword, err := h.decryptPassword(strings.TrimSpace(req.NewPassword))
	if err != nil {
		Fail(c, "400", "新密码格式不正确")
		return
	}
	if oldPassword == "" || newPassword == "" {
		Fail(c, "400", "密码不能为空")
		return
	}
	if oldPassword == newPassword {
		Fail(c, "400", "新密码与旧密码不能相同")
		return
	}
	domainUser, err := h.users.GetByID(c.Request.Context(), userID)
	if err != nil || domainUser == nil || !domainUser.IsEnabled() {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	if h.verifier == nil || h.hasher == nil || h.db == nil {
		Fail(c, "500", "密码组件未初始化")
		return
	}
	ok, err := h.verifier.Verify(oldPassword, domainUser.Password)
	if err != nil {
		Fail(c, "500", "密码校验失败")
		return
	}
	if !ok {
		Fail(c, "400", "当前密码不正确")
		return
	}
	passwordPolicy, err := loadPasswordPolicy(c.Request.Context(), h.db)
	if err != nil {
		Fail(c, "500", "读取密码策略失败")
		return
	}
	if err := validatePassword(newPassword, domainUser.Username, passwordPolicy); err != nil {
		Fail(c, "400", err.Error())
		return
	}
	used, err := passwordRecentlyUsed(
		c.Request.Context(), h.db, h.verifier, userID, newPassword, domainUser.Password, passwordPolicy.RepetitionTimes,
	)
	if err != nil {
		Fail(c, "500", "校验历史密码失败")
		return
	}
	if used {
		Fail(c, "400", fmt.Sprintf("不能使用最近 %d 次使用过的密码", passwordPolicy.RepetitionTimes))
		return
	}
	encoded, err := h.hasher.Hash(newPassword)
	if err != nil {
		Fail(c, "500", "密码加密失败")
		return
	}
	if err := updatePasswordWithHistory(
		c.Request.Context(), h.db, userID, encoded, domainUser.Password, userID, passwordPolicy.RepetitionTimes,
	); err != nil {
		Fail(c, "500", "修改密码失败")
		return
	}
	OK(c, true)
}

func (h *UserHandler) decryptPassword(raw string) (string, error) {
	if raw == "" {
		return "", nil
	}
	if h.rsaDecryptor == nil {
		return raw, nil
	}
	plain, err := h.rsaDecryptor.DecryptBase64(raw)
	if err != nil {
		return "", err
	}
	return strings.TrimSpace(plain), nil
}

func (h *UserHandler) loadDeptName(c *gin.Context, deptID int64) (string, error) {
	if h.db == nil || deptID == 0 {
		return "", nil
	}
	var name sql.NullString
	err := h.db.QueryRowContext(
		c.Request.Context(),
		`SELECT name FROM sys_dept WHERE id = $1 LIMIT 1`,
		deptID,
	).Scan(&name)
	if errors.Is(err, sql.ErrNoRows) {
		return "", nil
	}
	if err != nil {
		return "", err
	}
	if !name.Valid {
		return "", nil
	}
	return name.String, nil
}

type passwordExpiryState struct {
	Expired       bool
	Warning       bool
	DaysRemaining int
}

func (h *UserHandler) loadPasswordExpiry(c *gin.Context, pwdResetTime *time.Time) (passwordExpiryState, error) {
	if h.db == nil || pwdResetTime == nil {
		return passwordExpiryState{}, nil
	}
	expirationValue, found, err := loadPasswordOption(c.Request.Context(), h.db, "PASSWORD_EXPIRATION_DAYS")
	if err != nil {
		return passwordExpiryState{}, err
	}
	if !found || expirationValue == "" {
		return passwordExpiryState{}, nil
	}
	days, err := strconv.Atoi(expirationValue)
	if err != nil {
		return passwordExpiryState{}, err
	}
	if days <= 0 {
		return passwordExpiryState{}, nil
	}
	expiresAt := pwdResetTime.AddDate(0, 0, days)
	remaining := time.Until(expiresAt)
	if remaining <= 0 {
		return passwordExpiryState{Expired: true}, nil
	}
	daysRemaining := int((remaining + 24*time.Hour - 1) / (24 * time.Hour))
	warningValue, found, err := loadPasswordOption(c.Request.Context(), h.db, "PASSWORD_EXPIRATION_WARNING_DAYS")
	if err != nil {
		return passwordExpiryState{}, err
	}
	warningDays := 0
	if found && warningValue != "" {
		warningDays, err = strconv.Atoi(warningValue)
		if err != nil {
			return passwordExpiryState{}, err
		}
	}
	return passwordExpiryState{
		Warning:       warningDays > 0 && daysRemaining <= warningDays,
		DaysRemaining: daysRemaining,
	}, nil
}

func (h *UserHandler) saveAvatarFile(c *gin.Context, userID int64, file *multipart.FileHeader) (string, error) {
	if file == nil {
		return "", errors.New("avatar file is nil")
	}
	ext := strings.ToLower(filepath.Ext(file.Filename))
	switch ext {
	case ".jpg", ".jpeg", ".png", ".webp", ".gif":
	default:
		ext = ".png"
	}
	root := os.Getenv("FILE_STORAGE_DIR")
	if root == "" {
		root = "./data/file"
	}
	dir := filepath.Join(root, "avatar")
	if err := os.MkdirAll(dir, 0o755); err != nil {
		return "", err
	}
	name := fmt.Sprintf("%d-%d%s", userID, time.Now().UnixNano(), ext)
	dst := filepath.Join(dir, name)
	if err := c.SaveUploadedFile(file, dst); err != nil {
		return "", err
	}
	return "/file/avatar/" + name, nil
}
