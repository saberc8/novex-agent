package http

import (
	"database/sql"
	"errors"

	"github.com/gin-gonic/gin"

	"voc-go-backend/internal/infrastructure/security"
)

const clientPrincipalContextKey = "clientPrincipal"

type ClientPrincipal struct {
	UserID   int64
	Phone    string
	Nickname string
	Avatar   string
}

func setCurrentClientPrincipal(c *gin.Context, principal *ClientPrincipal) {
	c.Set(clientPrincipalContextKey, principal)
}

func CurrentClientPrincipal(c *gin.Context) (*ClientPrincipal, bool) {
	v, ok := c.Get(clientPrincipalContextKey)
	if !ok {
		return nil, false
	}
	principal, ok := v.(*ClientPrincipal)
	return principal, ok && principal != nil
}

func CurrentClientUserID(c *gin.Context) int64 {
	if principal, ok := CurrentClientPrincipal(c); ok {
		return principal.UserID
	}
	return 0
}

func RequireClientAuth(db *sql.DB, tokenSvc *security.TokenService) gin.HandlerFunc {
	return func(c *gin.Context) {
		principal, err := loadClientPrincipal(c, db, tokenSvc)
		if err != nil {
			Fail(c, "401", "未授权，请重新登录")
			c.Abort()
			return
		}
		setCurrentClientPrincipal(c, principal)
		c.Next()
	}
}

func loadClientPrincipal(c *gin.Context, db *sql.DB, tokenSvc *security.TokenService) (*ClientPrincipal, error) {
	if db == nil || tokenSvc == nil {
		return nil, errors.New("client auth dependency is nil")
	}
	claims, err := tokenSvc.ParseContext(c.Request.Context(), c.GetHeader("Authorization"))
	if err != nil || claims.UserID == 0 || claims.Subject != security.SubjectClient {
		return nil, errors.New("invalid client token")
	}

	const query = `
SELECT id,
       phone,
       nickname,
       COALESCE(avatar, ''),
       COALESCE(status, 0)
FROM client_user
WHERE id = $1
LIMIT 1;
`
	var principal ClientPrincipal
	var status int16
	if err := db.QueryRowContext(c.Request.Context(), query, claims.UserID).Scan(
		&principal.UserID,
		&principal.Phone,
		&principal.Nickname,
		&principal.Avatar,
		&status,
	); err != nil {
		return nil, err
	}
	if status != 1 {
		return nil, errors.New("client user disabled")
	}
	return &principal, nil
}
