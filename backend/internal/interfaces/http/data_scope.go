package http

import (
	"context"
	"database/sql"
	"errors"
	"fmt"
	"sort"
	"strings"
)

const (
	dataScopeAll             int32 = 1
	dataScopeDeptAndChildren int32 = 2
	dataScopeDept            int32 = 3
	dataScopeSelf            int32 = 4
	dataScopeCustom          int32 = 5
)

type userDataScope struct {
	All     bool
	UserID  int64
	Self    bool
	DeptIDs []int64
}

func resolveUserDataScope(ctx context.Context, db *sql.DB, principal *Principal) (userDataScope, error) {
	if principal == nil || principal.UserID <= 0 {
		return userDataScope{}, errors.New("missing authenticated principal")
	}

	scope := userDataScope{
		All:    principal.IsAdmin,
		UserID: principal.UserID,
	}
	if scope.All {
		return scope, nil
	}
	if len(principal.Roles) == 0 {
		scope.Self = true
		return scope, nil
	}

	deptSet := make(map[int64]struct{})
	var customRoleIDs []int64
	for _, role := range principal.Roles {
		switch role.DataScope {
		case dataScopeAll:
			scope.All = true
			return scope, nil
		case dataScopeDeptAndChildren:
			if principal.DeptID <= 0 {
				continue
			}
			ids, err := loadDeptAndDescendantIDs(ctx, db, principal.DeptID)
			if err != nil {
				return userDataScope{}, err
			}
			addDeptIDs(deptSet, ids)
		case dataScopeDept:
			addDeptID(deptSet, principal.DeptID)
		case dataScopeSelf:
			scope.Self = true
		case dataScopeCustom:
			if role.ID > 0 {
				customRoleIDs = append(customRoleIDs, role.ID)
			}
		default:
			scope.Self = true
		}
	}

	if len(customRoleIDs) > 0 {
		ids, err := loadCustomDataScopeDeptIDs(ctx, db, customRoleIDs)
		if err != nil {
			return userDataScope{}, err
		}
		addDeptIDs(deptSet, ids)
	}

	scope.DeptIDs = sortedInt64Keys(deptSet)
	return scope, nil
}

func appendUserDataScopeFilter(ctx context.Context, db *sql.DB, principal *Principal, where *string, args *[]any, argPos *int, userAlias string) error {
	if where == nil || args == nil || argPos == nil {
		return errors.New("invalid data scope SQL state")
	}
	scope, err := resolveUserDataScope(ctx, db, principal)
	if err != nil {
		return err
	}
	if scope.All {
		return nil
	}
	if strings.TrimSpace(userAlias) == "" {
		userAlias = "u"
	}

	conditions := make([]string, 0, 2)
	if scope.Self && scope.UserID > 0 {
		conditions = append(conditions, fmt.Sprintf("%s.id = $%d", userAlias, *argPos))
		*args = append(*args, scope.UserID)
		*argPos++
	}
	if len(scope.DeptIDs) > 0 {
		conditions = append(conditions, fmt.Sprintf("%s.dept_id = ANY($%d::bigint[])", userAlias, *argPos))
		*args = append(*args, pqInt64Array(scope.DeptIDs))
		*argPos++
	}

	if len(conditions) == 0 {
		*where += " AND 1=0"
		return nil
	}
	*where += " AND (" + strings.Join(conditions, " OR ") + ")"
	return nil
}

func appendDeptDataScopeFilter(ctx context.Context, db *sql.DB, principal *Principal, where *string, args *[]any, argPos *int, deptAlias string) error {
	if where == nil || args == nil || argPos == nil {
		return errors.New("invalid data scope SQL state")
	}
	scope, err := resolveUserDataScope(ctx, db, principal)
	if err != nil {
		return err
	}
	if scope.All {
		return nil
	}
	if strings.TrimSpace(deptAlias) == "" {
		deptAlias = "d"
	}
	if len(scope.DeptIDs) == 0 {
		*where += " AND 1=0"
		return nil
	}
	*where += fmt.Sprintf(" AND %s.id = ANY($%d::bigint[])", deptAlias, *argPos)
	*args = append(*args, pqInt64Array(scope.DeptIDs))
	*argPos++
	return nil
}

func principalCanAccessUser(ctx context.Context, db *sql.DB, principal *Principal, userID int64) (bool, error) {
	if userID <= 0 {
		return false, nil
	}
	scope, err := resolveUserDataScope(ctx, db, principal)
	if err != nil {
		return false, err
	}
	if scope.All {
		return true, nil
	}
	if scope.Self && userID == scope.UserID {
		return true, nil
	}

	var deptID int64
	err = db.QueryRowContext(ctx, `SELECT dept_id FROM sys_user WHERE id = $1`, userID).Scan(&deptID)
	if err == sql.ErrNoRows {
		return false, nil
	}
	if err != nil {
		return false, err
	}
	return containsInt64(scope.DeptIDs, deptID), nil
}

func principalCanAccessUsers(ctx context.Context, db *sql.DB, principal *Principal, userIDs []int64) (bool, error) {
	for _, userID := range normalizeInt64IDs(userIDs) {
		can, err := principalCanAccessUser(ctx, db, principal, userID)
		if err != nil || !can {
			return can, err
		}
	}
	return true, nil
}

func principalCanAccessUserRoleRows(ctx context.Context, db *sql.DB, principal *Principal, userRoleIDs []int64) (bool, error) {
	userRoleIDs = normalizeInt64IDs(userRoleIDs)
	if len(userRoleIDs) == 0 {
		return true, nil
	}

	rows, err := db.QueryContext(ctx, `SELECT user_id FROM sys_user_role WHERE id = ANY($1::bigint[])`, pqInt64Array(userRoleIDs))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	userIDs := make([]int64, 0, len(userRoleIDs))
	for rows.Next() {
		var userID int64
		if err := rows.Scan(&userID); err != nil {
			return false, err
		}
		userIDs = append(userIDs, userID)
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	if len(normalizeInt64IDs(userIDs)) != len(userRoleIDs) {
		return false, nil
	}
	return principalCanAccessUsers(ctx, db, principal, userIDs)
}

func principalCanUseDept(ctx context.Context, db *sql.DB, principal *Principal, deptID int64) (bool, error) {
	if deptID <= 0 {
		return false, nil
	}
	scope, err := resolveUserDataScope(ctx, db, principal)
	if err != nil {
		return false, err
	}
	if scope.All {
		return true, nil
	}
	return containsInt64(scope.DeptIDs, deptID), nil
}

func principalCanConfigureRoleDataScope(ctx context.Context, db *sql.DB, principal *Principal, dataScope int32, deptIDs []int64) (bool, error) {
	if !isValidDataScope(dataScope) {
		return false, nil
	}
	if principal == nil || principal.UserID <= 0 {
		return false, errors.New("missing authenticated principal")
	}
	if principal.IsAdmin {
		if dataScope == dataScopeCustom {
			return len(normalizeInt64IDs(deptIDs)) > 0, nil
		}
		return true, nil
	}

	scope, err := resolveUserDataScope(ctx, db, principal)
	if err != nil {
		return false, err
	}
	switch dataScope {
	case dataScopeAll:
		return false, nil
	case dataScopeSelf:
		return true, nil
	case dataScopeDept:
		return scope.All || len(scope.DeptIDs) > 0, nil
	case dataScopeDeptAndChildren:
		return scope.All || principalHasDataScope(principal, dataScopeDeptAndChildren), nil
	case dataScopeCustom:
		deptIDs = normalizeInt64IDs(deptIDs)
		if len(deptIDs) == 0 {
			return false, nil
		}
		if scope.All {
			return true, nil
		}
		for _, deptID := range deptIDs {
			if !containsInt64(scope.DeptIDs, deptID) {
				return false, nil
			}
		}
		return true, nil
	default:
		return false, nil
	}
}

func principalHasDataScope(principal *Principal, dataScope int32) bool {
	if principal == nil {
		return false
	}
	for _, role := range principal.Roles {
		if role.DataScope == dataScope {
			return true
		}
	}
	return false
}

func principalCanAssignMenuIDs(ctx context.Context, db *sql.DB, principal *Principal, menuIDs []int64) (bool, error) {
	menuIDs = normalizeInt64IDs(menuIDs)
	if len(menuIDs) == 0 {
		return true, nil
	}
	if principal == nil || principal.UserID <= 0 {
		return false, errors.New("missing authenticated principal")
	}
	if principal.IsAdmin {
		return true, nil
	}

	const query = `
SELECT DISTINCT rm.menu_id
  FROM sys_role_menu AS rm
  JOIN sys_user_role AS ur ON ur.role_id = rm.role_id
 WHERE ur.user_id = $1
   AND rm.menu_id = ANY($2::bigint[]);
`
	rows, err := db.QueryContext(ctx, query, principal.UserID, pqInt64Array(menuIDs))
	if err != nil {
		return false, err
	}
	defer rows.Close()

	owned := make(map[int64]struct{}, len(menuIDs))
	for rows.Next() {
		var menuID int64
		if err := rows.Scan(&menuID); err != nil {
			return false, err
		}
		owned[menuID] = struct{}{}
	}
	if err := rows.Err(); err != nil {
		return false, err
	}
	for _, menuID := range menuIDs {
		if _, ok := owned[menuID]; !ok {
			return false, nil
		}
	}
	return true, nil
}

func principalCanGrantRoles(ctx context.Context, db *sql.DB, principal *Principal, roleIDs []int64) (bool, error) {
	roleIDs = normalizeInt64IDs(roleIDs)
	if len(roleIDs) == 0 {
		return true, nil
	}
	if principal == nil || principal.UserID <= 0 {
		return false, errors.New("missing authenticated principal")
	}

	roles, err := loadRoleGrantInfos(ctx, db, roleIDs)
	if err != nil {
		return false, err
	}
	if len(roles) != len(roleIDs) {
		return false, nil
	}
	if principal.IsAdmin {
		return true, nil
	}

	customRoleIDs := make([]int64, 0)
	for _, role := range roles {
		if role.ID == 1 || strings.EqualFold(role.Code, "admin") {
			return false, nil
		}
		if role.DataScope == dataScopeCustom {
			customRoleIDs = append(customRoleIDs, role.ID)
		}
	}
	roleDeptIDs, err := loadRoleDeptIDMap(ctx, db, customRoleIDs)
	if err != nil {
		return false, err
	}
	for _, roleID := range roleIDs {
		role := roles[roleID]
		canConfigureScope, err := principalCanConfigureRoleDataScope(ctx, db, principal, role.DataScope, roleDeptIDs[role.ID])
		if err != nil || !canConfigureScope {
			return canConfigureScope, err
		}
	}

	menuIDs, err := loadRoleMenuIDs(ctx, db, roleIDs)
	if err != nil {
		return false, err
	}
	return principalCanAssignMenuIDs(ctx, db, principal, menuIDs)
}

func principalCanManageRole(ctx context.Context, db *sql.DB, principal *Principal, roleID int64) (bool, error) {
	if roleID <= 0 {
		return false, nil
	}
	return principalCanGrantRoles(ctx, db, principal, []int64{roleID})
}

type roleGrantInfo struct {
	ID        int64
	Code      string
	DataScope int32
	IsSystem  bool
}

func loadRoleGrantInfos(ctx context.Context, db *sql.DB, roleIDs []int64) (map[int64]roleGrantInfo, error) {
	roleIDs = normalizeInt64IDs(roleIDs)
	if len(roleIDs) == 0 {
		return map[int64]roleGrantInfo{}, nil
	}
	const query = `
SELECT id,
       code,
       COALESCE(data_scope, 4),
       COALESCE(is_system, FALSE)
  FROM sys_role
 WHERE id = ANY($1::bigint[]);
`
	rows, err := db.QueryContext(ctx, query, pqInt64Array(roleIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	roles := make(map[int64]roleGrantInfo, len(roleIDs))
	for rows.Next() {
		var role roleGrantInfo
		if err := rows.Scan(&role.ID, &role.Code, &role.DataScope, &role.IsSystem); err != nil {
			return nil, err
		}
		roles[role.ID] = role
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return roles, nil
}

func loadRoleMenuIDs(ctx context.Context, db *sql.DB, roleIDs []int64) ([]int64, error) {
	roleIDs = normalizeInt64IDs(roleIDs)
	if len(roleIDs) == 0 {
		return nil, nil
	}
	const query = `
SELECT role_id, menu_id
  FROM sys_role_menu
 WHERE role_id = ANY($1::bigint[]);
`
	rows, err := db.QueryContext(ctx, query, pqInt64Array(roleIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var roleID, menuID int64
		if err := rows.Scan(&roleID, &menuID); err != nil {
			return nil, err
		}
		ids = append(ids, menuID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return normalizeInt64IDs(ids), nil
}

func loadRoleDeptIDMap(ctx context.Context, db *sql.DB, roleIDs []int64) (map[int64][]int64, error) {
	roleIDs = normalizeInt64IDs(roleIDs)
	result := make(map[int64][]int64, len(roleIDs))
	if len(roleIDs) == 0 {
		return result, nil
	}
	const query = `
SELECT role_id, dept_id
  FROM sys_role_dept
 WHERE role_id = ANY($1::bigint[]);
`
	rows, err := db.QueryContext(ctx, query, pqInt64Array(roleIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	sets := make(map[int64]map[int64]struct{}, len(roleIDs))
	for rows.Next() {
		var roleID, deptID int64
		if err := rows.Scan(&roleID, &deptID); err != nil {
			return nil, err
		}
		set := sets[roleID]
		if set == nil {
			set = make(map[int64]struct{})
			sets[roleID] = set
		}
		addDeptID(set, deptID)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	for roleID, set := range sets {
		result[roleID] = sortedInt64Keys(set)
	}
	return result, nil
}

func isValidDataScope(dataScope int32) bool {
	switch dataScope {
	case dataScopeAll, dataScopeDeptAndChildren, dataScopeDept, dataScopeSelf, dataScopeCustom:
		return true
	default:
		return false
	}
}

func loadDeptAndDescendantIDs(ctx context.Context, db *sql.DB, deptID int64) ([]int64, error) {
	const query = `
WITH RECURSIVE dept_tree AS (
    SELECT id
      FROM sys_dept
     WHERE id = $1
    UNION ALL
    SELECT d.id
      FROM sys_dept AS d
      JOIN dept_tree AS parent ON d.parent_id = parent.id
)
SELECT id FROM dept_tree;
`
	rows, err := db.QueryContext(ctx, query, deptID)
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func loadCustomDataScopeDeptIDs(ctx context.Context, db *sql.DB, roleIDs []int64) ([]int64, error) {
	roleIDs = normalizeInt64IDs(roleIDs)
	if len(roleIDs) == 0 {
		return nil, nil
	}
	const query = `
SELECT DISTINCT dept_id
  FROM sys_role_dept
 WHERE role_id = ANY($1::bigint[]);
`
	rows, err := db.QueryContext(ctx, query, pqInt64Array(roleIDs))
	if err != nil {
		return nil, err
	}
	defer rows.Close()

	var ids []int64
	for rows.Next() {
		var id int64
		if err := rows.Scan(&id); err != nil {
			return nil, err
		}
		ids = append(ids, id)
	}
	if err := rows.Err(); err != nil {
		return nil, err
	}
	return ids, nil
}

func addDeptIDs(set map[int64]struct{}, ids []int64) {
	for _, id := range ids {
		addDeptID(set, id)
	}
}

func addDeptID(set map[int64]struct{}, id int64) {
	if id > 0 {
		set[id] = struct{}{}
	}
}

func sortedInt64Keys(set map[int64]struct{}) []int64 {
	ids := make([]int64, 0, len(set))
	for id := range set {
		ids = append(ids, id)
	}
	sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
	return ids
}

func containsInt64(ids []int64, target int64) bool {
	for _, id := range ids {
		if id == target {
			return true
		}
	}
	return false
}

func normalizeInt64IDs(ids []int64) []int64 {
	seen := make(map[int64]struct{}, len(ids))
	result := make([]int64, 0, len(ids))
	for _, id := range ids {
		if id <= 0 {
			continue
		}
		if _, ok := seen[id]; ok {
			continue
		}
		seen[id] = struct{}{}
		result = append(result, id)
	}
	return result
}
