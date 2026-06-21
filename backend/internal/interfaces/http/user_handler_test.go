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
	"sync"
	"testing"
	"time"

	"github.com/gin-gonic/gin"

	rbacdomain "voc-go-backend/internal/domain/rbac"
	domainuser "voc-go-backend/internal/domain/user"
	"voc-go-backend/internal/infrastructure/security"
)

func TestGetUserInfoIncludesDeptNameAndPwdExpired(t *testing.T) {
	resetTime := time.Now().AddDate(0, 0, -31)
	user := &domainuser.User{
		ID:           7,
		Username:     "operator",
		Nickname:     "操作员",
		Password:     "old-password",
		Status:       1,
		DeptID:       10,
		PwdResetTime: &resetTime,
		CreateTime:   time.Now().AddDate(0, -1, 0),
	}
	router, store := newUserHandlerTestRouter(t, user, &userHandlerTestStore{
		deptNames:       map[int64]string{10: "研发部"},
		passwordOptions: map[string]string{"PASSWORD_EXPIRATION_DAYS": "30"},
	})

	resp := performUserHandlerRequest(router, nethttp.MethodGet, "/auth/user/info", "")

	assertBusinessCode(t, resp, "200")
	var body struct {
		Data struct {
			DeptName   string `json:"deptName"`
			PwdExpired bool   `json:"pwdExpired"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body %q: %v", resp.Body.String(), err)
	}
	if body.Data.DeptName != "研发部" {
		t.Fatalf("deptName = %q, want 研发部; body=%s", body.Data.DeptName, resp.Body.String())
	}
	if !body.Data.PwdExpired {
		t.Fatalf("pwdExpired = false, want true; store=%+v body=%s", store, resp.Body.String())
	}
}

func TestUserProfileUpdatesBasicInfoAndPassword(t *testing.T) {
	resetTime := time.Now().AddDate(0, 0, -1)
	user := &domainuser.User{
		ID:           7,
		Username:     "operator",
		Nickname:     "操作员",
		Password:     "old-password",
		Status:       1,
		DeptID:       10,
		PwdResetTime: &resetTime,
		CreateTime:   time.Now().AddDate(0, -1, 0),
	}
	router, store := newUserHandlerTestRouter(t, user, &userHandlerTestStore{
		deptNames:       map[int64]string{10: "研发部"},
		passwordOptions: map[string]string{"PASSWORD_EXPIRATION_DAYS": "0"},
	})

	basicResp := performUserHandlerRequest(router, nethttp.MethodPatch, "/user/profile/basic/info", `{
		"nickname":"新昵称",
		"gender":2
	}`)
	assertBusinessCode(t, basicResp, "200")
	if store.nickname != "新昵称" || store.gender != 2 {
		t.Fatalf("basic info update = (%q, %d), want (新昵称, 2)", store.nickname, store.gender)
	}

	passwordResp := performUserHandlerRequest(router, nethttp.MethodPatch, "/user/profile/password", `{
		"oldPassword":"old-password",
		"newPassword":"new-password"
	}`)
	assertBusinessCode(t, passwordResp, "200")
	if store.password != "hashed:new-password" {
		t.Fatalf("password = %q, want hashed:new-password", store.password)
	}
	if store.pwdResetTime.IsZero() {
		t.Fatal("pwdResetTime was not updated")
	}
}

func newUserHandlerTestRouter(t *testing.T, u *domainuser.User, store *userHandlerTestStore) (*gin.Engine, *userHandlerTestStore) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	store.userID = u.ID
	store.nickname = u.Nickname
	store.gender = u.Gender
	store.password = u.Password
	db := sql.OpenDB(userHandlerTestConnector{store: store})
	t.Cleanup(func() { _ = db.Close() })
	handler := NewUserHandler(
		userHandlerRepo{user: u},
		userHandlerRoleRepo{},
		userHandlerMenuRepo{},
		security.NewTokenService("user-handler-secret", time.Hour),
		db,
		nil,
		userHandlerHasher{},
		userHandlerVerifier{},
	)
	router := gin.New()
	router.Use(func(c *gin.Context) {
		setCurrentPrincipal(c, &Principal{
			UserID:      u.ID,
			Username:    u.Username,
			DeptID:      u.DeptID,
			Roles:       []PrincipalRole{{ID: 2, Code: "operator", DataScope: dataScopeAll}},
			Permissions: map[string]struct{}{},
		})
		c.Next()
	})
	handler.RegisterUserRoutes(router)
	return router, store
}

func performUserHandlerRequest(router *gin.Engine, method, path, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

type userHandlerRepo struct {
	user *domainuser.User
}

func (r userHandlerRepo) GetByUsername(context.Context, string) (*domainuser.User, error) {
	return r.user, nil
}

func (r userHandlerRepo) GetByID(context.Context, int64) (*domainuser.User, error) {
	return r.user, nil
}

type userHandlerRoleRepo struct{}

func (userHandlerRoleRepo) ListByUserID(context.Context, int64) ([]rbacdomain.Role, error) {
	return []rbacdomain.Role{{ID: 2, Name: "操作员", Code: "operator"}}, nil
}

func (userHandlerRoleRepo) ListCodesByUserID(context.Context, int64) ([]string, error) {
	return []string{"operator"}, nil
}

type userHandlerMenuRepo struct{}

func (userHandlerMenuRepo) ListByRoleID(context.Context, int64) ([]rbacdomain.Menu, error) {
	return []rbacdomain.Menu{}, nil
}

func (userHandlerMenuRepo) ListPermissionsByUserID(context.Context, int64) ([]string, error) {
	return []string{"system:user:list"}, nil
}

type userHandlerHasher struct{}

func (userHandlerHasher) Hash(raw string) (string, error) {
	return "hashed:" + raw, nil
}

type userHandlerVerifier struct{}

func (userHandlerVerifier) Verify(raw, encoded string) (bool, error) {
	return raw == encoded, nil
}

type userHandlerTestStore struct {
	mu              sync.Mutex
	userID          int64
	nickname        string
	gender          int16
	password        string
	pwdResetTime    time.Time
	deptNames       map[int64]string
	passwordOptions map[string]string
}

type userHandlerTestConnector struct {
	store *userHandlerTestStore
}

func (c userHandlerTestConnector) Connect(context.Context) (driver.Conn, error) {
	return userHandlerTestConn{store: c.store}, nil
}

func (c userHandlerTestConnector) Driver() driver.Driver {
	return userHandlerTestDriver{}
}

type userHandlerTestDriver struct{}

func (userHandlerTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use OpenDB with userHandlerTestConnector")
}

type userHandlerTestConn struct {
	store *userHandlerTestStore
}

func (c userHandlerTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented")
}

func (c userHandlerTestConn) Close() error {
	return nil
}

func (c userHandlerTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not implemented")
}

func (c userHandlerTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	compact := strings.ToLower(strings.Join(strings.Fields(query), " "))
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	switch {
	case strings.Contains(compact, "from sys_dept"):
		deptID := asInt64(args[0].Value)
		name := c.store.deptNames[deptID]
		if name == "" {
			return newUserHandlerRows([]string{"name"}, nil), nil
		}
		return newUserHandlerRows([]string{"name"}, [][]driver.Value{{name}}), nil
	case strings.Contains(compact, "from sys_option"):
		code := asString(args[0].Value)
		value := c.store.passwordOptions[code]
		if value == "" {
			value = "0"
		}
		return newUserHandlerRows([]string{"value"}, [][]driver.Value{{value}}), nil
	default:
		return nil, errors.New("unexpected query: " + compact)
	}
}

func (c userHandlerTestConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	compact := strings.ToLower(strings.Join(strings.Fields(query), " "))
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	switch {
	case strings.Contains(compact, "set nickname"):
		c.store.nickname = asString(args[0].Value)
		c.store.gender = int16(asInt64(args[1].Value))
		return driver.RowsAffected(1), nil
	case strings.Contains(compact, "set password"):
		c.store.password = asString(args[0].Value)
		if t, ok := args[1].Value.(time.Time); ok {
			c.store.pwdResetTime = t
		}
		return driver.RowsAffected(1), nil
	default:
		return nil, errors.New("unexpected exec: " + compact)
	}
}

type userHandlerRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func newUserHandlerRows(columns []string, values [][]driver.Value) *userHandlerRows {
	return &userHandlerRows{columns: columns, values: values}
}

func (r *userHandlerRows) Columns() []string {
	return r.columns
}

func (r *userHandlerRows) Close() error {
	return nil
}

func (r *userHandlerRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
