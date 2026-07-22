package http

import (
	"context"
	"database/sql"
	"encoding/csv"
	"encoding/json"
	"errors"
	"fmt"
	"io"
	"net/http"
	"net/mail"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"voc-go-backend/internal/infrastructure/id"
	"voc-go-backend/internal/infrastructure/security"
)

// UserResp matches UserResp in admin/src/apis/system/type.ts.
type UserResp struct {
	ID               int64    `json:"id"`
	Username         string   `json:"username"`
	Nickname         string   `json:"nickname"`
	Avatar           string   `json:"avatar"`
	Gender           int16    `json:"gender"`
	Email            string   `json:"email"`
	Phone            string   `json:"phone"`
	Description      string   `json:"description"`
	Status           int16    `json:"status"`
	IsSystem         bool     `json:"isSystem"`
	CreateUserString string   `json:"createUserString"`
	CreateTime       string   `json:"createTime"`
	UpdateUserString string   `json:"updateUserString"`
	UpdateTime       string   `json:"updateTime"`
	DeptID           int64    `json:"deptId"`
	DeptName         string   `json:"deptName"`
	RoleIDs          []int64  `json:"roleIds"`
	RoleNames        []string `json:"roleNames"`
	Disabled         bool     `json:"disabled"`
}

// UserDetailResp extends UserResp with pwdResetTime.
type UserDetailResp struct {
	UserResp
	PwdResetTime string `json:"pwdResetTime,omitempty"`
}

type userReq struct {
	Username    string  `json:"username"`
	Nickname    string  `json:"nickname"`
	Password    string  `json:"password"` // only for create
	Gender      int16   `json:"gender"`
	Email       string  `json:"email"`
	Phone       string  `json:"phone"`
	Avatar      string  `json:"avatar"`
	Description string  `json:"description"`
	Status      int16   `json:"status"`
	DeptID      int64   `json:"deptId"`
	RoleIDs     []int64 `json:"roleIds"`
}

type userPasswordResetReq struct {
	NewPassword string `json:"newPassword"`
}

type userRoleUpdateReq struct {
	RoleIDs []int64 `json:"roleIds"`
}

// UserImportParseResp summarizes validated CSV data held temporarily in Redis.
type UserImportParseResp struct {
	ImportKey          string `json:"importKey"`
	TotalRows          int    `json:"totalRows"`
	ValidRows          int    `json:"validRows"`
	DuplicateUserRows  int    `json:"duplicateUserRows"`
	DuplicateEmailRows int    `json:"duplicateEmailRows"`
	DuplicatePhoneRows int    `json:"duplicatePhoneRows"`
}

// UserImportResultResp summarizes a committed user import.
type UserImportResultResp struct {
	TotalRows  int `json:"totalRows"`
	InsertRows int `json:"insertRows"`
	UpdateRows int `json:"updateRows"`
}

type userImportRow struct {
	Username string `json:"username"`
	Nickname string `json:"nickname"`
	Password string `json:"password"`
	Gender   int16  `json:"gender"`
	Email    string `json:"email"`
	Phone    string `json:"phone"`
}

type userImportPayload struct {
	OwnerUserID int64           `json:"ownerUserId"`
	DeptID      int64           `json:"deptId"`
	Rows        []userImportRow `json:"rows"`
}

type userImportReq struct {
	ImportKey      string `json:"importKey"`
	DuplicateUser  int    `json:"duplicateUser"`
	DuplicateEmail int    `json:"duplicateEmail"`
	DuplicatePhone int    `json:"duplicatePhone"`
	DefaultStatus  int16  `json:"defaultStatus"`
}

// SystemUserHandler provides /system/user endpoints.
type SystemUserHandler struct {
	db           *sql.DB
	tokenSvc     *security.TokenService
	rsaDecryptor *security.RSADecryptor
	hasher       security.PasswordHasher
	redis        *redis.Client
}

func NewSystemUserHandler(db *sql.DB, tokenSvc *security.TokenService, rsa *security.RSADecryptor, hasher security.PasswordHasher, redisClients ...*redis.Client) *SystemUserHandler {
	handler := &SystemUserHandler{
		db:           db,
		tokenSvc:     tokenSvc,
		rsaDecryptor: rsa,
		hasher:       hasher,
	}
	if len(redisClients) > 0 {
		handler.redis = redisClients[0]
	}
	return handler
}

// RegisterSystemUserRoutes registers /system/user related routes.
func (h *SystemUserHandler) RegisterSystemUserRoutes(r *gin.Engine) {
	r.GET("/system/user", h.ListUserPage)
	r.GET("/system/user/list", h.ListAllUser)
	r.GET("/system/user/:id", h.GetUserDetail)
	r.POST("/system/user", h.CreateUser)
	r.PUT("/system/user/:id", h.UpdateUser)
	r.DELETE("/system/user", h.DeleteUser)
	r.PATCH("/system/user/:id/password", h.ResetPassword)
	r.PATCH("/system/user/:id/role", h.UpdateUserRole)

	// 导出与导入相关接口
	r.GET("/system/user/export", h.ExportUser)
	r.GET("/system/user/import/template", h.DownloadImportTemplate)
	r.POST("/system/user/import/parse", h.ParseImportUser)
	r.POST("/system/user/import", h.ImportUser)
}

func (h *SystemUserHandler) currentUserID(c *gin.Context) int64 {
	return requireCurrentUser(c)
}

// ListUserPage handles GET /system/user (分页查询用户).
func (h *SystemUserHandler) ListUserPage(c *gin.Context) {
	page, _ := strconv.Atoi(c.Query("page"))
	size, _ := strconv.Atoi(c.Query("size"))
	if page <= 0 {
		page = 1
	}
	if size <= 0 {
		size = 10
	}

	desc := strings.TrimSpace(c.Query("description"))
	statusStr := strings.TrimSpace(c.Query("status"))
	deptStr := strings.TrimSpace(c.Query("deptId"))

	var (
		statusFilter int64
		deptID       int64
	)
	if statusStr != "" {
		statusFilter, _ = strconv.ParseInt(statusStr, 10, 64)
	}
	if deptStr != "" {
		deptID, _ = strconv.ParseInt(deptStr, 10, 64)
	}

	where := "WHERE 1=1"
	args := []any{}
	argPos := 1
	if desc != "" {
		where += fmt.Sprintf(" AND (u.username ILIKE $%d OR u.nickname ILIKE $%d OR COALESCE(u.description,'') ILIKE $%d)", argPos, argPos, argPos)
		args = append(args, "%"+desc+"%")
		argPos++
	}
	if statusFilter != 0 {
		where += fmt.Sprintf(" AND u.status = $%d", argPos)
		args = append(args, statusFilter)
		argPos++
	}
	if deptID != 0 {
		where += fmt.Sprintf(" AND u.dept_id = $%d", argPos)
		args = append(args, deptID)
		argPos++
	}
	principal, ok := CurrentPrincipal(c)
	if !ok {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	if err := appendUserDataScopeFilter(c.Request.Context(), h.db, principal, &where, &args, &argPos, "u"); err != nil {
		Fail(c, "500", "解析数据权限失败")
		return
	}

	countSQL := "SELECT COUNT(*) FROM sys_user AS u " + where
	var total int64
	if err := h.db.QueryRowContext(c.Request.Context(), countSQL, args...).Scan(&total); err != nil {
		Fail(c, "500", "查询用户失败")
		return
	}
	if total == 0 {
		OK(c, PageResult[UserResp]{List: []UserResp{}, Total: 0})
		return
	}

	offset := int64((page - 1) * size)
	limitPos := argPos
	offsetPos := argPos + 1
	argsWithPage := append(args, int64(size), offset)

	query := fmt.Sprintf(`
SELECT u.id,
       u.username,
       u.nickname,
       COALESCE(u.avatar, ''),
       u.gender,
       COALESCE(u.email, ''),
       COALESCE(u.phone, ''),
       COALESCE(u.description, ''),
       u.status,
       u.is_system,
       u.dept_id,
       COALESCE(d.name, ''),
       u.create_time,
       COALESCE(cu.nickname, ''),
       u.update_time,
       COALESCE(uu.nickname, '')
FROM sys_user AS u
LEFT JOIN sys_dept AS d ON d.id = u.dept_id
LEFT JOIN sys_user AS cu ON cu.id = u.create_user
LEFT JOIN sys_user AS uu ON uu.id = u.update_user
%s
ORDER BY u.id DESC
LIMIT $%d OFFSET $%d;
`, where, limitPos, offsetPos)

	rows, err := h.db.QueryContext(c.Request.Context(), query, argsWithPage...)
	if err != nil {
		Fail(c, "500", "查询用户失败")
		return
	}
	defer rows.Close()

	var users []UserResp
	for rows.Next() {
		var (
			u        UserResp
			createAt time.Time
			createBy string
			updateAt sql.NullTime
			updateBy string
		)
		if err := rows.Scan(
			&u.ID,
			&u.Username,
			&u.Nickname,
			&u.Avatar,
			&u.Gender,
			&u.Email,
			&u.Phone,
			&u.Description,
			&u.Status,
			&u.IsSystem,
			&u.DeptID,
			&u.DeptName,
			&createAt,
			&createBy,
			&updateAt,
			&updateBy,
		); err != nil {
			Fail(c, "500", "解析用户数据失败")
			return
		}
		u.CreateUserString = createBy
		u.CreateTime = formatTime(createAt)
		if updateAt.Valid {
			u.UpdateUserString = updateBy
			u.UpdateTime = formatTime(updateAt.Time)
		}
		u.Disabled = u.IsSystem
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		Fail(c, "500", "查询用户失败")
		return
	}

	// 填充角色信息
	if err := h.fillUserRoles(c, users); err != nil {
		Fail(c, "500", "查询用户角色失败")
		return
	}

	OK(c, PageResult[UserResp]{List: users, Total: total})
}

// ListAllUser handles GET /system/user/list.
func (h *SystemUserHandler) ListAllUser(c *gin.Context) {
	idStrs := c.QueryArray("userIds")
	var ids []int64
	for _, s := range idStrs {
		if v, err := strconv.ParseInt(s, 10, 64); err == nil && v > 0 {
			ids = append(ids, v)
		}
	}

	where := "WHERE 1=1"
	args := []any{}
	argPos := 1
	if len(ids) > 0 {
		where += fmt.Sprintf(" AND u.id = ANY($%d::bigint[])", argPos)
		args = append(args, pqInt64Array(ids))
		argPos++
	}
	principal, ok := CurrentPrincipal(c)
	if !ok {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	if err := appendUserDataScopeFilter(c.Request.Context(), h.db, principal, &where, &args, &argPos, "u"); err != nil {
		Fail(c, "500", "解析数据权限失败")
		return
	}

	query := fmt.Sprintf(`SELECT u.id,
       u.username,
       u.nickname,
       COALESCE(u.avatar, ''),
       u.gender,
       COALESCE(u.email, ''),
       COALESCE(u.phone, ''),
       COALESCE(u.description, ''),
       u.status,
       u.is_system,
       u.dept_id,
       COALESCE(d.name, ''),
       u.create_time,
       COALESCE(cu.nickname, ''),
       u.update_time,
       COALESCE(uu.nickname, '')
FROM sys_user AS u
LEFT JOIN sys_dept AS d ON d.id = u.dept_id
LEFT JOIN sys_user AS cu ON cu.id = u.create_user
LEFT JOIN sys_user AS uu ON uu.id = u.update_user
%s
ORDER BY u.id DESC`, where)
	rows, err := h.db.QueryContext(c.Request.Context(), query, args...)
	if err != nil {
		Fail(c, "500", "查询用户失败")
		return
	}
	defer rows.Close()

	var users []UserResp
	for rows.Next() {
		var (
			u        UserResp
			createAt time.Time
			createBy string
			updateAt sql.NullTime
			updateBy string
		)
		if err := rows.Scan(
			&u.ID,
			&u.Username,
			&u.Nickname,
			&u.Avatar,
			&u.Gender,
			&u.Email,
			&u.Phone,
			&u.Description,
			&u.Status,
			&u.IsSystem,
			&u.DeptID,
			&u.DeptName,
			&createAt,
			&createBy,
			&updateAt,
			&updateBy,
		); err != nil {
			Fail(c, "500", "解析用户数据失败")
			return
		}
		u.CreateUserString = createBy
		u.CreateTime = formatTime(createAt)
		if updateAt.Valid {
			u.UpdateUserString = updateBy
			u.UpdateTime = formatTime(updateAt.Time)
		}
		u.Disabled = u.IsSystem
		users = append(users, u)
	}
	if err := rows.Err(); err != nil {
		Fail(c, "500", "查询用户失败")
		return
	}

	if err := h.fillUserRoles(c, users); err != nil {
		Fail(c, "500", "查询用户角色失败")
		return
	}

	OK(c, users)
}

// fillUserRoles loads roleIds and roleNames for the given users.
func (h *SystemUserHandler) fillUserRoles(c *gin.Context, users []UserResp) error {
	if len(users) == 0 {
		return nil
	}
	userIDs := make([]int64, 0, len(users))
	seen := make(map[int64]struct{})
	for _, u := range users {
		if _, ok := seen[u.ID]; !ok {
			seen[u.ID] = struct{}{}
			userIDs = append(userIDs, u.ID)
		}
	}
	const query = `
SELECT ur.user_id, ur.role_id, r.name
FROM sys_user_role AS ur
JOIN sys_role AS r ON r.id = ur.role_id
WHERE ur.user_id = ANY($1::bigint[]);
`
	rows, err := h.db.QueryContext(c.Request.Context(), query, pqInt64Array(userIDs))
	if err != nil {
		return err
	}
	defer rows.Close()

	type info struct {
		ids   []int64
		names []string
	}
	roleMap := make(map[int64]*info)
	for rows.Next() {
		var uid, rid int64
		var name string
		if err := rows.Scan(&uid, &rid, &name); err != nil {
			return err
		}
		entry := roleMap[uid]
		if entry == nil {
			entry = &info{}
			roleMap[uid] = entry
		}
		entry.ids = append(entry.ids, rid)
		entry.names = append(entry.names, name)
	}
	for i := range users {
		if entry := roleMap[users[i].ID]; entry != nil {
			users[i].RoleIDs = entry.ids
			users[i].RoleNames = entry.names
		}
	}
	if err := rows.Err(); err != nil {
		return err
	}
	return nil
}

// GetUserDetail handles GET /system/user/:id.
func (h *SystemUserHandler) GetUserDetail(c *gin.Context) {
	idVal, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || idVal <= 0 {
		Fail(c, "400", "ID 参数不正确")
		return
	}

	where := "WHERE u.id = $1"
	args := []any{idVal}
	argPos := 2
	principal, ok := CurrentPrincipal(c)
	if !ok {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	if err := appendUserDataScopeFilter(c.Request.Context(), h.db, principal, &where, &args, &argPos, "u"); err != nil {
		Fail(c, "500", "解析数据权限失败")
		return
	}

	query := fmt.Sprintf(`
SELECT u.id,
       u.username,
       u.nickname,
       COALESCE(u.avatar, ''),
       u.gender,
       COALESCE(u.email, ''),
       COALESCE(u.phone, ''),
       COALESCE(u.description, ''),
       u.status,
       u.is_system,
       u.dept_id,
       COALESCE(d.name, ''),
       u.pwd_reset_time,
       u.create_time,
       COALESCE(cu.nickname, ''),
       u.update_time,
       COALESCE(uu.nickname, '')
FROM sys_user AS u
LEFT JOIN sys_dept AS d ON d.id = u.dept_id
LEFT JOIN sys_user AS cu ON cu.id = u.create_user
LEFT JOIN sys_user AS uu ON uu.id = u.update_user
%s;
`, where)
	var (
		u        UserDetailResp
		pwdReset sql.NullTime
		createAt time.Time
		createBy string
		updateAt sql.NullTime
		updateBy string
	)
	err = h.db.QueryRowContext(c.Request.Context(), query, args...).
		Scan(
			&u.ID,
			&u.Username,
			&u.Nickname,
			&u.Avatar,
			&u.Gender,
			&u.Email,
			&u.Phone,
			&u.Description,
			&u.Status,
			&u.IsSystem,
			&u.DeptID,
			&u.DeptName,
			&pwdReset,
			&createAt,
			&createBy,
			&updateAt,
			&updateBy,
		)
	if err == sql.ErrNoRows {
		Fail(c, "404", "用户不存在")
		return
	}
	if err != nil {
		Fail(c, "500", "查询用户失败")
		return
	}
	u.CreateUserString = createBy
	u.CreateTime = formatTime(createAt)
	if updateAt.Valid {
		u.UpdateUserString = updateBy
		u.UpdateTime = formatTime(updateAt.Time)
	}
	u.Disabled = u.IsSystem
	if pwdReset.Valid {
		u.PwdResetTime = formatTime(pwdReset.Time)
	}

	// 角色信息
	roleUsers := []UserResp{u.UserResp}
	if err := h.fillUserRoles(c, roleUsers); err != nil {
		Fail(c, "500", "查询用户角色失败")
		return
	}
	if len(roleUsers) > 0 {
		u.RoleIDs = roleUsers[0].RoleIDs
		u.RoleNames = roleUsers[0].RoleNames
	}

	OK(c, u)
}

// CreateUser handles POST /system/user.
func (h *SystemUserHandler) CreateUser(c *gin.Context) {
	userID := h.currentUserID(c)
	if userID == 0 {
		return
	}

	var req userReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, "400", "请求参数不正确")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Nickname = strings.TrimSpace(req.Nickname)
	if req.Username == "" || req.Nickname == "" {
		Fail(c, "400", "用户名和昵称不能为空")
		return
	}
	if req.DeptID == 0 {
		Fail(c, "400", "所属部门不能为空")
		return
	}
	if req.Status == 0 {
		req.Status = 1
	}
	if strings.TrimSpace(req.Password) == "" {
		Fail(c, "400", "密码不能为空")
		return
	}
	principal, ok := CurrentPrincipal(c)
	if !ok {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	canUseDept, err := principalCanUseDept(c.Request.Context(), h.db, principal, req.DeptID)
	if err != nil {
		Fail(c, "500", "校验数据权限失败")
		return
	}
	if !canUseDept {
		Fail(c, "403", "没有该部门的数据权限")
		return
	}
	req.RoleIDs = normalizeInt64IDs(req.RoleIDs)
	canGrantRoles, err := principalCanGrantRoles(c.Request.Context(), h.db, principal, req.RoleIDs)
	if err != nil {
		Fail(c, "500", "校验角色权限失败")
		return
	}
	if !canGrantRoles {
		Fail(c, "403", "没有授予所选角色的权限")
		return
	}

	rawPwd, err := h.rsaDecryptor.DecryptBase64(req.Password)
	if err != nil {
		Fail(c, "400", "密码解密失败")
		return
	}
	passwordPolicy, err := loadPasswordPolicy(c.Request.Context(), h.db)
	if err != nil {
		Fail(c, "500", "读取密码策略失败")
		return
	}
	if err := validatePassword(rawPwd, req.Username, passwordPolicy); err != nil {
		Fail(c, "400", err.Error())
		return
	}

	encodedPwd, err := h.hasher.Hash(rawPwd)
	if err != nil {
		Fail(c, "500", "密码加密失败")
		return
	}

	now := time.Now()
	idVal := id.Next()

	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		Fail(c, "500", "新增用户失败")
		return
	}
	defer tx.Rollback()

	const insertUser = `
INSERT INTO sys_user (
    id, username, nickname, password, gender, email, phone, avatar,
    description, status, is_system, pwd_reset_time, dept_id,
    create_user, create_time
)
VALUES ($1, $2, $3, $4, $5, $6, $7, $8,
        $9, $10, FALSE, $11, $12,
        $13, $14);
`
	if _, err := tx.ExecContext(
		c.Request.Context(),
		insertUser,
		idVal,
		req.Username,
		req.Nickname,
		encodedPwd,
		req.Gender,
		req.Email,
		req.Phone,
		req.Avatar,
		req.Description,
		req.Status,
		now,
		req.DeptID,
		userID,
		now,
	); err != nil {
		Fail(c, "500", "新增用户失败")
		return
	}

	if len(req.RoleIDs) > 0 {
		const insertUserRole = `
INSERT INTO sys_user_role (id, user_id, role_id)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, role_id) DO NOTHING;
`
		for _, rid := range req.RoleIDs {
			if _, err := tx.ExecContext(c.Request.Context(), insertUserRole, id.Next(), idVal, rid); err != nil {
				Fail(c, "500", "保存用户角色失败")
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		Fail(c, "500", "新增用户失败")
		return
	}
	OK(c, gin.H{"id": idVal})
}

// UpdateUser handles PUT /system/user/:id.
func (h *SystemUserHandler) UpdateUser(c *gin.Context) {
	userID := h.currentUserID(c)
	if userID == 0 {
		return
	}
	idVal, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || idVal <= 0 {
		Fail(c, "400", "ID 参数不正确")
		return
	}
	if !ensureSystemUserMutable(c, h.db, idVal, "修改") {
		return
	}

	var req userReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, "400", "请求参数不正确")
		return
	}
	req.Username = strings.TrimSpace(req.Username)
	req.Nickname = strings.TrimSpace(req.Nickname)
	if req.Username == "" || req.Nickname == "" {
		Fail(c, "400", "用户名和昵称不能为空")
		return
	}
	if req.DeptID == 0 {
		Fail(c, "400", "所属部门不能为空")
		return
	}
	if req.Status == 0 {
		req.Status = 1
	}
	principal, ok := CurrentPrincipal(c)
	if !ok {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	canAccessUser, err := principalCanAccessUser(c.Request.Context(), h.db, principal, idVal)
	if err != nil {
		Fail(c, "500", "校验数据权限失败")
		return
	}
	if !canAccessUser {
		Fail(c, "403", "没有该用户的数据权限")
		return
	}
	canUseDept, err := principalCanUseDept(c.Request.Context(), h.db, principal, req.DeptID)
	if err != nil {
		Fail(c, "500", "校验数据权限失败")
		return
	}
	if !canUseDept {
		Fail(c, "403", "没有该部门的数据权限")
		return
	}

	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		Fail(c, "500", "修改用户失败")
		return
	}
	defer tx.Rollback()

	const updateUser = `
UPDATE sys_user
   SET username    = $1,
       nickname    = $2,
       gender      = $3,
       email       = $4,
       phone       = $5,
       avatar      = $6,
       description = $7,
       status      = $8,
       dept_id     = $9,
       update_user = $10,
       update_time = $11
 WHERE id          = $12;
`
	if _, err := tx.ExecContext(
		c.Request.Context(),
		updateUser,
		req.Username,
		req.Nickname,
		req.Gender,
		req.Email,
		req.Phone,
		req.Avatar,
		req.Description,
		req.Status,
		req.DeptID,
		userID,
		time.Now(),
		idVal,
	); err != nil {
		Fail(c, "500", "修改用户失败")
		return
	}

	if err := tx.Commit(); err != nil {
		Fail(c, "500", "修改用户失败")
		return
	}
	OK(c, true)
}

// DeleteUser handles DELETE /system/user.
func (h *SystemUserHandler) DeleteUser(c *gin.Context) {
	userID := h.currentUserID(c)
	if userID == 0 {
		return
	}
	_ = userID

	var req idsRequest
	if err := c.ShouldBindJSON(&req); err != nil || len(req.IDs) == 0 {
		Fail(c, "400", "ID 列表不能为空")
		return
	}
	for _, idVal := range req.IDs {
		if !ensureSystemUserMutable(c, h.db, idVal, "删除") {
			return
		}
	}
	principal, ok := CurrentPrincipal(c)
	if !ok {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	canAccessUsers, err := principalCanAccessUsers(c.Request.Context(), h.db, principal, req.IDs)
	if err != nil {
		Fail(c, "500", "校验数据权限失败")
		return
	}
	if !canAccessUsers {
		Fail(c, "403", "没有该用户的数据权限")
		return
	}

	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		Fail(c, "500", "删除用户失败")
		return
	}
	defer tx.Rollback()

	for _, idVal := range req.IDs {
		// 不允许删除系统内置用户
		var isSystem bool
		if err := tx.QueryRowContext(c.Request.Context(), `SELECT is_system FROM sys_user WHERE id = $1`, idVal).Scan(&isSystem); err != nil {
			if err == sql.ErrNoRows {
				continue
			}
			Fail(c, "500", "删除用户失败")
			return
		}
		if isSystem {
			continue
		}
		if _, err := tx.ExecContext(c.Request.Context(), `DELETE FROM sys_user_role WHERE user_id = $1`, idVal); err != nil {
			Fail(c, "500", "删除用户失败")
			return
		}
		if _, err := tx.ExecContext(c.Request.Context(), `DELETE FROM sys_user WHERE id = $1`, idVal); err != nil {
			Fail(c, "500", "删除用户失败")
			return
		}
	}

	if err := tx.Commit(); err != nil {
		Fail(c, "500", "删除用户失败")
		return
	}
	OK(c, true)
}

// ResetPassword handles PATCH /system/user/:id/password.
func (h *SystemUserHandler) ResetPassword(c *gin.Context) {
	userID := h.currentUserID(c)
	if userID == 0 {
		return
	}
	idVal, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || idVal <= 0 {
		Fail(c, "400", "ID 参数不正确")
		return
	}
	if !ensureSystemUserMutable(c, h.db, idVal, "重置密码") {
		return
	}
	principal, ok := CurrentPrincipal(c)
	if !ok {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	canAccessUser, err := principalCanAccessUser(c.Request.Context(), h.db, principal, idVal)
	if err != nil {
		Fail(c, "500", "校验数据权限失败")
		return
	}
	if !canAccessUser {
		Fail(c, "403", "没有该用户的数据权限")
		return
	}

	var req userPasswordResetReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, "400", "请求参数不正确")
		return
	}
	if strings.TrimSpace(req.NewPassword) == "" {
		Fail(c, "400", "密码不能为空")
		return
	}

	rawPwd, err := h.rsaDecryptor.DecryptBase64(req.NewPassword)
	if err != nil {
		Fail(c, "400", "密码解密失败")
		return
	}
	var targetUsername, currentHash string
	if err := h.db.QueryRowContext(
		c.Request.Context(),
		`SELECT username, COALESCE(password, '') FROM sys_user WHERE id = $1 LIMIT 1`,
		idVal,
	).Scan(&targetUsername, &currentHash); err != nil {
		if err == sql.ErrNoRows {
			Fail(c, "404", "用户不存在")
			return
		}
		Fail(c, "500", "查询用户失败")
		return
	}
	passwordPolicy, err := loadPasswordPolicy(c.Request.Context(), h.db)
	if err != nil {
		Fail(c, "500", "读取密码策略失败")
		return
	}
	if err := validatePassword(rawPwd, targetUsername, passwordPolicy); err != nil {
		Fail(c, "400", err.Error())
		return
	}
	used, err := passwordRecentlyUsed(
		c.Request.Context(), h.db, security.BcryptVerifier{}, idVal, rawPwd, currentHash, passwordPolicy.RepetitionTimes,
	)
	if err != nil {
		Fail(c, "500", "校验历史密码失败")
		return
	}
	if used {
		Fail(c, "400", fmt.Sprintf("不能使用最近 %d 次使用过的密码", passwordPolicy.RepetitionTimes))
		return
	}

	encodedPwd, err := h.hasher.Hash(rawPwd)
	if err != nil {
		Fail(c, "500", "密码加密失败")
		return
	}

	if err := updatePasswordWithHistory(
		c.Request.Context(), h.db, idVal, encodedPwd, currentHash, userID, passwordPolicy.RepetitionTimes,
	); err != nil {
		Fail(c, "500", "重置密码失败")
		return
	}
	OK(c, true)
}

// UpdateUserRole handles PATCH /system/user/:id/role.
func (h *SystemUserHandler) UpdateUserRole(c *gin.Context) {
	userID := h.currentUserID(c)
	if userID == 0 {
		return
	}

	idVal, err := strconv.ParseInt(c.Param("id"), 10, 64)
	if err != nil || idVal <= 0 {
		Fail(c, "400", "ID 参数不正确")
		return
	}
	if !CurrentUserIsAdmin(c) && idVal == userID {
		Fail(c, "400", "不能修改自己的角色")
		return
	}
	if !ensureSystemUserMutable(c, h.db, idVal, "分配角色") {
		return
	}
	principal, ok := CurrentPrincipal(c)
	if !ok {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	canAccessUser, err := principalCanAccessUser(c.Request.Context(), h.db, principal, idVal)
	if err != nil {
		Fail(c, "500", "校验数据权限失败")
		return
	}
	if !canAccessUser {
		Fail(c, "403", "没有该用户的数据权限")
		return
	}

	var req userRoleUpdateReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, "400", "请求参数不正确")
		return
	}
	req.RoleIDs = normalizeInt64IDs(req.RoleIDs)
	canGrantRoles, err := principalCanGrantRoles(c.Request.Context(), h.db, principal, req.RoleIDs)
	if err != nil {
		Fail(c, "500", "校验角色权限失败")
		return
	}
	if !canGrantRoles {
		Fail(c, "403", "没有授予所选角色的权限")
		return
	}

	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		Fail(c, "500", "分配角色失败")
		return
	}
	defer tx.Rollback()

	if _, err := tx.ExecContext(c.Request.Context(), `DELETE FROM sys_user_role WHERE user_id = $1`, idVal); err != nil {
		Fail(c, "500", "分配角色失败")
		return
	}
	if len(req.RoleIDs) > 0 {
		const insertUserRole = `
INSERT INTO sys_user_role (id, user_id, role_id)
VALUES ($1, $2, $3)
ON CONFLICT (user_id, role_id) DO NOTHING;
`
		for _, rid := range req.RoleIDs {
			if _, err := tx.ExecContext(c.Request.Context(), insertUserRole, id.Next(), idVal, rid); err != nil {
				Fail(c, "500", "分配角色失败")
				return
			}
		}
	}

	if err := tx.Commit(); err != nil {
		Fail(c, "500", "分配角色失败")
		return
	}
	OK(c, true)
}

// ExportUser handles GET /system/user/export.
// It returns a very simple CSV file with basic user information.
func (h *SystemUserHandler) ExportUser(c *gin.Context) {
	where := "WHERE 1=1"
	args := []any{}
	argPos := 1
	principal, ok := CurrentPrincipal(c)
	if !ok {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	if err := appendUserDataScopeFilter(c.Request.Context(), h.db, principal, &where, &args, &argPos, "u"); err != nil {
		Fail(c, "500", "解析数据权限失败")
		return
	}
	query := fmt.Sprintf(`SELECT username, nickname, gender, COALESCE(email,''), COALESCE(phone,'') FROM sys_user AS u %s ORDER BY id`, where)
	rows, err := h.db.QueryContext(c.Request.Context(), query, args...)
	if err != nil {
		Fail(c, "500", "导出用户失败")
		return
	}
	defer rows.Close()

	var b strings.Builder
	writer := csv.NewWriter(&b)
	if err := writer.Write([]string{"username", "nickname", "gender", "email", "phone"}); err != nil {
		Fail(c, "500", "导出用户失败")
		return
	}
	for rows.Next() {
		var username, nickname, email, phone string
		var gender int16
		if err := rows.Scan(&username, &nickname, &gender, &email, &phone); err != nil {
			Fail(c, "500", "导出用户失败")
			return
		}
		if err := writer.Write([]string{
			safeCSVCell(username),
			safeCSVCell(nickname),
			strconv.FormatInt(int64(gender), 10),
			safeCSVCell(email),
			safeCSVCell(phone),
		}); err != nil {
			Fail(c, "500", "导出用户失败")
			return
		}
	}
	if err := rows.Err(); err != nil {
		Fail(c, "500", "导出用户失败")
		return
	}
	writer.Flush()
	if err := writer.Error(); err != nil {
		Fail(c, "500", "导出用户失败")
		return
	}

	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\"users.csv\"")
	c.String(http.StatusOK, b.String())
}

func safeCSVCell(value string) string {
	if value == "" {
		return value
	}
	switch value[0] {
	case '=', '+', '-', '@':
		return "'" + value
	default:
		return value
	}
}

// DownloadImportTemplate handles GET /system/user/import/template.
func (h *SystemUserHandler) DownloadImportTemplate(c *gin.Context) {
	content := "username,nickname,password,gender,email,phone\nexample,示例用户,Example123!,1,example@example.com,13800000000\n"
	c.Header("Content-Type", "text/csv; charset=utf-8")
	c.Header("Content-Disposition", "attachment; filename=\"user_import_template.csv\"")
	c.String(http.StatusOK, "\ufeff"+content)
}

// ParseImportUser handles POST /system/user/import/parse.
func (h *SystemUserHandler) ParseImportUser(c *gin.Context) {
	principal, ok := CurrentPrincipal(c)
	if !ok {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	if h.redis == nil {
		Fail(c, "500", "导入缓存服务未初始化")
		return
	}
	fileHeader, err := c.FormFile("file")
	if err != nil {
		Fail(c, "400", "文件不能为空")
		return
	}
	if !strings.EqualFold(strings.TrimSpace(strings.TrimPrefix(filepathExt(fileHeader.Filename), ".")), "csv") {
		Fail(c, "400", "仅支持 CSV 文件")
		return
	}
	file, err := fileHeader.Open()
	if err != nil {
		Fail(c, "400", "读取导入文件失败")
		return
	}
	defer file.Close()

	rows, err := parseUserImportCSV(file)
	if err != nil {
		Fail(c, "400", err.Error())
		return
	}
	if len(rows) == 0 {
		Fail(c, "400", "导入文件没有数据")
		return
	}
	if len(rows) > 5000 {
		Fail(c, "400", "单次最多导入 5000 行")
		return
	}
	policy, err := loadPasswordPolicy(c.Request.Context(), h.db)
	if err != nil {
		Fail(c, "500", "读取密码策略失败")
		return
	}

	resp := UserImportParseResp{TotalRows: len(rows), ValidRows: len(rows)}
	for index, row := range rows {
		if err := validateUserImportRow(row, policy); err != nil {
			Fail(c, "400", fmt.Sprintf("第 %d 行：%v", index+2, err))
			return
		}
		var usernameExists, emailExists, phoneExists bool
		if err := h.db.QueryRowContext(c.Request.Context(), `
SELECT
    EXISTS (SELECT 1 FROM sys_user WHERE username = $1),
    ($2 <> '' AND EXISTS (SELECT 1 FROM sys_user WHERE email = $2)),
    ($3 <> '' AND EXISTS (SELECT 1 FROM sys_user WHERE phone = $3))`, row.Username, row.Email, row.Phone).
			Scan(&usernameExists, &emailExists, &phoneExists); err != nil {
			Fail(c, "500", "校验导入数据失败")
			return
		}
		if usernameExists {
			resp.DuplicateUserRows++
		}
		if emailExists {
			resp.DuplicateEmailRows++
		}
		if phoneExists {
			resp.DuplicatePhoneRows++
		}
	}

	payloadBytes, err := json.Marshal(userImportPayload{OwnerUserID: principal.UserID, DeptID: principal.DeptID, Rows: rows})
	if err != nil {
		Fail(c, "500", "保存导入数据失败")
		return
	}
	resp.ImportKey = strconv.FormatInt(id.Next(), 10)
	if err := h.redis.Set(c.Request.Context(), userImportRedisKey(resp.ImportKey), payloadBytes, 15*time.Minute).Err(); err != nil {
		Fail(c, "500", "保存导入数据失败")
		return
	}
	OK(c, resp)
}

// ImportUser handles POST /system/user/import.
func (h *SystemUserHandler) ImportUser(c *gin.Context) {
	principal, ok := CurrentPrincipal(c)
	if !ok {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	if h.redis == nil {
		Fail(c, "500", "导入缓存服务未初始化")
		return
	}
	var req userImportReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, "400", "请求参数不正确")
		return
	}
	if strings.TrimSpace(req.ImportKey) == "" {
		Fail(c, "400", "请先解析导入文件")
		return
	}
	if req.DuplicateUser < 1 || req.DuplicateUser > 3 || req.DuplicateEmail != 1 && req.DuplicateEmail != 3 || req.DuplicatePhone != 1 && req.DuplicatePhone != 3 {
		Fail(c, "400", "导入策略不正确")
		return
	}
	if req.DefaultStatus != 1 && req.DefaultStatus != 2 {
		req.DefaultStatus = 1
	}
	payloadBytes, err := h.redis.Get(c.Request.Context(), userImportRedisKey(req.ImportKey)).Bytes()
	if err == redis.Nil {
		Fail(c, "400", "导入数据已过期，请重新上传")
		return
	}
	if err != nil {
		Fail(c, "500", "读取导入数据失败")
		return
	}
	var payload userImportPayload
	if err := json.Unmarshal(payloadBytes, &payload); err != nil || payload.OwnerUserID != principal.UserID {
		Fail(c, "403", "无权使用该导入数据")
		return
	}
	if payload.DeptID == 0 {
		Fail(c, "400", "当前用户未配置所属部门")
		return
	}

	tx, err := h.db.BeginTx(c.Request.Context(), nil)
	if err != nil {
		Fail(c, "500", "开始导入失败")
		return
	}
	defer tx.Rollback()
	resp := UserImportResultResp{TotalRows: len(payload.Rows)}
	for rowIndex, row := range payload.Rows {
		usernameID, err := queryImportedUserID(c.Request.Context(), tx, "username", row.Username)
		if err != nil {
			Fail(c, "500", "校验用户名失败")
			return
		}
		emailID, err := queryImportedUserID(c.Request.Context(), tx, "email", row.Email)
		if err != nil {
			Fail(c, "500", "校验邮箱失败")
			return
		}
		phoneID, err := queryImportedUserID(c.Request.Context(), tx, "phone", row.Phone)
		if err != nil {
			Fail(c, "500", "校验手机号失败")
			return
		}
		if emailID != 0 && emailID != usernameID {
			if req.DuplicateEmail == 3 {
				Fail(c, "409", fmt.Sprintf("第 %d 行邮箱已存在", rowIndex+2))
				return
			}
			continue
		}
		if phoneID != 0 && phoneID != usernameID {
			if req.DuplicatePhone == 3 {
				Fail(c, "409", fmt.Sprintf("第 %d 行手机号已存在", rowIndex+2))
				return
			}
			continue
		}
		if usernameID != 0 {
			switch req.DuplicateUser {
			case 1:
				continue
			case 3:
				Fail(c, "409", fmt.Sprintf("第 %d 行用户名已存在", rowIndex+2))
				return
			case 2:
				if _, err := tx.ExecContext(c.Request.Context(), `
UPDATE sys_user
SET nickname = $1, gender = $2, email = NULLIF($3, ''), phone = NULLIF($4, ''),
    status = $5, update_user = $6, update_time = $7
WHERE id = $8`, row.Nickname, row.Gender, row.Email, row.Phone, req.DefaultStatus, principal.UserID, time.Now(), usernameID); err != nil {
					Fail(c, "500", "更新导入用户失败")
					return
				}
				resp.UpdateRows++
				continue
			}
		}

		encodedPassword, err := h.hasher.Hash(row.Password)
		if err != nil {
			Fail(c, "500", "加密导入用户密码失败")
			return
		}
		now := time.Now()
		if _, err := tx.ExecContext(c.Request.Context(), `
INSERT INTO sys_user (
    id, username, nickname, password, gender, email, phone, status,
    is_system, pwd_reset_time, dept_id, create_user, create_time
) VALUES ($1, $2, $3, $4, $5, NULLIF($6, ''), NULLIF($7, ''), $8,
          FALSE, $9, $10, $11, $12)`,
			id.Next(), row.Username, row.Nickname, encodedPassword, row.Gender, row.Email, row.Phone,
			req.DefaultStatus, now, payload.DeptID, principal.UserID, now,
		); err != nil {
			Fail(c, "409", fmt.Sprintf("第 %d 行数据冲突，导入已取消", rowIndex+2))
			return
		}
		resp.InsertRows++
	}
	if err := tx.Commit(); err != nil {
		Fail(c, "500", "提交导入失败")
		return
	}
	_ = h.redis.Del(c.Request.Context(), userImportRedisKey(req.ImportKey)).Err()
	OK(c, resp)
}

func userImportRedisKey(importKey string) string {
	return "USER_IMPORT:" + strings.TrimSpace(importKey)
}

func filepathExt(filename string) string {
	filename = strings.TrimSpace(filename)
	if index := strings.LastIndex(filename, "."); index >= 0 {
		return filename[index:]
	}
	return ""
}

func parseUserImportCSV(reader io.Reader) ([]userImportRow, error) {
	csvReader := csv.NewReader(reader)
	csvReader.TrimLeadingSpace = true
	csvReader.FieldsPerRecord = -1
	records, err := csvReader.ReadAll()
	if err != nil {
		return nil, errors.New("CSV 文件格式不正确")
	}
	if len(records) == 0 {
		return nil, errors.New("CSV 文件为空")
	}
	headers := records[0]
	if len(headers) > 0 {
		headers[0] = strings.TrimPrefix(headers[0], "\ufeff")
	}
	expected := []string{"username", "nickname", "password", "gender", "email", "phone"}
	if len(headers) != len(expected) {
		return nil, errors.New("CSV 表头不正确，请使用最新模板")
	}
	for index := range expected {
		if !strings.EqualFold(strings.TrimSpace(headers[index]), expected[index]) {
			return nil, errors.New("CSV 表头不正确，请使用最新模板")
		}
	}

	rows := make([]userImportRow, 0, len(records)-1)
	for index, record := range records[1:] {
		if len(record) == 1 && strings.TrimSpace(record[0]) == "" {
			continue
		}
		if len(record) != len(expected) {
			return nil, fmt.Errorf("第 %d 行列数不正确", index+2)
		}
		gender, err := strconv.ParseInt(strings.TrimSpace(record[3]), 10, 16)
		if err != nil {
			return nil, fmt.Errorf("第 %d 行性别不正确", index+2)
		}
		rows = append(rows, userImportRow{
			Username: strings.TrimSpace(record[0]),
			Nickname: strings.TrimSpace(record[1]),
			Password: strings.TrimSpace(record[2]),
			Gender:   int16(gender),
			Email:    strings.TrimSpace(record[4]),
			Phone:    strings.TrimSpace(record[5]),
		})
	}
	return rows, nil
}

func validateUserImportRow(row userImportRow, policy passwordPolicy) error {
	if row.Username == "" || len([]rune(row.Username)) > 64 {
		return errors.New("用户名不能为空且不能超过 64 个字符")
	}
	if row.Nickname == "" || len([]rune(row.Nickname)) > 30 {
		return errors.New("昵称不能为空且不能超过 30 个字符")
	}
	if row.Gender < 0 || row.Gender > 2 {
		return errors.New("性别只能为 0、1 或 2")
	}
	if row.Email != "" {
		address, err := mail.ParseAddress(row.Email)
		if err != nil || !strings.EqualFold(address.Address, row.Email) {
			return errors.New("邮箱格式不正确")
		}
	}
	if len(row.Phone) > 32 {
		return errors.New("手机号不能超过 32 个字符")
	}
	return validatePassword(row.Password, row.Username, policy)
}

func queryImportedUserID(ctx context.Context, tx *sql.Tx, column, value string) (int64, error) {
	if strings.TrimSpace(value) == "" {
		return 0, nil
	}
	var userID int64
	err := tx.QueryRowContext(ctx, `SELECT id FROM sys_user WHERE `+column+` = $1 LIMIT 1`, value).Scan(&userID)
	if errors.Is(err, sql.ErrNoRows) {
		return 0, nil
	}
	return userID, err
}
