package http

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"encoding/json"
	"errors"
	"io"
	nethttp "net/http"
	"net/http/httptest"
	"strings"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	"voc-go-backend/internal/infrastructure/security"
)

type rbacTestUser struct {
	id       int64
	username string
	status   int64
	isSystem bool
	deptID   int64
}

type rbacTestRole struct {
	id        int64
	code      string
	dataScope int64
}

type rbacTestStore struct {
	users map[int64]rbacTestUser
	roles map[int64][]rbacTestRole
	perms map[int64][]string
}

func TestRequireRBACRejectsMissingToken(t *testing.T) {
	router, _, _ := newRBACMiddlewareTestRouter(t, rbacTestStore{})

	resp := performRBACRequest(router, "")

	assertBusinessCode(t, resp, "401")
}

func TestRequireRBACRejectsDisabledUser(t *testing.T) {
	router, tokenSvc, _ := newRBACMiddlewareTestRouter(t, rbacTestStore{
		users: map[int64]rbacTestUser{
			1: {id: 1, username: "disabled", status: 2, deptID: 100},
		},
	})

	resp := performRBACRequest(router, bearerToken(t, tokenSvc, 1))

	assertBusinessCode(t, resp, "401")
}

func TestRequireRBACRejectsMissingPermission(t *testing.T) {
	router, tokenSvc, _ := newRBACMiddlewareTestRouter(t, rbacTestStore{
		users: map[int64]rbacTestUser{
			2: {id: 2, username: "limited", status: 1, deptID: 100},
		},
		roles: map[int64][]rbacTestRole{
			2: {{id: 20, code: "general", dataScope: 4}},
		},
		perms: map[int64][]string{
			2: {"system:role:list"},
		},
	})

	resp := performRBACRequest(router, bearerToken(t, tokenSvc, 2))

	assertBusinessCode(t, resp, "403")
}

func TestRequireRBACAllowsMappedPermission(t *testing.T) {
	router, tokenSvc, _ := newRBACMiddlewareTestRouter(t, rbacTestStore{
		users: map[int64]rbacTestUser{
			3: {id: 3, username: "operator", status: 1, deptID: 100},
		},
		roles: map[int64][]rbacTestRole{
			3: {{id: 30, code: "operator", dataScope: 4}},
		},
		perms: map[int64][]string{
			3: {"system:user:list"},
		},
	})

	resp := performRBACRequest(router, bearerToken(t, tokenSvc, 3))

	assertBusinessCode(t, resp, "200")
}

func TestRequireRBACAllowsAdminRole(t *testing.T) {
	router, tokenSvc, _ := newRBACMiddlewareTestRouter(t, rbacTestStore{
		users: map[int64]rbacTestUser{
			4: {id: 4, username: "admin", status: 1, isSystem: true, deptID: 100},
		},
		roles: map[int64][]rbacTestRole{
			4: {{id: 1, code: "admin", dataScope: 1}},
		},
	})

	resp := performRBACRequest(router, bearerToken(t, tokenSvc, 4))

	assertBusinessCode(t, resp, "200")
}

func TestRequireRBACRejectsClientSubject(t *testing.T) {
	router, tokenSvc, _ := newRBACMiddlewareTestRouter(t, rbacTestStore{
		users: map[int64]rbacTestUser{
			5: {id: 5, username: "client-shadow", status: 1, deptID: 100},
		},
		roles: map[int64][]rbacTestRole{
			5: {{id: 50, code: "operator", dataScope: 4}},
		},
		perms: map[int64][]string{
			5: {"system:user:list"},
		},
	})
	token, err := tokenSvc.GenerateForSubject(5, security.SubjectClient)
	if err != nil {
		t.Fatalf("GenerateForSubject: %v", err)
	}

	resp := performRBACRequest(router, "Bearer "+token)

	assertBusinessCode(t, resp, "401")
}

func TestDefaultRoutePermissionsCoverRegisteredProtectedRoutes(t *testing.T) {
	gin.SetMode(gin.TestMode)
	router := gin.New()

	(&AuthHandler{}).RegisterProtectedAuthRoutes(router)
	(&CommonHandler{}).RegisterProtectedCommonRoutes(router)
	(&UserHandler{}).RegisterUserRoutes(router)
	NewOnlineUserHandler(nil, nil).RegisterOnlineUserRoutes(router)
	NewMenuHandler(nil, nil).RegisterMenuRoutes(router)
	NewRoleHandler(nil, nil).RegisterRoleRoutes(router)
	NewDeptHandler(nil, nil).RegisterDeptRoutes(router)
	NewSystemUserHandler(nil, nil, nil, nil).RegisterSystemUserRoutes(router)
	NewDictHandler(nil, nil).RegisterDictRoutes(router)
	NewOptionHandler(nil, nil).RegisterOptionRoutes(router)
	NewFileHandler(nil, nil).RegisterFileRoutes(router)
	NewStorageHandler(nil, nil, nil).RegisterStorageRoutes(router)
	NewClientHandler(nil, nil).RegisterClientRoutes(router)
	NewLogHandler(nil).RegisterLogRoutes(router)

	permissions := DefaultRoutePermissions()
	index := make(map[string]RoutePermission, len(permissions))
	for _, permission := range permissions {
		key := routeKey(permission.Method, permission.Path)
		if _, exists := index[key]; exists {
			t.Fatalf("duplicate route permission mapping for %s", key)
		}
		index[key] = permission
	}

	for _, route := range router.Routes() {
		key := routeKey(route.Method, route.Path)
		if _, ok := index[key]; !ok {
			t.Fatalf("missing RBAC permission mapping for registered route %s", key)
		}
	}
}

func newRBACMiddlewareTestRouter(t *testing.T, store rbacTestStore) (*gin.Engine, *security.TokenService, *sql.DB) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := sql.OpenDB(rbacTestConnector{store: store})
	tokenSvc := security.NewTokenService("test-secret", time.Hour)
	router := gin.New()
	router.Use(RequireRBAC(db, tokenSvc, []RoutePermission{
		{Method: nethttp.MethodGet, Path: "/system/user", Permission: "system:user:list"},
	}))
	router.GET("/system/user", func(c *gin.Context) {
		OK(c, gin.H{"ok": true})
	})
	t.Cleanup(func() { _ = db.Close() })
	return router, tokenSvc, db
}

func performRBACRequest(router *gin.Engine, authz string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(nethttp.MethodGet, "/system/user", nil)
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func bearerToken(t *testing.T, tokenSvc *security.TokenService, userID int64) string {
	t.Helper()
	token, err := tokenSvc.Generate(userID)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	return "Bearer " + token
}

func assertBusinessCode(t *testing.T, resp *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body struct {
		Code string `json:"code"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body %q: %v", resp.Body.String(), err)
	}
	if body.Code != want {
		t.Fatalf("business code = %s, want %s; body=%s", body.Code, want, resp.Body.String())
	}
}

type rbacTestConnector struct {
	store rbacTestStore
}

func (c rbacTestConnector) Connect(context.Context) (driver.Conn, error) {
	return rbacTestConn{store: c.store}, nil
}

func (c rbacTestConnector) Driver() driver.Driver {
	return rbacTestDriver{}
}

type rbacTestDriver struct{}

func (rbacTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use OpenDB with rbacTestConnector")
}

type rbacTestConn struct {
	store rbacTestStore
}

func (c rbacTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented")
}

func (c rbacTestConn) Close() error {
	return nil
}

func (c rbacTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not implemented")
}

func (c rbacTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	userID := int64(0)
	if len(args) > 0 {
		userID = asInt64(args[0].Value)
	}
	compact := strings.Join(strings.Fields(query), " ")
	switch {
	case strings.Contains(compact, "FROM sys_user") && strings.Contains(compact, "WHERE id"):
		u, ok := c.store.users[userID]
		if !ok {
			return newRBACRows([]string{"id", "username", "status", "is_system", "dept_id"}, nil), nil
		}
		return newRBACRows(
			[]string{"id", "username", "status", "is_system", "dept_id"},
			[][]driver.Value{{u.id, u.username, u.status, u.isSystem, u.deptID}},
		), nil
	case strings.Contains(compact, "FROM sys_role"):
		roles := c.store.roles[userID]
		values := make([][]driver.Value, 0, len(roles))
		for _, role := range roles {
			values = append(values, []driver.Value{role.id, role.code, role.dataScope})
		}
		return newRBACRows([]string{"id", "code", "data_scope"}, values), nil
	case strings.Contains(compact, "FROM sys_menu"):
		perms := c.store.perms[userID]
		values := make([][]driver.Value, 0, len(perms))
		for _, perm := range perms {
			values = append(values, []driver.Value{perm})
		}
		return newRBACRows([]string{"permission"}, values), nil
	default:
		return nil, errors.New("unexpected query: " + compact)
	}
}

type rbacTestRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func newRBACRows(columns []string, values [][]driver.Value) *rbacTestRows {
	return &rbacTestRows{columns: columns, values: values}
}

func (r *rbacTestRows) Columns() []string {
	return r.columns
}

func (r *rbacTestRows) Close() error {
	return nil
}

func (r *rbacTestRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func asInt64(v any) int64 {
	switch n := v.(type) {
	case int64:
		return n
	case int:
		return int64(n)
	case int32:
		return int64(n)
	default:
		return 0
	}
}
