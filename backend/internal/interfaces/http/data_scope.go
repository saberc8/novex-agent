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
