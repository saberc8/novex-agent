package http

import (
	"database/sql"
	"fmt"

	"github.com/gin-gonic/gin"
)

func ensureSystemRoleMutable(c *gin.Context, db *sql.DB, roleID int64, action string) bool {
	if CurrentUserIsAdmin(c) {
		return true
	}
	var name string
	err := db.QueryRowContext(c.Request.Context(), `
SELECT name
FROM sys_role
WHERE id = $1 AND is_system = TRUE
LIMIT 1;
`, roleID).Scan(&name)
	return handleSystemGuardResult(c, err, "校验系统内置角色失败", fmt.Sprintf("角色 [%s] 是系统内置角色，不允许%s", name, action))
}

func ensureSystemUserMutable(c *gin.Context, db *sql.DB, userID int64, action string) bool {
	if CurrentUserIsAdmin(c) {
		return true
	}
	var nickname string
	err := db.QueryRowContext(c.Request.Context(), `
SELECT nickname
FROM sys_user
WHERE id = $1 AND is_system = TRUE
LIMIT 1;
`, userID).Scan(&nickname)
	return handleSystemGuardResult(c, err, "校验系统内置用户失败", fmt.Sprintf("用户 [%s] 是系统内置用户，不允许%s", nickname, action))
}

func ensureSystemDictMutable(c *gin.Context, db *sql.DB, dictID int64, action string) bool {
	if CurrentUserIsAdmin(c) {
		return true
	}
	var name string
	err := db.QueryRowContext(c.Request.Context(), `
SELECT name
FROM sys_dict
WHERE id = $1 AND is_system = TRUE
LIMIT 1;
	`, dictID).Scan(&name)
	return handleSystemGuardResult(c, err, "校验系统内置字典失败", fmt.Sprintf("字典 [%s] 是系统内置字典，不允许%s", name, action))
}

func ensureSystemClientMutable(c *gin.Context, db *sql.DB, clientID int64, action string) bool {
	if CurrentUserIsAdmin(c) {
		return true
	}
	var clientCode string
	err := db.QueryRowContext(c.Request.Context(), `
SELECT client_id
FROM sys_client
WHERE id = $1
  AND client_id IN ('ef51c9a3e9046c4f2ea45142c8a8344a', 'h5-client')
LIMIT 1;
`, clientID).Scan(&clientCode)
	return handleSystemGuardResult(c, err, "校验系统内置客户端失败", fmt.Sprintf("客户端 [%s] 是系统内置客户端，不允许%s", clientCode, action))
}

func ensureSystemMenuMutable(c *gin.Context, db *sql.DB, menuID int64, action string) bool {
	if CurrentUserIsAdmin(c) {
		return true
	}
	var title string
	err := db.QueryRowContext(c.Request.Context(), `
SELECT title
FROM sys_menu
WHERE id = $1 AND is_system = TRUE
LIMIT 1;
`, menuID).Scan(&title)
	return handleSystemGuardResult(c, err, "校验系统内置菜单失败", fmt.Sprintf("菜单 [%s] 是系统内置菜单，不允许%s", title, action))
}

func handleSystemGuardResult(c *gin.Context, err error, systemErrMsg, forbiddenMsg string) bool {
	if err == nil {
		Fail(c, "400", forbiddenMsg)
		return false
	}
	if err == sql.ErrNoRows {
		return true
	}
	Fail(c, "500", systemErrMsg)
	return false
}

func requireCurrentUser(c *gin.Context) int64 {
	userID := CurrentUserID(c)
	if userID == 0 {
		Fail(c, "401", "未授权，请重新登录")
	}
	return userID
}
