package main

import (
	"crypto/rand"
	"encoding/base64"
	"fmt"
	"log"
	"os"
	"strconv"
	"strings"
	"time"

	"github.com/gin-gonic/gin"
	swaggerFiles "github.com/swaggo/files"
	ginSwagger "github.com/swaggo/gin-swagger"

	docs "voc-go-backend/docs"
	appauth "voc-go-backend/internal/application/auth"
	rbacdomain "voc-go-backend/internal/domain/rbac"
	"voc-go-backend/internal/domain/user"
	"voc-go-backend/internal/infrastructure/cache"
	"voc-go-backend/internal/infrastructure/db"
	rbacp "voc-go-backend/internal/infrastructure/persistence/rbac"
	syslogp "voc-go-backend/internal/infrastructure/persistence/syslog"
	persistence "voc-go-backend/internal/infrastructure/persistence/user"
	"voc-go-backend/internal/infrastructure/security"
	httpif "voc-go-backend/internal/interfaces/http"
	"voc-go-backend/internal/novex"
)

// @title voc-go-backend 接口文档
// @version 1.0
// @description Avalon 平台 Go 后端接口文档，涵盖认证、系统管理、日志等模块。
// @BasePath /
// @schemes http
// @securityDefinitions.apikey BearerAuth
// @in header
// @name Authorization

func main() {
	// 1. 初始化数据库连接（PostgreSQL）
	dbCfg := db.LoadConfigFromEnv()
	pg, err := db.NewPostgres(dbCfg)
	if err != nil {
		log.Fatalf("failed to connect postgres: %v", err)
	}
	defer pg.Close()

	// 1.0 初始化 Redis 连接（验证码等缓存使用）
	redisCfg := cache.LoadConfigFromEnv()
	redisClient, err := cache.NewRedis(redisCfg)
	if err != nil {
		log.Fatalf("failed to connect redis: %v", err)
	}
	defer redisClient.Close()

	// 1.1 自动迁移/初始化数据库及必要的基础数据。
	if err := db.AutoMigrate(pg); err != nil {
		log.Fatalf("failed to auto-migrate database: %v", err)
	}

	// 2. 初始化安全组件：RSA 解密器、BCrypt 密码校验、JWT 生成器
	rsaKey, err := loadRSAKey()
	if err != nil {
		log.Fatalf("failed to load RSA key: %v", err)
	}
	rsaDecryptor, err := security.NewRSADecryptorFromBase64(rsaKey)
	if err != nil {
		log.Fatalf("failed to init RSA decryptor: %v", err)
	}
	pwdVerifier := security.BcryptVerifier{}
	pwdHasher := security.BcryptHasher{}

	jwtSecret, err := loadJWTSecret()
	if err != nil {
		log.Fatalf("failed to load JWT secret: %v", err)
	}
	tokenTTL := 24 * time.Hour
	tokenSvc := security.NewTokenService(jwtSecret, tokenTTL)
	tokenSvc.SetSessionStore(security.NewRedisTokenSessionStore(redisClient))

	// 3. 初始化领域仓储和应用服务
	var userRepo user.Repository = persistence.NewPgRepository(pg)
	var roleRepo rbacdomain.RoleRepository = rbacp.NewPgRoleRepository(pg)
	var menuRepo rbacdomain.MenuRepository = rbacp.NewPgMenuRepository(pg)
	authSvc := appauth.NewService(userRepo, rsaDecryptor, pwdVerifier, tokenSvc)

	// 4. 初始化 HTTP 服务（Gin）
	r := gin.Default()
	maxRequestBodyBytes := getenvInt64Default("HTTP_MAX_REQUEST_BODY_BYTES", 32<<20)
	r.MaxMultipartMemory = 8 << 20
	allowedOrigins := parseAllowedOrigins(getenvDefault(
		"HTTP_CORS_ALLOWED_ORIGINS",
		"http://localhost:8202,http://127.0.0.1:8202,http://localhost:8203,http://127.0.0.1:8203",
	))

	// 全局 CORS（开发阶段允许前端本地调试）
	r.Use(func(c *gin.Context) {
		origin := c.Request.Header.Get("Origin")
		if _, ok := allowedOrigins[origin]; ok {
			c.Writer.Header().Set("Access-Control-Allow-Origin", origin)
			c.Writer.Header().Set("Vary", "Origin")
		}
		c.Writer.Header().Set("Access-Control-Allow-Credentials", "true")
		c.Writer.Header().Set("Access-Control-Allow-Headers", "Content-Type, Authorization, X-Requested-With")
		c.Writer.Header().Set("Access-Control-Allow-Methods", "GET, POST, PUT, PATCH, DELETE, OPTIONS")

		// 预检请求直接返回
		if c.Request.Method == "OPTIONS" {
			c.AbortWithStatus(204)
			return
		}

		c.Next()
	})
	r.Use(httpif.MaxRequestBodySize(maxRequestBodyBytes))

	// 系统操作日志中间件：在业务处理前后统一记录 sys_log。
	sysLogRepo := syslogp.NewPgRepository(pg)
	r.Use(httpif.NewSysLogMiddleware(sysLogRepo, tokenSvc))

	// 在线列表用于展示；鉴权、闲置超时和会话撤销由 Redis 统一维护。
	onlineStore := httpif.NewRedisOnlineStore(redisClient, tokenSvc)
	r.Use(httpif.NewOnlineActivityMiddleware(onlineStore))

	// 公共接口：站点基础配置、公开字典、验证码、登录、静态文件和 Swagger。
	commonHandler := httpif.NewCommonHandler(pg)
	commonHandler.RegisterPublicCommonRoutes(r)

	// 验证码接口（登录图片验证码）
	captchaHandler := httpif.NewCaptchaHandler(pg, redisClient)
	captchaHandler.RegisterCaptchaRoutes(r)

	// 登录接口保持公开，登出和用户信息在 RBAC 中间件之后注册。
	authHandler := httpif.NewAuthHandler(authSvc, onlineStore, pg, redisClient, tokenSvc)
	authHandler.RegisterPublicAuthRoutes(r)

	// H5 consumer auth uses an independent user table and token subject.
	clientAuthHandler := httpif.NewClientAuthHandler(pg, tokenSvc, pwdHasher, pwdVerifier)
	clientAuthHandler.RegisterPublicClientRoutes(r)

	// 静态文件访问（上传文件）
	fileRoot := getenvDefault("FILE_STORAGE_DIR", "./data/file")
	r.Static("/file", fileRoot)

	// Swagger 接口文档
	// 访问地址示例：http://localhost:8201/swagger/index.html
	r.GET("/swagger/*any", ginSwagger.WrapHandler(swaggerFiles.Handler))

	clientProtected := r.Group("")
	clientProtected.Use(httpif.RequireClientAuth(pg, tokenSvc))
	clientAuthHandler.RegisterProtectedClientRoutes(clientProtected)

	// Novex owns authenticated routes and run-scoped MCP before the legacy route allowlist.
	novexServer, err := novex.New(pg, tokenSvc)
	if err != nil {
		log.Fatalf("failed to initialize Novex: %v", err)
	}
	novexServer.Register(r)
	r.Use(novexServer.LegacyGovernanceGuard())

	// 受保护接口统一认证与 RBAC 授权。
	r.Use(httpif.RequireRBAC(pg, tokenSvc, httpif.DefaultRoutePermissions()))

	// 需要登录但不需要菜单权限的认证接口。
	authHandler.RegisterProtectedAuthRoutes(r)

	// 需要登录和权限的公共辅助接口。
	commonHandler.RegisterProtectedCommonRoutes(r)

	// 用户信息接口
	userHandler := httpif.NewUserHandler(userRepo, roleRepo, menuRepo, tokenSvc, pg, rsaDecryptor, pwdHasher, pwdVerifier)
	userHandler.RegisterUserRoutes(r)

	// 系统监控：在线用户
	onlineUserHandler := httpif.NewOnlineUserHandler(onlineStore, tokenSvc)
	onlineUserHandler.RegisterOnlineUserRoutes(r)

	// 系统管理：菜单管理
	menuHandler := httpif.NewMenuHandler(pg, tokenSvc)
	menuHandler.RegisterMenuRoutes(r)

	// 系统管理：角色管理
	roleHandler := httpif.NewRoleHandler(pg, tokenSvc)
	roleHandler.RegisterRoleRoutes(r)

	// 系统管理：部门管理（仅树查询）
	deptHandler := httpif.NewDeptHandler(pg, tokenSvc)
	deptHandler.RegisterDeptRoutes(r)

	// 系统管理：用户管理
	systemUserHandler := httpif.NewSystemUserHandler(pg, tokenSvc, rsaDecryptor, pwdHasher, redisClient)
	systemUserHandler.RegisterSystemUserRoutes(r)

	// 系统管理：字典管理
	dictHandler := httpif.NewDictHandler(pg, tokenSvc)
	dictHandler.RegisterDictRoutes(r)

	// 系统管理：系统配置（参数管理）
	optionHandler := httpif.NewOptionHandler(pg, tokenSvc)
	optionHandler.RegisterOptionRoutes(r)

	// 系统管理：文件管理
	fileHandler := httpif.NewFileHandler(pg, tokenSvc)
	fileHandler.RegisterFileRoutes(r)

	// 系统管理：存储配置（需要 RSA 解密存储密钥）
	storageHandler := httpif.NewStorageHandler(pg, tokenSvc, rsaDecryptor)
	storageHandler.RegisterStorageRoutes(r)

	// 系统管理：客户端配置
	clientHandler := httpif.NewClientHandler(pg, tokenSvc)
	clientHandler.RegisterClientRoutes(r)

	// 系统监控：系统日志
	logHandler := httpif.NewLogHandler(pg)
	logHandler.RegisterLogRoutes(r)

	// 5. 启动 HTTP 服务
	port := getenvDefault("HTTP_PORT", "8201")
	// 在启动前设置 swagger 文档的 Host，便于在 UI 中调试。
	docs.SwaggerInfo.Host = "localhost:" + port
	if err := r.Run(":" + port); err != nil {
		log.Fatalf("failed to start http server: %v", err)
	}
}

func getenvDefault(key, def string) string {
	if v := os.Getenv(key); v != "" {
		return v
	}
	return def
}

func getenvInt64Default(key string, def int64) int64 {
	value := strings.TrimSpace(os.Getenv(key))
	if value == "" {
		return def
	}
	parsed, err := strconv.ParseInt(value, 10, 64)
	if err != nil || parsed <= 0 {
		log.Printf("[config] invalid %s=%q; using %d", key, value, def)
		return def
	}
	return parsed
}

func parseAllowedOrigins(raw string) map[string]struct{} {
	origins := make(map[string]struct{})
	for _, item := range strings.Split(raw, ",") {
		origin := strings.TrimSpace(item)
		if origin != "" {
			origins[origin] = struct{}{}
		}
	}
	return origins
}

func loadRSAKey() (string, error) {
	if key := strings.TrimSpace(os.Getenv("AUTH_RSA_PRIVATE_KEY")); key != "" {
		return key, nil
	}
	return "", fmt.Errorf("AUTH_RSA_PRIVATE_KEY is required")
}

func loadJWTSecret() (string, error) {
	if secret := strings.TrimSpace(os.Getenv("AUTH_JWT_SECRET")); secret != "" {
		if len(secret) < 32 {
			return "", fmt.Errorf("AUTH_JWT_SECRET must contain at least 32 characters")
		}
		return secret, nil
	}
	if isProduction() {
		return "", fmt.Errorf("AUTH_JWT_SECRET is required when APP_ENV=production")
	}
	randomBytes := make([]byte, 32)
	if _, err := rand.Read(randomBytes); err != nil {
		return "", fmt.Errorf("generate development JWT secret: %w", err)
	}
	log.Print("[security] AUTH_JWT_SECRET is unset; generated an ephemeral development secret")
	return base64.RawURLEncoding.EncodeToString(randomBytes), nil
}

func isProduction() bool {
	env := strings.ToLower(strings.TrimSpace(os.Getenv("APP_ENV")))
	return env == "production" || env == "prod"
}
