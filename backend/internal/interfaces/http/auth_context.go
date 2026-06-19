package http

import "github.com/gin-gonic/gin"

const principalContextKey = "auth.principal"

// PrincipalRole is a role bound to the authenticated user.
type PrincipalRole struct {
	ID        int64
	Code      string
	DataScope int32
}

// Principal is the authenticated user resolved once by the RBAC middleware.
type Principal struct {
	UserID      int64
	Username    string
	DeptID      int64
	IsSystem    bool
	Roles       []PrincipalRole
	Permissions map[string]struct{}
	IsAdmin     bool
}

func setCurrentPrincipal(c *gin.Context, principal *Principal) {
	c.Set(principalContextKey, principal)
}

// CurrentPrincipal returns the authenticated principal stored in Gin context.
func CurrentPrincipal(c *gin.Context) (*Principal, bool) {
	if c == nil {
		return nil, false
	}
	value, ok := c.Get(principalContextKey)
	if !ok {
		return nil, false
	}
	principal, ok := value.(*Principal)
	return principal, ok && principal != nil
}

// CurrentUserID returns the authenticated user id or 0 when unavailable.
func CurrentUserID(c *gin.Context) int64 {
	principal, ok := CurrentPrincipal(c)
	if !ok {
		return 0
	}
	return principal.UserID
}

// CurrentUserIsAdmin reports whether the authenticated user has the admin role.
func CurrentUserIsAdmin(c *gin.Context) bool {
	principal, ok := CurrentPrincipal(c)
	return ok && principal.IsAdmin
}

// HasPermission reports whether the principal owns a permission code.
func (p *Principal) HasPermission(permission string) bool {
	if p == nil {
		return false
	}
	if p.IsAdmin {
		return true
	}
	if _, ok := p.Permissions["*:*:*"]; ok {
		return true
	}
	_, ok := p.Permissions[permission]
	return ok
}
