package http

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"reflect"
	"sort"
	"strconv"
	"strings"
	"testing"
)

func TestResolveUserDataScopeCombinesRoleScopes(t *testing.T) {
	db := newDataScopeTestDB(t, dataScopeTestStore{
		deptDescendants: map[int64][]int64{
			10: {10, 11, 12},
		},
		roleDepts: map[int64][]int64{
			50: {30, 31},
		},
	})

	scope, err := resolveUserDataScope(context.Background(), db, &Principal{
		UserID: 7,
		DeptID: 10,
		Roles: []PrincipalRole{
			{ID: 20, Code: "manager", DataScope: dataScopeDeptAndChildren},
			{ID: 30, Code: "self", DataScope: dataScopeSelf},
			{ID: 50, Code: "custom", DataScope: dataScopeCustom},
		},
	})
	if err != nil {
		t.Fatalf("resolve data scope: %v", err)
	}

	if scope.All {
		t.Fatalf("scope.All = true, want false")
	}
	if !scope.Self || scope.UserID != 7 {
		t.Fatalf("self scope = (%v, %d), want (true, 7)", scope.Self, scope.UserID)
	}
	if !reflect.DeepEqual(scope.DeptIDs, []int64{10, 11, 12, 30, 31}) {
		t.Fatalf("dept ids = %v, want [10 11 12 30 31]", scope.DeptIDs)
	}
}

func TestAppendUserDataScopeFilterBuildsUnionPredicate(t *testing.T) {
	db := newDataScopeTestDB(t, dataScopeTestStore{})
	where := "WHERE 1=1"
	args := []any{}
	argPos := 1

	err := appendUserDataScopeFilter(context.Background(), db, &Principal{
		UserID: 7,
		DeptID: 10,
		Roles: []PrincipalRole{
			{ID: 30, Code: "self", DataScope: dataScopeSelf},
			{ID: 40, Code: "dept", DataScope: dataScopeDept},
		},
	}, &where, &args, &argPos, "u")
	if err != nil {
		t.Fatalf("append data scope: %v", err)
	}

	if !strings.Contains(where, "u.id = $1") {
		t.Fatalf("where %q does not contain self predicate", where)
	}
	if !strings.Contains(where, "u.dept_id = ANY($2::bigint[])") {
		t.Fatalf("where %q does not contain dept predicate", where)
	}
	if argPos != 3 {
		t.Fatalf("argPos = %d, want 3", argPos)
	}
	if got := asInt64(args[0]); got != 7 {
		t.Fatalf("self arg = %d, want 7", got)
	}
	if got := mustValue(t, args[1]); got != "{10}" {
		t.Fatalf("dept arg = %q, want {10}", got)
	}
}

func TestAppendUserDataScopeFilterSkipsAdminAndAllScope(t *testing.T) {
	db := newDataScopeTestDB(t, dataScopeTestStore{})

	for _, tc := range []struct {
		name      string
		principal *Principal
	}{
		{
			name: "admin",
			principal: &Principal{
				UserID:  1,
				IsAdmin: true,
			},
		},
		{
			name: "all data role",
			principal: &Principal{
				UserID: 2,
				Roles:  []PrincipalRole{{ID: 2, Code: "operator", DataScope: dataScopeAll}},
			},
		},
	} {
		t.Run(tc.name, func(t *testing.T) {
			where := "WHERE 1=1"
			args := []any{"existing"}
			argPos := 2
			if err := appendUserDataScopeFilter(context.Background(), db, tc.principal, &where, &args, &argPos, "u"); err != nil {
				t.Fatalf("append data scope: %v", err)
			}
			if where != "WHERE 1=1" || argPos != 2 || len(args) != 1 {
				t.Fatalf("filter changed for all scope: where=%q argPos=%d args=%v", where, argPos, args)
			}
		})
	}
}

func TestPrincipalAccessChecksUseResolvedScope(t *testing.T) {
	db := newDataScopeTestDB(t, dataScopeTestStore{
		userDepts: map[int64]int64{
			100: 10,
			101: 11,
		},
	})
	principal := &Principal{
		UserID: 7,
		DeptID: 10,
		Roles:  []PrincipalRole{{ID: 40, Code: "dept", DataScope: dataScopeDept}},
	}

	can, err := principalCanAccessUser(context.Background(), db, principal, 100)
	if err != nil {
		t.Fatalf("check accessible user: %v", err)
	}
	if !can {
		t.Fatalf("same dept user should be accessible")
	}

	can, err = principalCanAccessUser(context.Background(), db, principal, 101)
	if err != nil {
		t.Fatalf("check inaccessible user: %v", err)
	}
	if can {
		t.Fatalf("other dept user should not be accessible")
	}

	can, err = principalCanUseDept(context.Background(), db, principal, 10)
	if err != nil {
		t.Fatalf("check accessible dept: %v", err)
	}
	if !can {
		t.Fatalf("same dept should be usable")
	}

	can, err = principalCanUseDept(context.Background(), db, principal, 11)
	if err != nil {
		t.Fatalf("check inaccessible dept: %v", err)
	}
	if can {
		t.Fatalf("other dept should not be usable")
	}
}

type dataScopeTestStore struct {
	deptDescendants map[int64][]int64
	roleDepts       map[int64][]int64
	userDepts       map[int64]int64
}

func newDataScopeTestDB(t *testing.T, store dataScopeTestStore) *sql.DB {
	t.Helper()
	db := sql.OpenDB(dataScopeTestConnector{store: store})
	t.Cleanup(func() { _ = db.Close() })
	return db
}

type dataScopeTestConnector struct {
	store dataScopeTestStore
}

func (c dataScopeTestConnector) Connect(context.Context) (driver.Conn, error) {
	return dataScopeTestConn{store: c.store}, nil
}

func (c dataScopeTestConnector) Driver() driver.Driver {
	return dataScopeTestDriver{}
}

type dataScopeTestDriver struct{}

func (dataScopeTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use OpenDB with dataScopeTestConnector")
}

type dataScopeTestConn struct {
	store dataScopeTestStore
}

func (c dataScopeTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented")
}

func (c dataScopeTestConn) Close() error {
	return nil
}

func (c dataScopeTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not implemented")
}

func (c dataScopeTestConn) QueryContext(_ context.Context, query string, args []driver.NamedValue) (driver.Rows, error) {
	compact := strings.Join(strings.Fields(query), " ")
	switch {
	case strings.Contains(compact, "WITH RECURSIVE dept_tree"):
		deptID := asInt64(args[0].Value)
		ids := append([]int64(nil), c.store.deptDescendants[deptID]...)
		if len(ids) == 0 && deptID > 0 {
			ids = []int64{deptID}
		}
		values := make([][]driver.Value, 0, len(ids))
		for _, id := range ids {
			values = append(values, []driver.Value{id})
		}
		return newDataScopeRows([]string{"id"}, values), nil
	case strings.Contains(compact, "FROM sys_role_dept"):
		roleIDs := parseDriverInt64Array(args[0].Value)
		deptSet := map[int64]struct{}{}
		for _, roleID := range roleIDs {
			for _, deptID := range c.store.roleDepts[roleID] {
				deptSet[deptID] = struct{}{}
			}
		}
		ids := make([]int64, 0, len(deptSet))
		for id := range deptSet {
			ids = append(ids, id)
		}
		sort.Slice(ids, func(i, j int) bool { return ids[i] < ids[j] })
		values := make([][]driver.Value, 0, len(ids))
		for _, id := range ids {
			values = append(values, []driver.Value{id})
		}
		return newDataScopeRows([]string{"dept_id"}, values), nil
	case strings.Contains(compact, "SELECT dept_id FROM sys_user"):
		userID := asInt64(args[0].Value)
		deptID, ok := c.store.userDepts[userID]
		if !ok {
			return newDataScopeRows([]string{"dept_id"}, nil), nil
		}
		return newDataScopeRows([]string{"dept_id"}, [][]driver.Value{{deptID}}), nil
	default:
		return nil, errors.New("unexpected query: " + compact)
	}
}

type dataScopeRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func newDataScopeRows(columns []string, values [][]driver.Value) *dataScopeRows {
	return &dataScopeRows{columns: columns, values: values}
}

func (r *dataScopeRows) Columns() []string {
	return r.columns
}

func (r *dataScopeRows) Close() error {
	return nil
}

func (r *dataScopeRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}

func mustValue(t *testing.T, v any) string {
	t.Helper()
	valuer, ok := v.(driver.Valuer)
	if !ok {
		t.Fatalf("%T does not implement driver.Valuer", v)
	}
	value, err := valuer.Value()
	if err != nil {
		t.Fatalf("value: %v", err)
	}
	s, ok := value.(string)
	if !ok {
		t.Fatalf("value = %T(%v), want string", value, value)
	}
	return s
}

func parseDriverInt64Array(v any) []int64 {
	switch x := v.(type) {
	case pqInt64Array:
		return []int64(x)
	case string:
		trimmed := strings.Trim(x, "{}")
		if trimmed == "" {
			return nil
		}
		parts := strings.Split(trimmed, ",")
		result := make([]int64, 0, len(parts))
		for _, part := range parts {
			n, err := strconv.ParseInt(strings.TrimSpace(part), 10, 64)
			if err == nil {
				result = append(result, n)
			}
		}
		return result
	default:
		return nil
	}
}
