package db

import (
	"context"
	"database/sql"
	"database/sql/driver"
	"errors"
	"io"
	"strings"
	"testing"
)

func TestAutoMigrateEnsuresClientUserAndH5Client(t *testing.T) {
	var execs []string
	database := sql.OpenDB(migrateTestConnector{execs: &execs})
	t.Cleanup(func() { _ = database.Close() })

	if err := AutoMigrate(database); err != nil {
		t.Fatalf("AutoMigrate returned error: %v", err)
	}

	assertExecutedSQL(t, execs, "client_user", "create table")
	assertExecutedSQL(t, execs, "client_user", "uk_client_user_phone")
	assertExecutedSQL(t, execs, "sys_client", "h5-client")
	assertExecutedSQL(t, execs, "sys_client", "'h5'")
	assertExecutedSQL(t, execs, "sys_dict_item", "'h5'")
}

func TestAutoMigrateEnsuresDataScopeDictionary(t *testing.T) {
	var execs []string
	database := sql.OpenDB(migrateTestConnector{execs: &execs})
	t.Cleanup(func() { _ = database.Close() })

	if err := AutoMigrate(database); err != nil {
		t.Fatalf("AutoMigrate returned error: %v", err)
	}

	assertExecutedSQL(t, execs, "sys_dict", "data_scope_enum")
	assertExecutedSQL(t, execs, "sys_dict_item", "全部数据权限")
	assertExecutedSQL(t, execs, "sys_dict_item", "自定义数据权限")
}

func TestAutoMigrateMarksSeedMenusAsSystem(t *testing.T) {
	var execs []string
	database := sql.OpenDB(migrateTestConnector{execs: &execs})
	t.Cleanup(func() { _ = database.Close() })

	if err := AutoMigrate(database); err != nil {
		t.Fatalf("AutoMigrate returned error: %v", err)
	}

	assertExecutedSQL(t, execs, "alter table sys_menu", "add column if not exists is_system")
	assertExecutedSQL(t, execs, "update sys_menu", "set is_system = true", "id between 1000 and 2033")
}

func TestAutoMigrateConsolidatesDictItemMenu(t *testing.T) {
	var execs []string
	database := sql.OpenDB(migrateTestConnector{execs: &execs})
	t.Cleanup(func() { _ = database.Close() })

	if err := AutoMigrate(database); err != nil {
		t.Fatalf("AutoMigrate returned error: %v", err)
	}

	assertNotExecutedSQL(t, execs, "system/dict/item/index")
	assertExecutedSQL(t, execs, "update sys_menu", "set parent_id = 1130", "where id in (1141, 1142, 1143, 1144, 1145)")
	assertNotExecutedSQL(t, execs, "delete from sys_role_menu", "menu_id = 1140")
	assertNotExecutedSQL(t, execs, "delete from sys_menu", "where id = 1140")
}

func assertExecutedSQL(t *testing.T, execs []string, parts ...string) {
	t.Helper()
	for _, query := range execs {
		compact := strings.ToLower(strings.Join(strings.Fields(query), " "))
		matched := true
		for _, part := range parts {
			if !strings.Contains(compact, strings.ToLower(part)) {
				matched = false
				break
			}
		}
		if matched {
			return
		}
	}
	t.Fatalf("expected executed SQL containing %q; executed queries:\n%s", parts, strings.Join(execs, "\n---\n"))
}

func assertNotExecutedSQL(t *testing.T, execs []string, parts ...string) {
	t.Helper()
	for _, query := range execs {
		compact := strings.ToLower(strings.Join(strings.Fields(query), " "))
		matched := true
		for _, part := range parts {
			if !strings.Contains(compact, strings.ToLower(part)) {
				matched = false
				break
			}
		}
		if matched {
			t.Fatalf("unexpected executed SQL containing %q; query:\n%s", parts, query)
		}
	}
}

type migrateTestConnector struct {
	execs *[]string
}

func (c migrateTestConnector) Connect(context.Context) (driver.Conn, error) {
	return migrateTestConn{execs: c.execs}, nil
}

func (c migrateTestConnector) Driver() driver.Driver {
	return migrateTestDriver{}
}

type migrateTestDriver struct{}

func (migrateTestDriver) Open(string) (driver.Conn, error) {
	return nil, errors.New("use OpenDB with migrateTestConnector")
}

type migrateTestConn struct {
	execs *[]string
}

func (c migrateTestConn) Prepare(string) (driver.Stmt, error) {
	return nil, errors.New("prepared statements are not implemented")
}

func (c migrateTestConn) Close() error {
	return nil
}

func (c migrateTestConn) Begin() (driver.Tx, error) {
	return nil, errors.New("transactions are not implemented")
}

func (c migrateTestConn) ExecContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Result, error) {
	*c.execs = append(*c.execs, query)
	return driver.RowsAffected(1), nil
}

func (c migrateTestConn) QueryContext(_ context.Context, query string, _ []driver.NamedValue) (driver.Rows, error) {
	compact := strings.ToLower(strings.Join(strings.Fields(query), " "))
	switch {
	case strings.Contains(compact, "information_schema.columns"):
		return newMigrateTestRows([]string{"exists"}, [][]driver.Value{{int64(1)}}), nil
	case strings.Contains(compact, "to_regclass"):
		return newMigrateTestRows([]string{"to_regclass"}, [][]driver.Value{{"public.existing_table"}}), nil
	default:
		return newMigrateTestRows([]string{"value"}, nil), nil
	}
}

type migrateTestRows struct {
	columns []string
	values  [][]driver.Value
	index   int
}

func newMigrateTestRows(columns []string, values [][]driver.Value) *migrateTestRows {
	return &migrateTestRows{columns: columns, values: values}
}

func (r *migrateTestRows) Columns() []string {
	return r.columns
}

func (r *migrateTestRows) Close() error {
	return nil
}

func (r *migrateTestRows) Next(dest []driver.Value) error {
	if r.index >= len(r.values) {
		return io.EOF
	}
	copy(dest, r.values[r.index])
	r.index++
	return nil
}
