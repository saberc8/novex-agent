package novex

import (
	"context"
	"crypto/sha256"
	"encoding/hex"
	"errors"
	"github.com/gin-gonic/gin"
	"golang.org/x/crypto/bcrypt"
	"net/url"
	"strings"
	"time"
	"voc-go-backend/internal/infrastructure/security"
	httpif "voc-go-backend/internal/interfaces/http"
)

// Global data access is an explicit RBAC capability, never inferred from a
// username, system administration privileges, or a client-provided flag.
func globalDataAccess(u *User) bool {
	if u == nil {
		return false
	}
	for _, role := range u.Roles {
		if role.Code == "novex_super_admin" {
			return true
		}
	}
	return false
}

func (s *Server) loadSystemUser(ctx context.Context, userID int64) (*User, error) {
	u := &User{Roles: []Role{}}
	err := s.DB.QueryRowContext(ctx, `SELECT u.id,u.username,u.nickname,u.status,u.is_system,u.dept_id,COALESCE(n.tenant_id,''),COALESCE(n.department_id,''),n.user_id IS NOT NULL FROM sys_user u LEFT JOIN novex_users n ON n.user_id=u.id WHERE u.id=$1`, userID).Scan(&u.ID, &u.Username, &u.Nickname, &u.Status, &u.IsSystem, &u.SystemDeptID, &u.TenantID, &u.DepartmentID, &u.NovexEnabled)
	if err != nil {
		return nil, err
	}
	rows, err := s.DB.QueryContext(ctx, `SELECT r.id,r.name,r.code FROM sys_role r JOIN sys_user_role ur ON ur.role_id=r.id WHERE ur.user_id=$1 ORDER BY r.id`, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()
	for rows.Next() {
		var role Role
		if err = rows.Scan(&role.ID, &role.Name, &role.Code); err != nil {
			return nil, err
		}
		u.Roles = append(u.Roles, role)
		if role.Code == "admin" {
			u.IsAdmin = true
		}
	}
	if err = rows.Err(); err != nil {
		return nil, err
	}
	u.GlobalDataAccess = globalDataAccess(u)
	u.PolicyVersion, err = s.epoch(ctx)
	return u, err
}
func (s *Server) loadUser(ctx context.Context, userID int64) (*User, error) {
	u, e := s.loadSystemUser(ctx, userID)
	if e != nil {
		return nil, e
	}
	if u.Status != 1 {
		return nil, errors.New("user disabled")
	}
	if !u.NovexEnabled {
		return nil, errors.New("Novex profile not enabled")
	}
	return u, nil
}
func (s *Server) auth(c *gin.Context) {
	claims, err := s.Tokens.ParseContext(c.Request.Context(), c.GetHeader("Authorization"))
	if err != nil || claims.UserID == 0 || (claims.Subject != "" && claims.Subject != security.SubjectAdmin) {
		fail(c, 401, "请先登录")
		return
	}
	u, err := s.loadUser(c.Request.Context(), claims.UserID)
	if err != nil {
		fail(c, 401, "账号不可用")
		return
	}
	c.Set("novex.user", u)
	c.Next()
}
func (s *Server) admin(c *gin.Context) {
	if !actor(c).IsAdmin {
		fail(c, 403, "需要系统管理角色")
		return
	}
	c.Next()
}
func (s *Server) login(c *gin.Context) {
	var req struct {
		Username string `json:"username"`
		Password string `json:"password"`
	}
	if !bind(c, &req) {
		return
	}
	key := c.ClientIP() + ":" + strings.ToLower(req.Username)
	s.loginMu.Lock()
	attempt := s.loginAttempts[key]
	if attempt != nil && attempt.Count >= 10 && time.Now().Before(attempt.Until) {
		s.loginMu.Unlock()
		fail(c, 429, "登录尝试过多，请10分钟后重试")
		return
	}
	s.loginMu.Unlock()
	var uid int64
	var hash string
	err := s.DB.QueryRowContext(c, `SELECT id,password FROM sys_user WHERE username=$1 AND status=1`, req.Username).Scan(&uid, &hash)
	if err != nil || bcrypt.CompareHashAndPassword([]byte(strings.TrimPrefix(hash, "{bcrypt}")), []byte(req.Password)) != nil {
		s.loginMu.Lock()
		if attempt == nil || time.Now().After(attempt.Until) {
			attempt = &loginAttempt{Until: time.Now().Add(10 * time.Minute)}
		}
		attempt.Count++
		s.loginAttempts[key] = attempt
		s.loginMu.Unlock()
		fail(c, 401, "账号或密码错误")
		return
	}
	s.loginMu.Lock()
	delete(s.loginAttempts, key)
	s.loginMu.Unlock()
	s.issueLogin(c, uid)
}
func (s *Server) issueLogin(c *gin.Context, uid int64) {
	u, e := s.loadUser(c, uid)
	if e != nil {
		fail(c, 401, "账号尚未配置业务租户")
		return
	}
	token, e := s.Tokens.GenerateForSubjectWithTTL(uid, security.SubjectAdmin, 4*time.Hour)
	if e == nil {
		e = s.Tokens.RegisterSession(c, token, 30*time.Minute)
	}
	if e != nil {
		fail(c, 500, "无法建立登录会话")
		return
	}
	ok(c, gin.H{"token": token, "user": u})
}
func tokenHash(v string) string { h := sha256.Sum256([]byte(v)); return hex.EncodeToString(h[:]) }
func (s *Server) embedTicket(c *gin.Context) {
	var r struct {
		Origin string `json:"origin"`
	}
	if !bind(c, &r) {
		return
	}
	allowed := strings.Split(env("NOVEX_EMBED_ORIGINS", env("NOVEX_EMBED_PARENT_ORIGINS", "http://localhost:8311,http://127.0.0.1:8311")), ",")
	for n := range allowed {
		allowed[n] = strings.TrimSpace(allowed[n])
	}
	parsed, e := url.Parse(r.Origin)
	if e != nil || parsed.Path != "" || !contains(allowed, r.Origin) {
		fail(c, 400, "嵌入来源不在允许列表")
		return
	}
	ticket := id() + id()
	_, e = s.DB.ExecContext(c, `INSERT INTO novex_embed_tickets(token_hash,user_id,parent_origin,expires_at) VALUES($1,$2,$3,now()+interval '60 seconds')`, tokenHash(ticket), actor(c).ID, r.Origin)
	if e != nil {
		fail(c, 500, "签发失败")
		return
	}
	ok(c, gin.H{"ticket": ticket, "expiresIn": 60})
}
func (s *Server) embedExchange(c *gin.Context) {
	var r struct {
		Ticket string `json:"ticket"`
		Origin string `json:"origin"`
	}
	if !bind(c, &r) {
		return
	}
	requestOrigin := c.GetHeader("Origin")
	allowedChat := strings.Split(env("NOVEX_CHAT_ORIGINS", env("NOVEX_CHAT_ORIGIN", "http://localhost:8312,http://127.0.0.1:8312")), ",")
	for n := range allowedChat {
		allowedChat[n] = strings.TrimSpace(allowedChat[n])
	}
	if requestOrigin != "" && !contains(allowedChat, requestOrigin) {
		fail(c, 403, "聊天来源不受信任")
		return
	}
	var uid int64
	err := s.DB.QueryRowContext(c, `UPDATE novex_embed_tickets SET used=true WHERE token_hash=$1 AND parent_origin=$2 AND used=false AND expires_at>now() RETURNING user_id`, tokenHash(r.Ticket), r.Origin).Scan(&uid)
	if err != nil {
		fail(c, 401, "启动票据无效或已使用")
		return
	}
	s.issueLogin(c, uid)
}
func (s *Server) policyMutation(c *gin.Context) {
	s.mu.Lock()
	defer s.mu.Unlock()
	// Re-resolve actor after waiting for the mutation barrier. An earlier request
	// may have removed this caller's admin/governance role while it was queued.
	current := actor(c)
	if current == nil {
		fail(c, 401, "请先登录")
		return
	}
	fresh, e := s.loadUser(c, current.ID)
	if e != nil {
		fail(c, 401, "账号不可用")
		return
	}
	path := c.Request.URL.Path
	if strings.HasPrefix(path, "/api/novex/admin/") && !fresh.IsAdmin {
		fail(c, 403, "需要系统管理角色")
		return
	}
	if strings.HasPrefix(path, "/system/") && (!fresh.IsAdmin || !governance(fresh)) {
		fail(c, 403, "账号与角色变更需要授权治理角色")
		return
	}
	c.Set("novex.user", fresh)
	c.Next()
	if c.Writer.Status() < 400 {
		_, err := s.DB.ExecContext(c, `UPDATE novex_policy_version SET version=version+1 WHERE id=1`)
		if err == nil {
			for _, cancel := range s.active {
				cancel()
			}
			_, _ = s.DB.ExecContext(c, `UPDATE novex_runs SET status='cancelled',error='权限已变化，请新建会话' WHERE status IN ('queued','running')`)
		}
	}
}
func (s *Server) Register(r *gin.Engine) {
	p := r.Group("/api/novex")
	p.POST("/login", s.login)
	p.POST("/embed-exchange", s.embedExchange)
	p.POST("/mcp", s.mcp)
	p.GET("/mcp", func(c *gin.Context) { c.Status(405) })
	a := p.Group("")
	a.Use(s.auth, s.readBarrier)
	a.GET("/me", func(c *gin.Context) { ok(c, actor(c)) })
	a.POST("/logout", func(c *gin.Context) {
		if e := s.Tokens.RevokeSession(c, c.GetHeader("Authorization")); e != nil {
			fail(c, 500, "退出失败")
			return
		}
		ok(c, gin.H{"loggedOut": true})
	})
	a.POST("/embed-tickets", s.embedTicket)
	a.GET("/runtimes", s.runtimes)
	a.GET("/resources", s.visibleResources)
	a.POST("/query", s.queryHTTP)
	a.GET("/knowledge", s.listKnowledge)
	a.POST("/knowledge", s.policyMutation, s.uploadKnowledge)
	a.GET("/knowledge/:id", s.getKnowledge)
	a.POST("/knowledge/:id/search", s.searchSpreadsheetHTTP)
	a.GET("/knowledge/:id/file", s.downloadSpreadsheet)
	a.DELETE("/knowledge/:id", s.policyMutation, s.deleteKnowledge)
	a.PUT("/knowledge/:id/acl", s.admin, s.policyMutation, s.knowledgeACL)
	a.GET("/conversations", s.listConversations)
	a.POST("/conversations", s.createConversation)
	a.GET("/conversations/:id/messages", s.listMessages)
	a.POST("/conversations/:id/messages", s.sendMessage)
	a.GET("/runs/:id", s.getRun)
	a.GET("/runs/:id/events", s.runEvents)
	a.POST("/runs/:id/cancel", s.cancelRun)
	ad := a.Group("/admin")
	ad.Use(s.admin, httpif.RequireRBAC(s.DB, s.Tokens, ManagementRoutePermissions()))
	ad.GET("/overview", s.overview)
	ad.GET("/users", s.listUsers)
	ad.POST("/users", s.policyMutation, s.createUser)
	ad.PUT("/users/:id/roles", s.policyMutation, s.setRoles)
	ad.PUT("/users/:id/profile", s.policyMutation, s.enableProfile)
	ad.GET("/roles", s.listRoles)
	ad.POST("/roles", s.policyMutation, s.createRole)
	ad.GET("/datasources", s.listDatasources)
	ad.POST("/datasources", s.policyMutation, s.saveDatasource)
	ad.PUT("/datasources/:id", s.policyMutation, s.saveDatasource)
	ad.POST("/datasources/test", s.testDatasource)
	ad.POST("/datasources/:id/introspect", s.policyMutation, s.introspect)
	ad.GET("/resources", s.listResources)
	ad.POST("/resources", s.policyMutation, s.saveResource)
	ad.PUT("/resources/:id", s.policyMutation, s.saveResource)
	ad.GET("/identities", s.listIdentities)
	ad.POST("/identities", s.policyMutation, s.saveIdentity)
	ad.PUT("/identities/:id", s.policyMutation, s.saveIdentity)
	ad.GET("/grants", s.listGrants)
	ad.POST("/grants", s.policyMutation, s.saveGrant)
	ad.DELETE("/grants/:id", s.policyMutation, s.deleteGrant)
	ad.POST("/query/preview", s.preview)
	ad.GET("/audit", s.listAudit)
}

// LegacyGovernanceGuard closes the older admin API path around Novex grant delegation.
// Read-only legacy pages remain available; identity/role mutation requires governance.
func (s *Server) LegacyGovernanceGuard() gin.HandlerFunc {
	return func(c *gin.Context) {
		if c.Request.Method == "GET" || c.Request.Method == "HEAD" || c.Request.Method == "OPTIONS" {
			c.Next()
			return
		}
		path := c.Request.URL.Path
		if !strings.HasPrefix(path, "/system/user") && !strings.HasPrefix(path, "/system/role") && !strings.HasPrefix(path, "/system/dept") {
			c.Next()
			return
		}
		claims, e := s.Tokens.ParseContext(c, c.GetHeader("Authorization"))
		if e != nil {
			fail(c, 401, "请先登录")
			return
		}
		u, e := s.loadUser(c, claims.UserID)
		if e != nil || !u.IsAdmin || !governance(u) {
			fail(c, 403, "账号与角色变更需要授权治理角色，请使用Novex授权管理")
			return
		}
		c.Set("novex.user", u)
		s.policyMutation(c)
	}
}

func (s *Server) readBarrier(c *gin.Context) {
	if c.Request.Method != "GET" || strings.HasSuffix(c.Request.URL.Path, "/events") {
		c.Next()
		return
	}
	s.mu.RLock()
	defer s.mu.RUnlock()
	u, e := s.loadUser(c, actor(c).ID)
	if e != nil {
		fail(c, 401, "账号不可用")
		return
	}
	c.Set("novex.user", u)
	c.Next()
}
