package http

import (
	"bytes"
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

type guardTestStore struct {
	systemRoles           map[int64]string
	systemUsers           map[int64]string
	systemDicts           map[int64]string
	systemClients         map[int64]string
	systemMenus           map[int64]string
	menuParents           map[int64]int64
	failOnUserRoleRewrite bool
	roles                 map[int64]dataScopeTestRole
	roleMenus             map[int64][]int64
	userMenuIDs           map[int64][]int64
	userDepts             map[int64]int64
}

func TestSystemRoleGuardRejectsDeleteByNonAdmin(t *testing.T) {
	tokenSvc := security.NewTokenService("guard-secret", time.Hour)
	db := newGuardTestDB(t, guardTestStore{
		systemRoles: map[int64]string{1: "系统管理员"},
	})
	handler := NewRoleHandler(db, tokenSvc)
	router := newGuardTestRouter(99, tokenSvc)
	router.DELETE("/system/role", handler.DeleteRole)

	resp := performGuardRequest(t, router, tokenSvc, nethttp.MethodDelete, "/system/role", `{"ids":[1]}`)

	assertBusinessCode(t, resp, "400")
}

func TestSystemUserGuardRejectsRoleUpdateByNonAdmin(t *testing.T) {
	tokenSvc := security.NewTokenService("guard-secret", time.Hour)
	db := newGuardTestDB(t, guardTestStore{
		systemUsers: map[int64]string{1: "管理员"},
	})
	handler := NewSystemUserHandler(db, tokenSvc, nil, security.BcryptHasher{})
	router := newGuardTestRouter(99, tokenSvc)
	router.PATCH("/system/user/:id/role", handler.UpdateUserRole)

	resp := performGuardRequest(t, router, tokenSvc, nethttp.MethodPatch, "/system/user/1/role", `{"roleIds":[2]}`)

	assertBusinessCode(t, resp, "400")
}

func TestSystemDictGuardRejectsDeleteByNonAdmin(t *testing.T) {
	tokenSvc := security.NewTokenService("guard-secret", time.Hour)
	db := newGuardTestDB(t, guardTestStore{
		systemDicts: map[int64]string{1: "系统配置"},
	})
	handler := NewDictHandler(db, tokenSvc)
	router := newGuardTestRouter(99, tokenSvc)
	router.DELETE("/system/dict", handler.DeleteDict)

	resp := performGuardRequest(t, router, tokenSvc, nethttp.MethodDelete, "/system/dict", `{"ids":[1]}`)

	assertBusinessCode(t, resp, "400")
}

func TestSystemClientGuardRejectsUpdateByNonAdmin(t *testing.T) {
	tokenSvc := security.NewTokenService("guard-secret", time.Hour)
	db := newGuardTestDB(t, guardTestStore{
		systemClients: map[int64]string{2: "h5-client"},
	})
	handler := NewClientHandler(db, tokenSvc)
	router := newGuardTestRouter(99, tokenSvc)
	router.PUT("/system/client/:id", handler.UpdateClient)

	resp := performGuardRequest(t, router, tokenSvc, nethttp.MethodPut, "/system/client/2", `{
		"clientType":"H5",
		"authType":["ACCOUNT"],
		"activeTimeout":1800,
		"timeout":86400,
		"status":1
	}`)

	assertBusinessCode(t, resp, "400")
}

func TestSystemClientGuardRejectsDeleteByNonAdmin(t *testing.T) {
	tokenSvc := security.NewTokenService("guard-secret", time.Hour)
	db := newGuardTestDB(t, guardTestStore{
		systemClients: map[int64]string{1: "ef51c9a3e9046c4f2ea45142c8a8344a"},
	})
	handler := NewClientHandler(db, tokenSvc)
	router := newGuardTestRouter(99, tokenSvc)
	router.DELETE("/system/client", handler.DeleteClient)

	resp := performGuardRequest(t, router, tokenSvc, nethttp.MethodDelete, "/system/client", `{"ids":[1]}`)

	assertBusinessCode(t, resp, "400")
}

func TestSystemMenuGuardRejectsUpdateByNonAdmin(t *testing.T) {
	tokenSvc := security.NewTokenService("guard-secret", time.Hour)
	db := newGuardTestDB(t, guardTestStore{
		systemMenus: map[int64]string{1000: "系统管理"},
	})
	handler := NewMenuHandler(db, tokenSvc)
	router := newGuardTestRouter(99, tokenSvc)
	router.PUT("/system/menu/:id", handler.UpdateMenu)

	resp := performGuardRequest(t, router, tokenSvc, nethttp.MethodPut, "/system/menu/1000", `{
		"type":1,
		"title":"系统管理",
		"sort":1,
		"parentId":0,
		"status":1
	}`)

	assertBusinessCode(t, resp, "400")
}

func TestSystemMenuGuardRejectsDeleteByNonAdmin(t *testing.T) {
	tokenSvc := security.NewTokenService("guard-secret", time.Hour)
	db := newGuardTestDB(t, guardTestStore{
		systemMenus: map[int64]string{1000: "系统管理"},
		menuParents: map[int64]int64{
			1000: 0,
			1010: 1000,
		},
	})
	handler := NewMenuHandler(db, tokenSvc)
	router := newGuardTestRouter(99, tokenSvc)
	router.DELETE("/system/menu", handler.DeleteMenu)

	resp := performGuardRequest(t, router, tokenSvc, nethttp.MethodDelete, "/system/menu", `{"ids":[1000]}`)

	assertBusinessCode(t, resp, "400")
}

func TestSystemUserUpdateDoesNotRewriteRoles(t *testing.T) {
	tokenSvc := security.NewTokenService("guard-secret", time.Hour)
	db := newGuardTestDB(t, guardTestStore{
		failOnUserRoleRewrite: true,
	})
	handler := NewSystemUserHandler(db, tokenSvc, nil, security.BcryptHasher{})
	router := newGuardTestRouter(99, tokenSvc)
	router.PUT("/system/user/:id", handler.UpdateUser)

	resp := performGuardRequest(t, router, tokenSvc, nethttp.MethodPut, "/system/user/10", `{
		"username":"operator",
		"nickname":"Operator",
		"gender":1,
		"email":"",
		"phone":"",
		"avatar":"",
		"description":"",
		"status":1,
		"deptId":1,
		"roleIds":[1]
	}`)

	assertBusinessCode(t, resp, "200")
}

func TestSystemUserCreateRejectsAdminRoleGrantByNonAdmin(t *testing.T) {
	tokenSvc := security.NewTokenService("guard-secret", time.Hour)
	db := newGuardTestDB(t, guardTestStore{
		roles: map[int64]dataScopeTestRole{
			1: {id: 1, code: "admin", dataScope: dataScopeAll, isSystem: true},
		},
	})
	handler := NewSystemUserHandler(db, tokenSvc, nil, security.BcryptHasher{})
	router := newGuardTestRouter(99, tokenSvc)
	router.POST("/system/user", handler.CreateUser)

	resp := performGuardRequest(t, router, tokenSvc, nethttp.MethodPost, "/system/user", `{
		"username":"new-admin",
		"nickname":"New Admin",
		"password":"encrypted",
		"gender":1,
		"status":1,
		"deptId":1,
		"roleIds":[1]
	}`)

	assertBusinessCode(t, resp, "403")
}

func TestSystemUserRoleUpdateRejectsAdminRoleGrantByNonAdmin(t *testing.T) {
	tokenSvc := security.NewTokenService("guard-secret", time.Hour)
	db := newGuardTestDB(t, guardTestStore{
		roles: map[int64]dataScopeTestRole{
			1: {id: 1, code: "admin", dataScope: dataScopeAll, isSystem: true},
		},
	})
	handler := NewSystemUserHandler(db, tokenSvc, nil, security.BcryptHasher{})
	router := newGuardTestRouter(99, tokenSvc)
	router.PATCH("/system/user/:id/role", handler.UpdateUserRole)

	resp := performGuardRequest(t, router, tokenSvc, nethttp.MethodPatch, "/system/user/10/role", `{"roleIds":[1]}`)

	assertBusinessCode(t, resp, "403")
}

func TestRoleCreateRejectsAllDataScopeByNonAdmin(t *testing.T) {
	tokenSvc := security.NewTokenService("guard-secret", time.Hour)
	db := newGuardTestDB(t, guardTestStore{})
	handler := NewRoleHandler(db, tokenSvc)
	router := newGuardTestRouter(99, tokenSvc)
	router.POST("/system/role", handler.CreateRole)

	resp := performGuardRequest(t, router, tokenSvc, nethttp.MethodPost, "/system/role", `{
		"name":"All Data",
		"code":"all_data",
		"sort":1,
		"dataScope":1
	}`)

	assertBusinessCode(t, resp, "403")
}

func TestRolePermissionRejectsMenuOutsidePrincipalByNonAdmin(t *testing.T) {
	tokenSvc := security.NewTokenService("guard-secret", time.Hour)
	db := newGuardTestDB(t, guardTestStore{
		roles: map[int64]dataScopeTestRole{
			10: {id: 10, code: "operator", dataScope: dataScopeSelf},
		},
		userMenuIDs: map[int64][]int64{
			99: {100},
		},
	})
	handler := NewRoleHandler(db, tokenSvc)
	router := newGuardTestRouter(99, tokenSvc)
	router.PUT("/system/role/:id/permission", handler.UpdateRolePermission)

	resp := performGuardRequest(t, router, tokenSvc, nethttp.MethodPut, "/system/role/10/permission", `{
		"menuIds":[100,999],
		"menuCheckStrictly":true
	}`)

	assertBusinessCode(t, resp, "403")
}

func TestRoleAssignRejectsSelfAssignmentByNonAdmin(t *testing.T) {
	tokenSvc := security.NewTokenService("guard-secret", time.Hour)
	db := newGuardTestDB(t, guardTestStore{})
	handler := NewRoleHandler(db, tokenSvc)
	router := newGuardTestRouter(99, tokenSvc)
	router.POST("/system/role/:id/user", handler.AssignToUsers)

	resp := performGuardRequest(t, router, tokenSvc, nethttp.MethodPost, "/system/role/10/user", `[99]`)

	assertBusinessCode(t, resp, "400")
}

func newGuardTestRouter(userID int64, tokenSvc *security.TokenService) *gin.Engine {
	gin.SetMode(gin.TestMode)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setCurrentPrincipal(c, &Principal{
			UserID:      userID,
			Username:    "operator",
			DeptID:      1,
			Roles:       []PrincipalRole{{ID: 2, Code: "operator", DataScope: dataScopeAll}},
			Permissions: map[string]struct{}{},
		})
		c.Next()
	})
	return router
}

func performGuardRequest(t *testing.T, router *gin.Engine, tokenSvc *security.TokenService, method, path, body string) *httptest.ResponseRecorder {
	t.Helper()
	token, err := tokenSvc.Generate(99)
	if err != nil {
		t.Fatalf("generate token: %v", err)
	}
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Authorization", "Bearer "+token)
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func newGuardTestDB(t *testing.T, store guardTestStore) *sql.DB {
	t.Helper()
	db := sql.OpenDB(guardTestConnector{store: store})
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type guardTestConnector struct {
	store guardTestStore
}

func (c guardTestConnector) Connect(context.Context) (driver.Conn, error) {
	return guardTestConn{store: c.store}, nil
}

func (c guardTestConnector) Driver() driver.Driver {
	return guardTestDriver{}
}

type guardTestDriver struct{}

func (guardTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use OpenDB with guardTestConnector")
}

type guardTestConn struct {
	store guardTestStore
}

func (c guardTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented")
}

func (c guardTestConn) Close() error {
	return nil
}

func (c guardTestConn) Begin() (driver.Tx, error) {
	return guardTestTx{}, nil
}

func (c guardTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	id := int64(0)
	if len(args) > 0 {
		id = asInt64(args[0].Value)
	}
	compact := strings.Join(strings.Fields(query), " ")
	switch {
	case strings.Contains(compact, "FROM sys_role") && strings.Contains(compact, "WHERE id = ANY"):
		roleIDs := parseDriverInt64Array(args[0].Value)
		values := make([][]driver.Value, 0, len(roleIDs))
		for _, roleID := range roleIDs {
			if role, ok := c.store.roles[roleID]; ok {
				values = append(values, []driver.Value{role.id, role.code, role.dataScope, role.isSystem})
			}
		}
		return newGuardRows([]string{"id", "code", "data_scope", "is_system"}, values), nil
	case strings.Contains(compact, "FROM sys_role_menu") && strings.Contains(compact, "role_id = ANY"):
		roleIDs := parseDriverInt64Array(args[0].Value)
		values := make([][]driver.Value, 0)
		for _, roleID := range roleIDs {
			for _, menuID := range c.store.roleMenus[roleID] {
				values = append(values, []driver.Value{roleID, menuID})
			}
		}
		return newGuardRows([]string{"role_id", "menu_id"}, values), nil
	case strings.Contains(compact, "FROM sys_role_menu") && strings.Contains(compact, "ur.user_id"):
		userID := asInt64(args[0].Value)
		requested := parseDriverInt64Array(args[1].Value)
		owned := make(map[int64]struct{}, len(c.store.userMenuIDs[userID]))
		for _, menuID := range c.store.userMenuIDs[userID] {
			owned[menuID] = struct{}{}
		}
		values := make([][]driver.Value, 0, len(requested))
		for _, menuID := range requested {
			if _, ok := owned[menuID]; ok {
				values = append(values, []driver.Value{menuID})
			}
		}
		return newGuardRows([]string{"menu_id"}, values), nil
	case strings.Contains(compact, "SELECT dept_id FROM sys_user"):
		userID := asInt64(args[0].Value)
		deptID, ok := c.store.userDepts[userID]
		if !ok {
			return newGuardRows([]string{"dept_id"}, nil), nil
		}
		return newGuardRows([]string{"dept_id"}, [][]driver.Value{{deptID}}), nil
	case strings.Contains(compact, "FROM sys_role") && strings.Contains(compact, "is_system"):
		if name, ok := c.store.systemRoles[id]; ok {
			return newGuardRows([]string{"name"}, [][]driver.Value{{name}}), nil
		}
		return newGuardRows([]string{"name"}, nil), nil
	case strings.Contains(compact, "FROM sys_user") && strings.Contains(compact, "is_system"):
		if name, ok := c.store.systemUsers[id]; ok {
			return newGuardRows([]string{"nickname"}, [][]driver.Value{{name}}), nil
		}
		return newGuardRows([]string{"nickname"}, nil), nil
	case strings.Contains(compact, "FROM sys_dict") && strings.Contains(compact, "is_system"):
		if name, ok := c.store.systemDicts[id]; ok {
			return newGuardRows([]string{"name"}, [][]driver.Value{{name}}), nil
		}
		return newGuardRows([]string{"name"}, nil), nil
	case strings.Contains(compact, "FROM sys_client"):
		if clientID, ok := c.store.systemClients[id]; ok {
			return newGuardRows([]string{"client_id"}, [][]driver.Value{{clientID}}), nil
		}
		return newGuardRows([]string{"client_id"}, nil), nil
	case strings.Contains(compact, "FROM sys_menu") && strings.Contains(compact, "is_system"):
		if title, ok := c.store.systemMenus[id]; ok {
			return newGuardRows([]string{"title"}, [][]driver.Value{{title}}), nil
		}
		return newGuardRows([]string{"title"}, nil), nil
	case strings.Contains(compact, "FROM sys_menu"):
		values := make([][]driver.Value, 0, len(c.store.menuParents))
		for menuID, parentID := range c.store.menuParents {
			values = append(values, []driver.Value{menuID, parentID})
		}
		return newGuardRows([]string{"id", "parent_id"}, values), nil
	default:
		return nil, errors.New("unexpected query: " + compact)
	}
}

func (c guardTestConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	if c.store.failOnUserRoleRewrite {
		compact := strings.ToLower(strings.Join(strings.Fields(query), " "))
		if strings.Contains(compact, "sys_user_role") {
			return nil, errors.New("sys_user_role must not be changed by UpdateUser")
		}
	}
	return driver.RowsAffected(1), nil
}

type guardTestTx struct{}

func (guardTestTx) Commit() error {
	return nil
}

func (guardTestTx) Rollback() error {
	return nil
}

type guardTestRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func newGuardRows(columns []string, values [][]driver.Value) *guardTestRows {
	return &guardTestRows{columns: columns, values: values}
}

func (r *guardTestRows) Columns() []string {
	return r.columns
}

func (r *guardTestRows) Close() error {
	return nil
}

func (r *guardTestRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func assertJSONSuccess(t *testing.T, resp *httptest.ResponseRecorder, want bool) {
	t.Helper()
	var body struct {
		Success bool `json:"success"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body %q: %v", resp.Body.String(), err)
	}
	if body.Success != want {
		t.Fatalf("success = %v, want %v; body=%s", body.Success, want, resp.Body.String())
	}
}
