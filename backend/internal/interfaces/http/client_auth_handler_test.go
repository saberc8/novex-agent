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

	"voc-go-backend/internal/infrastructure/security"
)

func TestClientAuthRegistersLogsInAndLoadsProfile(t *testing.T) {
	router, _ := newClientAuthTestRouter(t, newClientAuthTestStore())

	registerResp := performClientAuthRequest(router, nethttp.MethodPost, "/client/auth/register", "", `{
		"phone":"13800138000",
		"password":"secret123",
		"nickname":"小明",
		"clientId":"h5-client"
	}`)
	assertBusinessCode(t, registerResp, "200")

	loginResp := performClientAuthRequest(router, nethttp.MethodPost, "/client/auth/login", "", `{
		"phone":"13800138000",
		"password":"secret123",
		"clientId":"h5-client"
	}`)
	assertBusinessCode(t, loginResp, "200")
	token := extractClientToken(t, loginResp)

	profileResp := performClientAuthRequest(router, nethttp.MethodGet, "/client/user/profile", "Bearer "+token, "")
	assertBusinessCode(t, profileResp, "200")
	assertClientProfilePhone(t, profileResp, "13800138000")
}

func TestClientAuthRejectsAdminToken(t *testing.T) {
	router, tokenSvc := newClientAuthTestRouter(t, newClientAuthTestStore())
	token, err := tokenSvc.Generate(1)
	if err != nil {
		t.Fatalf("Generate: %v", err)
	}

	resp := performClientAuthRequest(router, nethttp.MethodGet, "/client/user/profile", "Bearer "+token, "")

	assertBusinessCode(t, resp, "401")
}

func newClientAuthTestRouter(t *testing.T, store *clientAuthTestStore) (*gin.Engine, *security.TokenService) {
	t.Helper()
	gin.SetMode(gin.TestMode)
	db := sql.OpenDB(clientAuthTestConnector{store: store})
	tokenSvc := security.NewTokenService("client-secret", time.Hour)
	handler := NewClientAuthHandler(db, tokenSvc, security.BcryptHasher{}, security.BcryptVerifier{})
	router := gin.New()
	handler.RegisterPublicClientRoutes(router)
	protected := router.Group("")
	protected.Use(RequireClientAuth(db, tokenSvc))
	handler.RegisterProtectedClientRoutes(protected)
	t.Cleanup(func() { _ = db.Close() })
	return router, tokenSvc
}

func performClientAuthRequest(router *gin.Engine, method, path, authz, body string) *httptest.ResponseRecorder {
	req := httptest.NewRequest(method, path, bytes.NewBufferString(body))
	req.Header.Set("Content-Type", "application/json")
	if authz != "" {
		req.Header.Set("Authorization", authz)
	}
	resp := httptest.NewRecorder()
	router.ServeHTTP(resp, req)
	return resp
}

func extractClientToken(t *testing.T, resp *httptest.ResponseRecorder) string {
	t.Helper()
	var body struct {
		Data struct {
			Token string `json:"token"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body %q: %v", resp.Body.String(), err)
	}
	if body.Data.Token == "" {
		t.Fatalf("empty token in body: %s", resp.Body.String())
	}
	return body.Data.Token
}

func assertClientProfilePhone(t *testing.T, resp *httptest.ResponseRecorder, want string) {
	t.Helper()
	var body struct {
		Data struct {
			Phone string `json:"phone"`
		} `json:"data"`
	}
	if err := json.Unmarshal(resp.Body.Bytes(), &body); err != nil {
		t.Fatalf("decode response body %q: %v", resp.Body.String(), err)
	}
	if body.Data.Phone != want {
		t.Fatalf("profile phone = %q, want %q; body=%s", body.Data.Phone, want, resp.Body.String())
	}
}

type clientAuthTestUser struct {
	id       int64
	phone    string
	password string
	nickname string
	avatar   string
	status   int64
}

type clientAuthTestStore struct {
	mu         sync.Mutex
	nextID     int64
	activeH5   map[string]bool
	users      map[int64]clientAuthTestUser
	phoneIndex map[string]int64
}

func newClientAuthTestStore() *clientAuthTestStore {
	return &clientAuthTestStore{
		nextID:     100,
		activeH5:   map[string]bool{"h5-client": true},
		users:      map[int64]clientAuthTestUser{},
		phoneIndex: map[string]int64{},
	}
}

type clientAuthTestConnector struct {
	store *clientAuthTestStore
}

func (c clientAuthTestConnector) Connect(context.Context) (driver.Conn, error) {
	return clientAuthTestConn{store: c.store}, nil
}

func (c clientAuthTestConnector) Driver() driver.Driver {
	return clientAuthTestDriver{}
}

type clientAuthTestDriver struct{}

func (clientAuthTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use OpenDB with clientAuthTestConnector")
}

type clientAuthTestConn struct {
	store *clientAuthTestStore
}

func (c clientAuthTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented")
}

func (c clientAuthTestConn) Close() error {
	return nil
}

func (c clientAuthTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not implemented")
}

func (c clientAuthTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	compact := strings.ToLower(strings.Join(strings.Fields(query), " "))
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	switch {
	case strings.Contains(compact, "from sys_client"):
		clientID := asString(args[0].Value)
		active := int64(0)
		if c.store.activeH5[clientID] {
			active = 1
		}
		return newClientAuthRows([]string{"active"}, [][]driver.Value{{active}}), nil
	case strings.Contains(compact, "from client_user") && strings.Contains(compact, "where phone"):
		phone := asString(args[0].Value)
		id, ok := c.store.phoneIndex[phone]
		if !ok {
			return newClientAuthRows([]string{"id", "phone", "password", "nickname", "avatar", "status"}, nil), nil
		}
		u := c.store.users[id]
		return newClientAuthRows(
			[]string{"id", "phone", "password", "nickname", "avatar", "status"},
			[][]driver.Value{{u.id, u.phone, u.password, u.nickname, u.avatar, u.status}},
		), nil
	case strings.Contains(compact, "from client_user") && strings.Contains(compact, "where id"):
		id := asInt64(args[0].Value)
		u, ok := c.store.users[id]
		if !ok {
			return newClientAuthRows([]string{"id", "phone", "nickname", "avatar", "status"}, nil), nil
		}
		return newClientAuthRows(
			[]string{"id", "phone", "nickname", "avatar", "status"},
			[][]driver.Value{{u.id, u.phone, u.nickname, u.avatar, u.status}},
		), nil
	default:
		return nil, errors.New("unexpected query: " + compact)
	}
}

func (c clientAuthTestConn) ExecContext(_ context.Context, query string, args []driver.NamedValue) (driver.Result, error) {
	compact := strings.ToLower(strings.Join(strings.Fields(query), " "))
	c.store.mu.Lock()
	defer c.store.mu.Unlock()
	switch {
	case strings.Contains(compact, "insert into client_user"):
		id := asInt64(args[0].Value)
		u := clientAuthTestUser{
			id:       id,
			phone:    asString(args[1].Value),
			password: asString(args[2].Value),
			nickname: asString(args[3].Value),
			avatar:   asString(args[4].Value),
			status:   asInt64(args[5].Value),
		}
		c.store.users[id] = u
		c.store.phoneIndex[u.phone] = id
		return driver.RowsAffected(1), nil
	case strings.Contains(compact, "update client_user"):
		return driver.RowsAffected(1), nil
	default:
		return nil, errors.New("unexpected exec: " + compact)
	}
}

type clientAuthTestRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func newClientAuthRows(columns []string, values [][]driver.Value) *clientAuthTestRows {
	return &clientAuthTestRows{columns: columns, values: values}
}

func (r *clientAuthTestRows) Columns() []string {
	return r.columns
}

func (r *clientAuthTestRows) Close() error {
	return nil
}

func (r *clientAuthTestRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func asString(v any) string {
	if s, ok := v.(string); ok {
		return s
	}
	return ""
}
