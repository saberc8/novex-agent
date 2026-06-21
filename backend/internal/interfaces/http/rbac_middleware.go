package http

import (
	"context"
	"database/sql"
	"errors"
	nethttp "net/http"
	"strings"

	"github.com/gin-gonic/gin"

	"voc-go-backend/internal/infrastructure/security"
)

// RoutePermission maps a Gin route to the permission required by that route.
// Empty Permission means the route only requires a valid authenticated user.
type RoutePermission struct {
	Method         string
	Path           string
	Permission     string
	AnyPermissions []string
}

// RequireRBAC authenticates the current request and enforces route permission
// checks for routes registered after this middleware is applied.
func RequireRBAC(db *sql.DB, tokenSvc *security.TokenService, routes []RoutePermission) gin.HandlerFunc {
	routeIndex := make(map[string]RoutePermission, len(routes))
	for _, route := range routes {
		method := strings.ToUpper(strings.TrimSpace(route.Method))
		path := strings.TrimSpace(route.Path)
		if method == "" || path == "" {
			continue
		}
		route.Permission = strings.TrimSpace(route.Permission)
		for i := range route.AnyPermissions {
			route.AnyPermissions[i] = strings.TrimSpace(route.AnyPermissions[i])
		}
		routeIndex[routeKey(method, path)] = route
	}

	return func(c *gin.Context) {
		principal, err := loadPrincipal(c.Request.Context(), db, tokenSvc, c.GetHeader("Authorization"))
		if err != nil {
			Fail(c, "401", "未授权，请重新登录")
			c.Abort()
			return
		}
		setCurrentPrincipal(c, principal)

		route, ok := routeIndex[routeKey(c.Request.Method, c.FullPath())]
		if !ok {
			Fail(c, "403", "没有访问权限")
			c.Abort()
			return
		}
		if routeAllowsPrincipal(route, principal) {
			c.Next()
			return
		}
		Fail(c, "403", "没有访问权限")
		c.Abort()
	}
}

func routeAllowsPrincipal(route RoutePermission, principal *Principal) bool {
	if strings.TrimSpace(route.Permission) == "" && len(route.AnyPermissions) == 0 {
		return true
	}
	if principal.HasPermission(route.Permission) {
		return true
	}
	for _, permission := range route.AnyPermissions {
		if permission != "" && principal.HasPermission(permission) {
			return true
		}
	}
	return false
}

func routeKey(method, path string) string {
	return strings.ToUpper(strings.TrimSpace(method)) + " " + strings.TrimSpace(path)
}

func loadPrincipal(ctx context.Context, db *sql.DB, tokenSvc *security.TokenService, authz string) (*Principal, error) {
	if db == nil || tokenSvc == nil {
		return nil, errors.New("rbac dependency is nil")
	}
	claims, err := tokenSvc.Parse(authz)
	if err != nil || claims.UserID == 0 {
		return nil, errors.New("invalid token")
	}
	if claims.Subject != "" && claims.Subject != security.SubjectAdmin {
		return nil, errors.New("invalid admin token subject")
	}

	var principal Principal
	const userQuery = `
SELECT id,
       username,
       COALESCE(status, 0),
       COALESCE(is_system, FALSE),
       COALESCE(dept_id, 0)
FROM sys_user
WHERE id = $1
LIMIT 1;
`
	var status int16
	if err := db.QueryRowContext(ctx, userQuery, claims.UserID).Scan(
		&principal.UserID,
		&principal.Username,
		&status,
		&principal.IsSystem,
		&principal.DeptID,
	); err != nil {
		return nil, err
	}
	if status != 1 {
		return nil, errors.New("user disabled")
	}

	roles, err := loadPrincipalRoles(ctx, db, principal.UserID)
	if err != nil {
		return nil, err
	}
	principal.Roles = roles
	for _, role := range roles {
		if role.Code == "admin" {
			principal.IsAdmin = true
			break
		}
	}

	perms, err := loadPrincipalPermissions(ctx, db, principal.UserID)
	if err != nil {
		return nil, err
	}
	principal.Permissions = perms
	if principal.IsAdmin {
		principal.Permissions["*:*:*"] = struct{}{}
	}
	return &principal, nil
}

func loadPrincipalRoles(ctx context.Context, db *sql.DB, userID int64) ([]PrincipalRole, error) {
	const query = `
SELECT r.id,
       r.code,
       COALESCE(r.data_scope, 4)
FROM sys_role AS r
JOIN sys_user_role AS ur ON ur.role_id = r.id
WHERE ur.user_id = $1;
`
	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := make([]PrincipalRole, 0)
	for rows.Next() {
		var role PrincipalRole
		if err := rows.Scan(&role.ID, &role.Code, &role.DataScope); err != nil {
			return nil, err
		}
		roles = append(roles, role)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return roles, nil
}

func loadPrincipalPermissions(ctx context.Context, db *sql.DB, userID int64) (map[string]struct{}, error) {
	const query = `
SELECT DISTINCT m.permission
FROM sys_menu AS m
JOIN sys_role_menu AS rm ON rm.menu_id = m.id
JOIN sys_role AS r ON r.id = rm.role_id
JOIN sys_user_role AS ur ON ur.role_id = r.id
JOIN sys_user AS u ON u.id = ur.user_id
WHERE u.id = $1
  AND m.status = 1
  AND m.permission IS NOT NULL
  AND m.permission <> '';
`
	rows, err := db.QueryContext(ctx, query, userID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	perms := make(map[string]struct{})
	for rows.Next() {
		var permission string
		if err := rows.Scan(&permission); err != nil {
			return nil, err
		}
		perms[permission] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return perms, nil
}

// DefaultRoutePermissions returns the backend RBAC contract for protected APIs.
func DefaultRoutePermissions() []RoutePermission {
	return []RoutePermission{
		{Method: nethttp.MethodPost, Path: "/auth/logout"},
		{Method: nethttp.MethodGet, Path: "/auth/user/info"},
		{Method: nethttp.MethodGet, Path: "/auth/user/route"},
		{Method: nethttp.MethodPatch, Path: "/user/profile/avatar"},
		{Method: nethttp.MethodPatch, Path: "/user/profile/basic/info"},
		{Method: nethttp.MethodPatch, Path: "/user/profile/password"},

		{Method: nethttp.MethodGet, Path: "/common/tree/menu", Permission: "system:menu:list"},
		{Method: nethttp.MethodGet, Path: "/common/tree/dept", Permission: "system:dept:list"},
		{Method: nethttp.MethodGet, Path: "/common/dict/user", Permission: "system:user:list"},
		{Method: nethttp.MethodGet, Path: "/common/dict/role", Permission: "system:role:list"},
		{Method: nethttp.MethodPost, Path: "/common/file", Permission: "system:file:upload"},

		{Method: nethttp.MethodGet, Path: "/monitor/online", Permission: "monitor:online:list"},
		{Method: nethttp.MethodDelete, Path: "/monitor/online/:token", Permission: "monitor:online:kickout"},

		{Method: nethttp.MethodGet, Path: "/system/log", Permission: "monitor:log:list"},
		{Method: nethttp.MethodGet, Path: "/system/log/:id", Permission: "monitor:log:get"},
		{Method: nethttp.MethodGet, Path: "/system/log/export/login", Permission: "monitor:log:export"},
		{Method: nethttp.MethodGet, Path: "/system/log/export/operation", Permission: "monitor:log:export"},

		{Method: nethttp.MethodGet, Path: "/system/user", Permission: "system:user:list"},
		{Method: nethttp.MethodGet, Path: "/system/user/list", Permission: "system:user:list"},
		{Method: nethttp.MethodGet, Path: "/system/user/:id", Permission: "system:user:get"},
		{Method: nethttp.MethodPost, Path: "/system/user", Permission: "system:user:create"},
		{Method: nethttp.MethodPut, Path: "/system/user/:id", Permission: "system:user:update"},
		{Method: nethttp.MethodDelete, Path: "/system/user", Permission: "system:user:delete"},
		{Method: nethttp.MethodPatch, Path: "/system/user/:id/password", Permission: "system:user:resetPwd"},
		{Method: nethttp.MethodPatch, Path: "/system/user/:id/role", Permission: "system:user:updateRole"},
		{Method: nethttp.MethodGet, Path: "/system/user/export", Permission: "system:user:export"},
		{Method: nethttp.MethodGet, Path: "/system/user/import/template", Permission: "system:user:import"},
		{Method: nethttp.MethodPost, Path: "/system/user/import/parse", Permission: "system:user:import"},
		{Method: nethttp.MethodPost, Path: "/system/user/import", Permission: "system:user:import"},

		{Method: nethttp.MethodGet, Path: "/system/role/list", Permission: "system:role:list"},
		{Method: nethttp.MethodGet, Path: "/system/role/:id", Permission: "system:role:get"},
		{Method: nethttp.MethodPost, Path: "/system/role", Permission: "system:role:create"},
		{Method: nethttp.MethodPut, Path: "/system/role/:id", Permission: "system:role:update"},
		{Method: nethttp.MethodDelete, Path: "/system/role", Permission: "system:role:delete"},
		{Method: nethttp.MethodPut, Path: "/system/role/:id/permission", Permission: "system:role:updatePermission"},
		{Method: nethttp.MethodGet, Path: "/system/role/:id/user", Permission: "system:role:list"},
		{Method: nethttp.MethodPost, Path: "/system/role/:id/user", Permission: "system:role:assign"},
		{Method: nethttp.MethodDelete, Path: "/system/role/user", Permission: "system:role:unassign"},
		{Method: nethttp.MethodGet, Path: "/system/role/:id/user/id", Permission: "system:role:list"},

		{Method: nethttp.MethodGet, Path: "/system/menu/tree", Permission: "system:menu:list"},
		{Method: nethttp.MethodGet, Path: "/system/menu/:id", Permission: "system:menu:get"},
		{Method: nethttp.MethodPost, Path: "/system/menu", Permission: "system:menu:create"},
		{Method: nethttp.MethodPut, Path: "/system/menu/:id", Permission: "system:menu:update"},
		{Method: nethttp.MethodDelete, Path: "/system/menu", Permission: "system:menu:delete"},
		{Method: nethttp.MethodDelete, Path: "/system/menu/cache", Permission: "system:menu:clearCache"},

		{Method: nethttp.MethodGet, Path: "/system/dept/tree", Permission: "system:dept:list"},
		{Method: nethttp.MethodGet, Path: "/system/dept/:id", Permission: "system:dept:get"},
		{Method: nethttp.MethodPost, Path: "/system/dept", Permission: "system:dept:create"},
		{Method: nethttp.MethodPut, Path: "/system/dept/:id", Permission: "system:dept:update"},
		{Method: nethttp.MethodDelete, Path: "/system/dept", Permission: "system:dept:delete"},
		{Method: nethttp.MethodGet, Path: "/system/dept/export", Permission: "system:dept:export"},

		{Method: nethttp.MethodGet, Path: "/system/dict/list", Permission: "system:dict:list"},
		{Method: nethttp.MethodGet, Path: "/system/dict/:id", Permission: "system:dict:get"},
		{Method: nethttp.MethodPost, Path: "/system/dict", Permission: "system:dict:create"},
		{Method: nethttp.MethodPut, Path: "/system/dict/:id", Permission: "system:dict:update"},
		{Method: nethttp.MethodDelete, Path: "/system/dict", Permission: "system:dict:delete"},
		{Method: nethttp.MethodDelete, Path: "/system/dict/cache/:code", Permission: "system:dict:item:clearCache"},
		{Method: nethttp.MethodGet, Path: "/system/dict/item", Permission: "system:dict:item:list"},
		{Method: nethttp.MethodGet, Path: "/system/dict/item/:id", Permission: "system:dict:item:get"},
		{Method: nethttp.MethodPost, Path: "/system/dict/item", Permission: "system:dict:item:create"},
		{Method: nethttp.MethodPut, Path: "/system/dict/item/:id", Permission: "system:dict:item:update"},
		{Method: nethttp.MethodDelete, Path: "/system/dict/item", Permission: "system:dict:item:delete"},

		{
			Method: nethttp.MethodGet,
			Path:   "/system/option",
			AnyPermissions: []string{
				"system:siteConfig:get",
				"system:securityConfig:get",
				"system:loginConfig:get",
			},
		},
		{
			Method: nethttp.MethodPut,
			Path:   "/system/option",
			AnyPermissions: []string{
				"system:siteConfig:update",
				"system:securityConfig:update",
				"system:loginConfig:update",
			},
		},
		{
			Method: nethttp.MethodPatch,
			Path:   "/system/option/value",
			AnyPermissions: []string{
				"system:siteConfig:update",
				"system:securityConfig:update",
				"system:loginConfig:update",
			},
		},

		{Method: nethttp.MethodGet, Path: "/system/storage/list", Permission: "system:storage:list"},
		{Method: nethttp.MethodGet, Path: "/system/storage/:id", Permission: "system:storage:get"},
		{Method: nethttp.MethodPost, Path: "/system/storage", Permission: "system:storage:create"},
		{Method: nethttp.MethodPut, Path: "/system/storage/:id", Permission: "system:storage:update"},
		{Method: nethttp.MethodDelete, Path: "/system/storage", Permission: "system:storage:delete"},
		{Method: nethttp.MethodPut, Path: "/system/storage/:id/status", Permission: "system:storage:updateStatus"},
		{Method: nethttp.MethodPut, Path: "/system/storage/:id/default", Permission: "system:storage:setDefault"},

		{Method: nethttp.MethodGet, Path: "/system/client", Permission: "system:client:list"},
		{Method: nethttp.MethodGet, Path: "/system/client/:id", Permission: "system:client:get"},
		{Method: nethttp.MethodPost, Path: "/system/client", Permission: "system:client:create"},
		{Method: nethttp.MethodPut, Path: "/system/client/:id", Permission: "system:client:update"},
		{Method: nethttp.MethodDelete, Path: "/system/client", Permission: "system:client:delete"},

		{Method: nethttp.MethodGet, Path: "/system/file", Permission: "system:file:list"},
		{Method: nethttp.MethodPost, Path: "/system/file/upload", Permission: "system:file:upload"},
		{Method: nethttp.MethodPost, Path: "/system/file/dir", Permission: "system:file:createDir"},
		{Method: nethttp.MethodGet, Path: "/system/file/dir/:id/size", Permission: "system:file:calcDirSize"},
		{Method: nethttp.MethodGet, Path: "/system/file/statistics", Permission: "system:file:list"},
		{Method: nethttp.MethodGet, Path: "/system/file/check", Permission: "system:file:list"},
		{Method: nethttp.MethodPut, Path: "/system/file/:id", Permission: "system:file:update"},
		{Method: nethttp.MethodDelete, Path: "/system/file", Permission: "system:file:delete"},
	}
}
