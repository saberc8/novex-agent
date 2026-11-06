package novex

import (
	"context"
	"crypto/rand"
	"database/sql"
	"encoding/hex"
	"fmt"
	"net/http"
	"os"
	"path/filepath"
	"strconv"
	"strings"
	"sync"
	"time"

	"github.com/gin-gonic/gin"
	"voc-go-backend/internal/infrastructure/security"
)

type User struct {
	NovexEnabled     bool   `json:"novexEnabled"`
	SystemDeptID     int64  `json:"systemDeptId"`
	Status           int16  `json:"status"`
	IsSystem         bool   `json:"isSystem"`
	ID               int64  `json:"id"`
	Username         string `json:"username"`
	Nickname         string `json:"nickname"`
	TenantID         string `json:"tenantId"`
	DepartmentID     string `json:"departmentId"`
	IsAdmin          bool   `json:"isAdmin"`
	GlobalDataAccess bool   `json:"globalDataAccess"`
	Roles            []Role `json:"roles"`
	PolicyVersion    int64  `json:"policyVersion"`
}
type Role struct {
	ID   int64  `json:"id"`
	Name string `json:"name"`
	Code string `json:"code"`
}
type Datasource struct {
	TenantID    string    `json:"tenantId"`
	ID          string    `json:"id"`
	Name        string    `json:"name"`
	Host        string    `json:"host"`
	Port        int       `json:"port"`
	Database    string    `json:"database"`
	Username    string    `json:"username"`
	Password    string    `json:"password,omitempty"`
	SSLMode     string    `json:"sslMode"`
	HasPassword bool      `json:"hasPassword"`
	CreatedAt   time.Time `json:"createdAt"`
	encrypted   string
}
type Column struct {
	Name     string `json:"name"`
	DataType string `json:"dataType"`
	Nullable bool   `json:"nullable"`
}
type Table struct {
	Schema  string   `json:"schema"`
	Name    string   `json:"name"`
	Columns []Column `json:"columns"`
}
type Resource struct {
	ID               string   `json:"id"`
	Name             string   `json:"name"`
	Key              string   `json:"key"`
	DatasourceID     string   `json:"datasourceId"`
	Schema           string   `json:"schema"`
	Table            string   `json:"table"`
	TenantColumn     string   `json:"tenantColumn"`
	OwnerColumn      string   `json:"ownerColumn"`
	DepartmentColumn string   `json:"departmentColumn"`
	WarehouseColumn  string   `json:"warehouseColumn"`
	IdentityRequired bool     `json:"identityRequired"`
	Columns          []string `json:"columns"`
}
type Identity struct {
	ExternalTenantID string   `json:"externalTenantId"`
	ID               string   `json:"id"`
	UserID           int64    `json:"userId"`
	DatasourceID     string   `json:"datasourceId"`
	TenantID         string   `json:"tenantId"`
	EmployeeID       string   `json:"employeeId"`
	DepartmentID     string   `json:"departmentId"`
	WarehouseIDs     []string `json:"warehouseIds"`
}
type Grant struct {
	ID         string            `json:"id"`
	RoleID     int64             `json:"roleId"`
	ResourceID string            `json:"resourceId"`
	Scope      string            `json:"scope"`
	Fields     map[string]string `json:"fields"`
}
type Filter struct {
	Field string `json:"field"`
	Op    string `json:"op"`
	Value any    `json:"value"`
}
type Sort struct {
	Field     string `json:"field"`
	Direction string `json:"direction"`
}
type Query struct {
	UserID     int64    `json:"userId"`
	ResourceID string   `json:"resourceId"`
	Fields     []string `json:"fields"`
	Filters    []Filter `json:"filters"`
	Sort       *Sort    `json:"sort"`
	Limit      int      `json:"limit"`
}
type Document struct {
	Kind         string    `json:"kind"`
	SheetCount   int       `json:"sheetCount,omitempty"`
	RowCount     int       `json:"rowCount,omitempty"`
	Sheets       []Sheet   `json:"sheets,omitempty"`
	Warnings     []string  `json:"warnings,omitempty"`
	ID           string    `json:"id"`
	Title        string    `json:"title"`
	FileName     string    `json:"fileName"`
	Scope        string    `json:"scope"`
	OwnerID      int64     `json:"ownerId"`
	DepartmentID string    `json:"departmentId"`
	TenantID     string    `json:"tenantId"`
	CreatedAt    time.Time `json:"createdAt"`
	Characters   int       `json:"characters"`
	Markdown     string    `json:"markdown,omitempty"`
}
type Server struct {
	DB            *sql.DB
	Tokens        *security.TokenService
	key           []byte
	DataDir       string
	RunnerURL     string
	PublicURL     string
	RunnerSecret  string
	client        *http.Client
	mu            sync.RWMutex
	active        map[string]context.CancelFunc
	subscribers   map[string][]chan Event
	loginMu       sync.Mutex
	loginAttempts map[string]*loginAttempt
}
type loginAttempt struct {
	Count int
	Until time.Time
}
type Event struct {
	Kind string
	Data any
}

func New(db *sql.DB, tokens *security.TokenService) (*Server, error) {
	key, err := loadKey()
	if err != nil {
		return nil, err
	}
	dir, err := filepath.Abs(env("NOVEX_DATA_DIR", "./data/novex"))
	if err != nil {
		return nil, err
	}
	if err = os.MkdirAll(dir, 0700); err != nil {
		return nil, err
	}
	s := &Server{DB: db, Tokens: tokens, key: key, DataDir: dir, RunnerURL: env("NOVEX_RUNNER_URL", "http://127.0.0.1:8320"), PublicURL: env("NOVEX_PUBLIC_API_URL", env("NOVEX_PUBLIC_URL", "http://127.0.0.1:8310")), RunnerSecret: os.Getenv("NOVEX_RUNNER_SECRET"), client: &http.Client{Timeout: 10 * time.Minute}, active: map[string]context.CancelFunc{}, subscribers: map[string][]chan Event{}, loginAttempts: map[string]*loginAttempt{}}
	if err = s.Migrate(); err != nil {
		return nil, err
	}
	_, err = s.DB.Exec(`UPDATE novex_runs SET status='error',error='服务已重启，请重新发送' WHERE status IN ('queued','running')`)
	return s, err
}
func env(k, d string) string {
	if v := os.Getenv(k); v != "" {
		return v
	}
	return d
}
func id() string {
	b := make([]byte, 16)
	if _, err := rand.Read(b); err != nil {
		panic(err)
	}
	return hex.EncodeToString(b)
}
func ok(c *gin.Context, data any) {
	c.JSON(200, gin.H{"code": "200", "success": true, "data": data, "msg": "操作成功"})
}
func fail(c *gin.Context, status int, msg string) {
	c.AbortWithStatusJSON(status, gin.H{"code": strconv.Itoa(status), "success": false, "data": nil, "msg": msg})
}
func actor(c *gin.Context) *User { v, _ := c.Get("novex.user"); u, _ := v.(*User); return u }
func bind(c *gin.Context, v any) bool {
	if err := c.ShouldBindJSON(v); err != nil {
		fail(c, 400, "请求JSON格式错误")
		return false
	}
	return true
}
func (s *Server) epoch(ctx context.Context) (int64, error) {
	var v int64
	err := s.DB.QueryRowContext(ctx, `SELECT version FROM novex_policy_version WHERE id=1`).Scan(&v)
	return v, err
}
func (s *Server) audit(ctx context.Context, u int64, action, resource string, allowed bool, detail string) {
	_, _ = s.DB.ExecContext(ctx, `INSERT INTO novex_audit(id,user_id,action,resource_id,allowed,detail) VALUES($1,$2,$3,$4,$5,$6)`, id(), u, action, resource, allowed, detail)
}
func numericID(v string) (int64, error) {
	n, e := strconv.ParseInt(v, 10, 64)
	if e != nil || n <= 0 {
		return 0, fmt.Errorf("invalid id")
	}
	return n, nil
}
func contains(xs []string, x string) bool {
	for _, v := range xs {
		if v == x {
			return true
		}
	}
	return false
}
func validScope(v string) bool {
	return v == "self" || v == "department" || v == "warehouse" || v == "tenant"
}
func blank(v string) bool { return strings.TrimSpace(v) == "" }
