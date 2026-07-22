package http

import (
	"database/sql"
	"errors"
	"strings"

	"github.com/gin-gonic/gin"
	"github.com/redis/go-redis/v9"

	"voc-go-backend/internal/application/auth"
	"voc-go-backend/internal/infrastructure/security"
)

// AuthHandler 暴露认证相关 HTTP 接口。
type AuthHandler struct {
	svc    *auth.Service
	online *OnlineStore
	db     *sql.DB
	redis  *redis.Client
	tokens *security.TokenService
}

// NewAuthHandler 创建认证接口处理器。
// 其中 db 用于读取登录相关配置（如是否启用验证码）。
func NewAuthHandler(svc *auth.Service, online *OnlineStore, db *sql.DB, redisClient *redis.Client, tokenServices ...*security.TokenService) *AuthHandler {
	handler := &AuthHandler{
		svc:    svc,
		online: online,
		db:     db,
		redis:  redisClient,
	}
	if len(tokenServices) > 0 {
		handler.tokens = tokenServices[0]
	}
	return handler
}

// RegisterAuthRoutes 注册 /auth 相关路由。
func (h *AuthHandler) RegisterAuthRoutes(r *gin.Engine) {
	h.RegisterPublicAuthRoutes(r)
	h.RegisterProtectedAuthRoutes(r)
}

// RegisterPublicAuthRoutes registers authentication routes that must remain public.
func (h *AuthHandler) RegisterPublicAuthRoutes(r *gin.Engine) {
	r.POST("/auth/login", h.Login)
}

// RegisterProtectedAuthRoutes registers authentication routes that require a valid token.
func (h *AuthHandler) RegisterProtectedAuthRoutes(r *gin.Engine) {
	r.POST("/auth/logout", h.Logout)
}

// Login 处理 POST /auth/login。
// @Summary 用户登录
// @Description 使用账号密码进行登录，可选启用图形验证码。
// @Tags 认证
// @Accept json
// @Produce json
// @Param data body auth.LoginRequest true "登录请求参数"
// @Success 200 {object} map[string]interface{} "统一响应包装，data 为 LoginResponse"
// @Failure 400 {object} map[string]interface{} "登录参数或凭据错误"
// @Failure 423 {object} map[string]interface{} "账号被临时锁定"
// @Router /auth/login [post]
func (h *AuthHandler) Login(c *gin.Context) {
	var req auth.LoginRequest
	if err := c.ShouldBindJSON(&req); err != nil {
		// 参数缺失或格式不正确
		Fail(c, "400", "参数缺失或格式不正确")
		return
	}
	policy, err := loadClientSessionPolicy(c.Request.Context(), h.db, req.ClientID, "PC")
	if err != nil {
		Fail(c, "400", "客户端不可用")
		return
	}
	req.TokenTTL = policy.TokenTTL
	passwordPolicy, err := loadPasswordPolicy(c.Request.Context(), h.db)
	if err != nil {
		Fail(c, "500", "读取密码策略失败")
		return
	}
	locked, err := isLoginLocked(c.Request.Context(), h.redis, req.Username, passwordPolicy)
	if err != nil {
		Fail(c, "500", "读取账号锁定状态失败")
		return
	}
	if locked {
		Fail(c, "423", "登录失败次数过多，账号已临时锁定")
		return
	}

	// 当配置启用登录验证码且为账号登录时，先校验图形验证码。
	// 行为对齐 Java AccountLoginHandler.preLogin：
	// - LOGIN_CAPTCHA_ENABLED=0：不校验验证码；
	// - LOGIN_CAPTCHA_ENABLED!=0：必须校验 uuid + captcha。
	authType := strings.ToUpper(strings.TrimSpace(req.AuthType))
	if authType == "" || authType == "ACCOUNT" {
		enabled, err := isLoginCaptchaEnabled(c.Request.Context(), h.db)
		if err != nil {
			Fail(c, "500", "查询登录验证码配置失败")
			return
		}
		if enabled {
			if strings.TrimSpace(req.Captcha) == "" {
				Fail(c, "400", "验证码不能为空")
				return
			}
			if strings.TrimSpace(req.UUID) == "" {
				Fail(c, "400", "验证码标识不能为空")
				return
			}
			// 使用 Redis 校验验证码，key 与 Java 一致：CAPTCHA:{uuid}
			if h.redis == nil {
				Fail(c, "500", "验证码服务未初始化")
				return
			}
			ctx := c.Request.Context()
			key := buildCaptchaRedisKey(strings.TrimSpace(req.UUID))
			val, err := h.redis.Get(ctx, key).Result()
			if err != nil {
				if err == redis.Nil {
					Fail(c, "400", "验证码不正确或已过期")
					return
				}
				Fail(c, "500", "验证码校验失败")
				return
			}
			if !strings.EqualFold(strings.TrimSpace(req.Captcha), strings.TrimSpace(val)) {
				Fail(c, "400", "验证码不正确或已过期")
				return
			}
			// 校验通过后删除验证码，避免重复使用。
			_, _ = h.redis.Del(ctx, key).Result()
		}
	}

	resp, err := h.svc.Login(c.Request.Context(), req)
	if err != nil {
		if errors.Is(err, auth.ErrInvalidCredentials) {
			locked, lockErr := recordLoginFailure(c.Request.Context(), h.redis, req.Username, passwordPolicy)
			if lockErr != nil {
				Fail(c, "500", "记录登录失败次数失败")
				return
			}
			if locked {
				Fail(c, "423", "登录失败次数过多，账号已临时锁定")
				return
			}
		}
		Fail(c, "400", err.Error())
		return
	}
	if err := clearLoginFailures(c.Request.Context(), h.redis, req.Username); err != nil {
		Fail(c, "500", "清理登录失败状态失败")
		return
	}
	if h.tokens != nil && resp != nil {
		if err := h.tokens.RegisterSession(c.Request.Context(), resp.Token, policy.ActiveTimeout); err != nil {
			Fail(c, "500", "创建登录会话失败")
			return
		}
	}

	// 登录成功后记录在线用户展示信息；令牌有效性和撤销状态由 Redis 会话维护。
	if h.online != nil && resp != nil {
		h.online.RecordLogin(c, resp.UserID, resp.Username, resp.Nickname, req.ClientID, resp.Token)
	}

	// Successful login, return LoginResp as data.
	c.Header("Content-Type", "application/json; charset=utf-8")
	OK(c, resp)
}

// Logout 处理 POST /auth/logout。
// 同时撤销 Redis 中的服务端会话，并清理在线用户展示记录。
// @Summary 用户登出
// @Description 基于 Authorization Bearer Token 撤销服务端会话并登出。
// @Tags 认证
// @Produce json
// @Success 200 {object} map[string]interface{} "统一响应包装，data 为 true/false"
// @Router /auth/logout [post]
func (h *AuthHandler) Logout(c *gin.Context) {
	authz := c.GetHeader("Authorization")
	token := strings.TrimSpace(authz)
	if strings.HasPrefix(strings.ToLower(token), "bearer ") {
		token = strings.TrimSpace(token[7:])
	}
	if token != "" && h.online != nil {
		h.online.RemoveByToken(c.Request.Context(), token)
	}
	if h.tokens != nil {
		if err := h.tokens.RevokeSession(c.Request.Context(), authz); err != nil {
			Fail(c, "500", "退出登录失败")
			return
		}
	}
	OK(c, true)
}
