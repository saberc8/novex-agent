package http

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"

	"voc-go-backend/internal/infrastructure/id"
	"voc-go-backend/internal/infrastructure/security"
)

type ClientAuthHandler struct {
	db       *sql.DB
	tokenSvc *security.TokenService
	hasher   security.PasswordHasher
	verifier security.PasswordVerifier
}

type clientRegisterReq struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
	Nickname string `json:"nickname"`
	ClientID string `json:"clientId"`
}

type clientLoginReq struct {
	Phone    string `json:"phone"`
	Password string `json:"password"`
	ClientID string `json:"clientId"`
}

type clientLoginResp struct {
	Token    string `json:"token"`
	UserID   int64  `json:"userId"`
	Phone    string `json:"phone"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
}

type clientProfileResp struct {
	ID       int64  `json:"id"`
	Phone    string `json:"phone"`
	Nickname string `json:"nickname"`
	Avatar   string `json:"avatar"`
}

type clientUserRecord struct {
	ID       int64
	Phone    string
	Password string
	Nickname string
	Avatar   string
	Status   int16
}

func NewClientAuthHandler(
	db *sql.DB,
	tokenSvc *security.TokenService,
	hasher security.PasswordHasher,
	verifier security.PasswordVerifier,
) *ClientAuthHandler {
	return &ClientAuthHandler{
		db:       db,
		tokenSvc: tokenSvc,
		hasher:   hasher,
		verifier: verifier,
	}
}

func (h *ClientAuthHandler) RegisterPublicClientRoutes(r gin.IRoutes) {
	r.POST("/client/auth/register", h.Register)
	r.POST("/client/auth/login", h.Login)
}

func (h *ClientAuthHandler) RegisterProtectedClientRoutes(r gin.IRoutes) {
	r.POST("/client/auth/logout", h.Logout)
	r.GET("/client/user/profile", h.Profile)
}

func (h *ClientAuthHandler) Register(c *gin.Context) {
	var req clientRegisterReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, "400", "参数缺失或格式不正确")
		return
	}
	req.Phone = strings.TrimSpace(req.Phone)
	req.Password = strings.TrimSpace(req.Password)
	req.Nickname = strings.TrimSpace(req.Nickname)
	req.ClientID = strings.TrimSpace(req.ClientID)
	if err := validateClientRegisterReq(req); err != nil {
		Fail(c, "400", err.Error())
		return
	}
	policy, err := loadClientSessionPolicy(c.Request.Context(), h.db, req.ClientID, "H5")
	if err != nil {
		Fail(c, "400", "客户端不可用")
		return
	}
	existing, err := h.findClientUserByPhone(c, req.Phone)
	if err != nil {
		Fail(c, "500", "查询用户失败")
		return
	}
	if existing != nil {
		Fail(c, "400", "手机号已注册")
		return
	}
	encoded, err := h.hasher.Hash(req.Password)
	if err != nil {
		Fail(c, "500", "密码加密失败")
		return
	}
	userID := id.Next()
	const insertSQL = `
INSERT INTO client_user (
    id, phone, password, nickname, avatar, status, create_time
) VALUES ($1, $2, $3, $4, $5, $6, NOW());
`
	if _, err := h.db.ExecContext(c.Request.Context(), insertSQL, userID, req.Phone, encoded, req.Nickname, "", int16(1)); err != nil {
		Fail(c, "500", "注册失败")
		return
	}
	token, err := h.tokenSvc.GenerateForSubjectWithTTL(userID, security.SubjectClient, policy.TokenTTL)
	if err != nil {
		Fail(c, "500", "生成登录令牌失败")
		return
	}
	if err := h.tokenSvc.RegisterSession(c.Request.Context(), token, policy.ActiveTimeout); err != nil {
		Fail(c, "500", "创建登录会话失败")
		return
	}
	OK(c, clientLoginResp{
		Token:    token,
		UserID:   userID,
		Phone:    req.Phone,
		Nickname: req.Nickname,
	})
}

func (h *ClientAuthHandler) Login(c *gin.Context) {
	var req clientLoginReq
	if err := c.ShouldBindJSON(&req); err != nil {
		Fail(c, "400", "参数缺失或格式不正确")
		return
	}
	req.Phone = strings.TrimSpace(req.Phone)
	req.Password = strings.TrimSpace(req.Password)
	req.ClientID = strings.TrimSpace(req.ClientID)
	if err := validateClientLoginReq(req); err != nil {
		Fail(c, "400", err.Error())
		return
	}
	policy, err := loadClientSessionPolicy(c.Request.Context(), h.db, req.ClientID, "H5")
	if err != nil {
		Fail(c, "400", "客户端不可用")
		return
	}
	user, err := h.findClientUserByPhone(c, req.Phone)
	if err != nil {
		Fail(c, "500", "查询用户失败")
		return
	}
	if user == nil {
		Fail(c, "400", "手机号或密码不正确")
		return
	}
	if user.Status != 1 {
		Fail(c, "400", "账号已被禁用")
		return
	}
	ok, err := h.verifier.Verify(req.Password, user.Password)
	if err != nil {
		Fail(c, "500", "密码校验失败")
		return
	}
	if !ok {
		Fail(c, "400", "手机号或密码不正确")
		return
	}
	const updateLoginSQL = `UPDATE client_user SET last_login_time = NOW(), update_time = NOW() WHERE id = $1;`
	_, _ = h.db.ExecContext(c.Request.Context(), updateLoginSQL, user.ID)
	token, err := h.tokenSvc.GenerateForSubjectWithTTL(user.ID, security.SubjectClient, policy.TokenTTL)
	if err != nil {
		Fail(c, "500", "生成登录令牌失败")
		return
	}
	if err := h.tokenSvc.RegisterSession(c.Request.Context(), token, policy.ActiveTimeout); err != nil {
		Fail(c, "500", "创建登录会话失败")
		return
	}
	OK(c, clientLoginResp{
		Token:    token,
		UserID:   user.ID,
		Phone:    user.Phone,
		Nickname: user.Nickname,
		Avatar:   user.Avatar,
	})
}

func (h *ClientAuthHandler) Logout(c *gin.Context) {
	if err := h.tokenSvc.RevokeSession(c.Request.Context(), c.GetHeader("Authorization")); err != nil {
		Fail(c, "500", "退出登录失败")
		return
	}
	OK(c, true)
}

func (h *ClientAuthHandler) Profile(c *gin.Context) {
	principal, ok := CurrentClientPrincipal(c)
	if !ok {
		Fail(c, "401", "未授权，请重新登录")
		return
	}
	OK(c, clientProfileResp{
		ID:       principal.UserID,
		Phone:    principal.Phone,
		Nickname: principal.Nickname,
		Avatar:   principal.Avatar,
	})
}

func validateClientRegisterReq(req clientRegisterReq) error {
	if req.ClientID == "" {
		return errors.New("客户端ID不能为空")
	}
	if req.Phone == "" {
		return errors.New("手机号不能为空")
	}
	if len(req.Password) < 6 {
		return errors.New("密码长度不能小于 6 位")
	}
	if req.Nickname == "" {
		return errors.New("昵称不能为空")
	}
	return nil
}

func validateClientLoginReq(req clientLoginReq) error {
	if req.ClientID == "" {
		return errors.New("客户端ID不能为空")
	}
	if req.Phone == "" {
		return errors.New("手机号不能为空")
	}
	if req.Password == "" {
		return errors.New("密码不能为空")
	}
	return nil
}

func (h *ClientAuthHandler) findClientUserByPhone(c *gin.Context, phone string) (*clientUserRecord, error) {
	const query = `
SELECT id,
       phone,
       password,
       nickname,
       COALESCE(avatar, ''),
       COALESCE(status, 0)
FROM client_user
WHERE phone = $1
LIMIT 1;
`
	var user clientUserRecord
	if err := h.db.QueryRowContext(c.Request.Context(), query, phone).Scan(
		&user.ID,
		&user.Phone,
		&user.Password,
		&user.Nickname,
		&user.Avatar,
		&user.Status,
	); err != nil {
		if errors.Is(err, sql.ErrNoRows) {
			return nil, nil
		}
		return nil, err
	}
	return &user, nil
}
